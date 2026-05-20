package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
)

func IsSuggestionRun(rc *RunContext) bool {
	return isSuggestionRun(rc)
}

func isSuggestionRun(rc *RunContext) bool {
	if rc == nil {
		return false
	}
	if s, ok := stringField(rc.InputJSON, "run_kind"); ok && strings.EqualFold(s, runkind.Suggestion) {
		return true
	}
	if s, ok := stringField(rc.JobPayload, "run_kind"); ok && strings.EqualFold(s, runkind.Suggestion) {
		return true
	}
	return false
}

// NewSuggestionPrepareMiddleware 为 suggestion run 注入 user message 并在完成后解析、保存建议。
func NewSuggestionPrepareMiddleware(sugStore SuggestionStore, pool CompactPersistDB, auxGateway llm.Gateway, emitDebugEvents bool, configLoader *routing.ConfigLoader) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || !isSuggestionRun(rc) {
			return next(ctx, rc)
		}

		rc.SuggestionRun = true

		// 优先使用账户级工具模型
		if pool != nil && configLoader != nil {
			if resolution, ok := resolveEntitlementRoute(ctx, pool, rc.Run.AccountID, "spawn.profile.tool", auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes, configLoader, rc.RoutingByokEnabled); ok {
				rc.Gateway = resolution.Gateway
				rc.SelectedRoute = resolution.Selected
			}
		}

		// 从 payload 读取 mode
		mode := "chat"
		if m, ok := stringField(rc.JobPayload, "mode"); ok && m != "" {
			mode = m
		}

		content := "请为 mode=" + mode + " 的对话场景生成建议。"
		rc.Messages = append(rc.Messages, llm.Message{
			Role:    "user",
			Content: []llm.ContentPart{{Type: "text", Text: content}},
		})
		rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)

		err := next(ctx, rc)

		if err == nil && sugStore != nil && strings.TrimSpace(rc.FinalAssistantOutput) != "" {
			suggestions := parseSuggestionOutput(rc.FinalAssistantOutput)
			if len(suggestions) > 0 {
				sugJSON, _ := json.Marshal(suggestions)
				expiresAt := time.Now().Add(24 * time.Hour)
				accountID := rc.Run.AccountID
				agentID := StableAgentID(rc)
				var userID uuid.UUID
				if rc.UserID != nil {
					userID = *rc.UserID
				}
				if uErr := sugStore.UpsertSuggestions(ctx, accountID, userID, agentID, mode, string(sugJSON), expiresAt); uErr != nil {
					slog.WarnContext(ctx, "suggestion: upsert failed", "err", uErr.Error())
				} else {
					slog.InfoContext(ctx, "suggestion: updated",
						"account_id", accountID.String(),
						"user_id", userID.String(),
						"mode", mode,
						"count", len(suggestions),
					)
				}
			}
		}

		return err
	}
}

type suggestionItem struct {
	ShortTitle string `json:"short_title"`
	FullPrompt string `json:"full_prompt"`
}

// parseSuggestionOutput 从 LLM 输出中提取 JSON 数组。
func parseSuggestionOutput(output string) []suggestionItem {
	text := strings.TrimSpace(output)

	// 尝试提取 JSON 代码块
	if start := strings.Index(text, "```json"); start >= 0 {
		inner := text[start+7:]
		if end := strings.Index(inner, "```"); end >= 0 {
			text = strings.TrimSpace(inner[:end])
		}
	} else if start := strings.Index(text, "```"); start >= 0 {
		inner := text[start+3:]
		if end := strings.Index(inner, "```"); end >= 0 {
			text = strings.TrimSpace(inner[:end])
		}
	}

	// 尝试找到 JSON 数组边界
	if arrStart := strings.IndexByte(text, '['); arrStart >= 0 {
		if arrEnd := strings.LastIndexByte(text, ']'); arrEnd > arrStart {
			text = text[arrStart : arrEnd+1]
		}
	}

	var items []suggestionItem
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil
	}

	var valid []suggestionItem
	for _, item := range items {
		if strings.TrimSpace(item.ShortTitle) != "" && strings.TrimSpace(item.FullPrompt) != "" {
			valid = append(valid, item)
		}
	}
	return valid
}
