package pipeline

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

const stickerSearchToolName = "sticker_search"
const stickerListToolName = "sticker_list"

var stickerSearchAgentSpec = tools.AgentToolSpec{
	Name:        stickerSearchToolName,
	Version:     "1",
	Description: "搜索当前账户可发送的 Telegram sticker。",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var stickerListAgentSpec = tools.AgentToolSpec{
	Name:        stickerListToolName,
	Version:     "1",
	Description: "列出当前账户可发送的 Telegram sticker。",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var stickerSearchLlmSpec = llm.ToolSpec{
	Name:        stickerSearchToolName,
	Description: stickerStringPtr("搜索当前账户可发送的 Telegram sticker。当热列表不够用时调用。拿到结果后，如要发送，直接输出 [sticker:<id>]；不要调用 telegram_send_file 发送 sticker。"),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"query"},
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "情绪、场景、meme 或想表达的意思。",
			},
		},
	},
}

var stickerListLlmSpec = llm.ToolSpec{
	Name:        stickerListToolName,
	Description: stickerStringPtr("列出当前账户可发送的 Telegram sticker。想先浏览可用 sticker 时调用。拿到结果后，如要发送，直接输出 [sticker:<id>]；不要调用 telegram_send_file 发送 sticker。"),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "integer",
				"description": "返回数量上限，默认 20，最大 100。",
				"minimum":     1,
				"maximum":     100,
			},
		},
	},
}

func NewStickerInjectMiddleware(db data.QueryDB) RunMiddleware {
	repo := data.AccountStickersRepository{}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ChannelContext == nil || !strings.EqualFold(strings.TrimSpace(rc.ChannelContext.ChannelType), "telegram") || db == nil {
			return next(ctx, rc)
		}
		items, err := repo.ListHot(ctx, db, rc.Run.AccountID, time.Now().UTC().Add(-7*24*time.Hour), 20)
		if err == nil && len(items) > 0 {
			rc.UpsertPromptSegment(PromptSegment{
				Name:      "telegram.stickers",
				Target:    PromptTargetSystemPrefix,
				Role:      "system",
				Stability: PromptStabilityStablePrefix,
				Text:      renderHotStickerPrompt(items),
			})
		}
		rc.UpsertPromptSegment(PromptSegment{
			Name:      "telegram.sticker_instruction",
			Target:    PromptTargetSystemPrefix,
			Role:      "system",
			Stability: PromptStabilityStablePrefix,
			Text:      "发送 Telegram sticker 时，不要调用 telegram_send_file。优先从热列表选择；没有合适的就调用 sticker_list 或 sticker_search。确定 id 后，直接输出 [sticker:<id>]，delivery 会自动发成真正的 sticker。",
		})
		return next(ctx, rc)
	}
}

func renderHotStickerPrompt(items []data.AccountSticker) string {
	var sb strings.Builder
	sb.WriteString("<stickers>\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf(
			"  <sticker id=\"%s\" short=\"%s\" />\n",
			xmlEscapeAttr(strings.TrimSpace(item.ContentHash)),
			xmlEscapeAttr(strings.TrimSpace(item.ShortTags)),
		))
	}
	sb.WriteString("</stickers>")
	return sb.String()
}

func xmlEscapeAttr(value string) string {
	var sb strings.Builder
	if err := xml.EscapeText(&sb, []byte(value)); err != nil {
		return value
	}
	escaped := sb.String()
	escaped = strings.ReplaceAll(escaped, `"`, "&quot;")
	escaped = strings.ReplaceAll(escaped, `'`, "&apos;")
	return escaped
}

func NewStickerToolMiddleware(db data.QueryDB) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ChannelContext == nil || !strings.EqualFold(strings.TrimSpace(rc.ChannelContext.ChannelType), "telegram") || db == nil {
			return next(ctx, rc)
		}
		var specs []tools.AgentToolSpec
		if stickerToolAllowed(rc, stickerSearchToolName) {
			rc.ToolExecutors[stickerSearchToolName] = &stickerSearchExecutor{db: db, accountID: rc.Run.AccountID}
			rc.AllowlistSet[stickerSearchToolName] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, stickerSearchLlmSpec)
			specs = append(specs, stickerSearchAgentSpec)
		}
		if stickerToolAllowed(rc, stickerListToolName) {
			rc.ToolExecutors[stickerListToolName] = &stickerListExecutor{db: db, accountID: rc.Run.AccountID}
			rc.AllowlistSet[stickerListToolName] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, stickerListLlmSpec)
			specs = append(specs, stickerListAgentSpec)
		}
		if len(specs) > 0 {
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, specs)
		}
		return next(ctx, rc)
	}
}

type stickerSearchExecutor struct {
	db        data.QueryDB
	accountID uuid.UUID
}

func (e *stickerSearchExecutor) Execute(ctx context.Context, toolName string, args map[string]any, _ tools.ExecutionContext, _ string) tools.ExecutionResult {
	query, _ := args["query"].(string)
	items, err := data.AccountStickersRepository{}.Search(ctx, e.db, e.accountID, query, 10)
	if err != nil {
		return tools.ExecutionResult{Error: &tools.ExecutionError{
			ErrorClass: tools.ErrorClassToolExecutionFailed,
			Message:    err.Error(),
		}}
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		results = append(results, map[string]any{
			"id":          item.ContentHash,
			"short_tags":  item.ShortTags,
			"long_desc":   item.LongDesc,
			"usage_count": item.UsageCount,
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"query":   strings.TrimSpace(query),
			"results": results,
		},
	}
}

type stickerListExecutor struct {
	db        data.QueryDB
	accountID uuid.UUID
}

func (e *stickerListExecutor) Execute(ctx context.Context, toolName string, args map[string]any, _ tools.ExecutionContext, _ string) tools.ExecutionResult {
	limit := 20
	switch value := args["limit"].(type) {
	case int:
		limit = value
	case int32:
		limit = int(value)
	case int64:
		limit = int(value)
	case float64:
		limit = int(value)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, err := data.AccountStickersRepository{}.ListRegistered(ctx, e.db, e.accountID, limit)
	if err != nil {
		return tools.ExecutionResult{Error: &tools.ExecutionError{
			ErrorClass: tools.ErrorClassToolExecutionFailed,
			Message:    err.Error(),
		}}
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		results = append(results, map[string]any{
			"id":           item.ContentHash,
			"short_tags":   item.ShortTags,
			"long_desc":    item.LongDesc,
			"usage_count":  item.UsageCount,
			"last_used_at": item.LastUsedAt,
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"limit":   limit,
			"results": results,
		},
	}
}

func stickerStringPtr(value string) *string { return &value }

func containsStickerToolName(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func stickerToolAllowed(rc *RunContext, toolName string) bool {
	if rc == nil {
		return false
	}
	for _, denied := range rc.ToolDenylist {
		if strings.EqualFold(strings.TrimSpace(denied), toolName) {
			return false
		}
	}
	if rc.PersonaDefinition != nil && len(rc.PersonaDefinition.ToolAllowlist) > 0 {
		return containsStickerToolName(rc.PersonaDefinition.ToolAllowlist, toolName)
	}
	return true
}
