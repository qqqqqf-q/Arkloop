package pipeline

import (
	"context"
	"log/slog"
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
