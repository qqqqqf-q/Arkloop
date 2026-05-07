package pipeline

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
)

// isImpressionRun 判断当前 run 是否为 impression 生成 run。
func isImpressionRun(rc *RunContext) bool {
	if rc == nil {
		return false
	}
	if s, ok := stringField(rc.InputJSON, "run_kind"); ok && strings.EqualFold(s, runkind.Impression) {
		return true
	}
	if s, ok := stringField(rc.JobPayload, "run_kind"); ok && strings.EqualFold(s, runkind.Impression) {
		return true
	}
	return false
}

// NewImpressionPrepareMiddleware 为 impression run 注入 memory 数据并在完成后写入结果。
// auxGateway 用于覆盖 routing，使 impression run 使用工具模型而非主对话模型。
// 非 impression run 直接透传。
func NewImpressionPrepareMiddleware(impStore ImpressionStore, pool CompactPersistDB, auxGateway llm.Gateway, emitDebugEvents bool, configLoader *routing.ConfigLoader) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || !isImpressionRun(rc) {
			return next(ctx, rc)
		}

		rc.ImpressionRun = true

		// 优先使用账户级工具模型；无 override 时保留 routing middleware 选的默认路由
		if pool != nil && configLoader != nil {
			if resolution, ok := resolveAccountToolRoute(ctx, pool, rc.Run.AccountID, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes, configLoader, rc.RoutingByokEnabled); ok {
				rc.Gateway = resolution.Gateway
				rc.SelectedRoute = resolution.Selected
			}
		}

		provider := rc.MemoryProvider
		if provider == nil || rc.UserID == nil {
			slog.WarnContext(ctx, "impression: skipped, no memory provider or user")
			return next(ctx, rc)
		}

		ident := memory.MemoryIdentity{
			AccountID: rc.Run.AccountID,
			UserID:    *rc.UserID,
			AgentID:   StableAgentID(rc),
		}

		if source, ok := provider.(memory.MemoryFragmentSource); ok {
			if fragments, listed := buildSnapshotFromFragments(ctx, source, ident, impressionFragmentLimit); listed && len(fragments) > 0 {
				content := buildImpressionInputFromFragments(fragments)
				if strings.TrimSpace(content) == "" {
					return next(ctx, rc)
				}
				rc.Messages = append(rc.Messages, llm.Message{
					Role:    "user",
					Content: []llm.ContentPart{{Type: "text", Text: content}},
				})
				rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)
			}
		} else {
			skelCtx, skelCancel := context.WithTimeout(ctx, memorySkeletonTimeout)
			skeletonLines, leafLines, _, ok := buildSnapshotFromTree(skelCtx, provider, ident)
			skelCancel()

			if ok && (len(skeletonLines) > 0 || len(leafLines) > 0) {
				content := formatImpressionInput(skeletonLines, leafLines)
				rc.Messages = append(rc.Messages, llm.Message{
					Role:    "user",
					Content: []llm.ContentPart{{Type: "text", Text: content}},
				})
				rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)
			}
		}

		err := next(ctx, rc)

		if err == nil && impStore != nil && strings.TrimSpace(rc.FinalAssistantOutput) != "" {
			if uErr := impStore.Upsert(ctx, ident.AccountID, ident.UserID, ident.AgentID, rc.FinalAssistantOutput); uErr != nil {
				slog.WarnContext(ctx, "impression: upsert failed", "err", uErr.Error())
			} else {
				slog.InfoContext(ctx, "impression: updated",
					"account_id", ident.AccountID.String(),
					"user_id", ident.UserID.String(),
					"len", len(rc.FinalAssistantOutput),
				)
			}
		}

		return err
	}
}

func formatImpressionInput(skeletonLines, leafLines []string) string {
	var sb strings.Builder
	sb.WriteString("以下是 bot 的记忆数据，请基于这些信息生成画像。\n\n")
	sb.WriteString("## 记忆目录概览\n\n")
	for _, line := range skeletonLines {
		cleaned := strings.TrimSpace(line)
		if cleaned != "" {
			sb.WriteString(cleaned)
			sb.WriteString("\n\n")
		}
	}
	if len(leafLines) > 0 {
		sb.WriteString("## 记忆条目原文\n\n")
		for _, line := range leafLines {
			cleaned := strings.TrimSpace(line)
			if cleaned != "" {
				sb.WriteString("- ")
				sb.WriteString(cleaned)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func buildImpressionInputFromFragments(fragments []memory.MemoryFragment) string {
	var sb strings.Builder
	sb.WriteString("以下是 bot 的记忆条目种子，请先基于这些线索主动检索，再生成画像。URI 只用于后续工具调用，不要写入最终画像。\n\n")
	sb.WriteString("## 记忆条目\n")
	count := 0
	for _, fragment := range fragments {
		title := strings.TrimSpace(firstNonEmptyString(fragment.Title, compactInline(firstNonEmptyString(fragment.Content, fragment.Abstract), 100)))
		content := compactInline(firstNonEmptyString(fragment.Content, fragment.Abstract), 700)
		if title == "" && content == "" {
			continue
		}
		count++
		sb.WriteString("- 标题：")
		sb.WriteString(title)
		sb.WriteString("\n")
		writeImpressionField(&sb, "URI", fragment.URI)
		writeImpressionField(&sb, "时间", fragment.RecordedAt)
		writeImpressionField(&sb, "标签", strings.Join(fragment.Labels, ", "))
		writeImpressionField(&sb, "重要度", formatImpressionScore(fragment.Score))
		writeImpressionField(&sb, "摘要", compactInline(fragment.Abstract, 240))
		writeImpressionField(&sb, "内容", content)
	}
	if count == 0 {
		return ""
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func writeImpressionField(sb *strings.Builder, label, value string) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return
	}
	sb.WriteString("  ")
	sb.WriteString(label)
	sb.WriteString("：")
	sb.WriteString(cleaned)
	sb.WriteString("\n")
}

func formatImpressionScore(score float64) string {
	if score == 0 {
		return ""
	}
	formatted := strconv.FormatFloat(score, 'f', 3, 64)
	formatted = strings.TrimRight(formatted, "0")
	return strings.TrimRight(formatted, ".")
}
