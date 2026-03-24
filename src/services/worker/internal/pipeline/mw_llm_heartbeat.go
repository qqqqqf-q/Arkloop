package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/tools"
	heartbeattool "arkloop/services/worker/internal/tools/builtin/heartbeat_decision"

	"github.com/google/uuid"
)

// isHeartbeatRun checks whether run_kind=heartbeat is set in InputJSON or JobPayload.
func isHeartbeatRun(input, job map[string]any) bool {
	if s, ok := stringField(input, "run_kind"); ok && strings.EqualFold(s, runkind.Heartbeat) {
		return true
	}
	if s, ok := stringField(job, "run_kind"); ok && strings.EqualFold(s, runkind.Heartbeat) {
		return true
	}
	return false
}

func stringField(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

// heartbeatIntervalMinutes reads interval from InputJSON or JobPayload.
func heartbeatIntervalMinutes(input, job map[string]any) int {
	for _, m := range []map[string]any{input, job} {
		if m == nil {
			continue
		}
		if raw, ok := m["heartbeat_interval_minutes"]; ok {
			switch n := raw.(type) {
			case int:
				if n > 0 {
					return n
				}
			case float64:
				if int(n) > 0 {
					return int(n)
				}
			}
		}
	}
	return runkind.DefaultHeartbeatIntervalMinutes
}

// NewHeartbeatPrepareMiddleware 为心跳 run 注入合成 user 消息，并在 next 返回后将
// heartbeat_decision 工具报告的 memory_fragments 提交到 MemoryProvider。
// 非心跳 run 直接透传。
func NewHeartbeatPrepareMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil {
			return next(ctx, rc)
		}
		if !isHeartbeatRun(rc.InputJSON, rc.JobPayload) {
			return next(ctx, rc)
		}

		rc.HeartbeatRun = true
		interval := heartbeatIntervalMinutes(rc.InputJSON, rc.JobPayload)
		if rc.PersonaDefinition != nil && rc.PersonaDefinition.HeartbeatIntervalMinutes > 0 {
			interval = rc.PersonaDefinition.HeartbeatIntervalMinutes
		}

		var sb strings.Builder
		sb.WriteString("** 系统心跳 **\n")
		sb.WriteString(fmt.Sprintf("time_utc: %s\n", time.Now().UTC().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("interval_minutes: %d\n", interval))
		if rc.PersonaDefinition != nil && strings.TrimSpace(rc.PersonaDefinition.HeartbeatMD) != "" {
			sb.WriteString("\n---\n")
			sb.WriteString(strings.TrimSpace(rc.PersonaDefinition.HeartbeatMD))
			sb.WriteString("\n---\n")
		}
		sb.WriteString("\n这是一次系统自动触发的 heartbeat 检查。\n")
		sb.WriteString("如果没有需要用户关注的新事项，调用 `heartbeat_decision(reply_silent=true)`，不要输出占位文本。\n")
		sb.WriteString("如果有需要通知用户的事项，只输出最终要发给用户的正文，再调用 `heartbeat_decision(reply_silent=false)`。\n")
		sb.WriteString("如有值得长期记住的事实，再填写 `memory_fragments`。\n")

		rc.Messages = append(rc.Messages, llm.Message{
			Role:    "user",
			Content: []llm.ContentPart{{Type: "text", Text: sb.String()}},
		})
		rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)

		rc.SystemPrompt += "\n\n" + heartbeattool.SystemProtocolSnippet()

		if rc.AllowlistSet == nil {
			rc.AllowlistSet = map[string]struct{}{}
		}
		rc.AllowlistSet[heartbeattool.ToolName] = struct{}{}

		// heartbeat_decision 必须在 core 层，否则被 splitToolSpecs 踢到 searchable 层 LLM 看不到
		if rc.PersonaDefinition != nil && len(rc.PersonaDefinition.CoreTools) > 0 {
			rc.PersonaDefinition.CoreTools = append(rc.PersonaDefinition.CoreTools, heartbeattool.ToolName)
		}

		if rc.ToolRegistry != nil {
			if _, ok := rc.ToolRegistry.Get(heartbeattool.ToolName); !ok {
				if err := rc.ToolRegistry.Register(heartbeattool.AgentSpec); err != nil {
					return err
				}
			}
		}

		if rc.ToolExecutors == nil {
			rc.ToolExecutors = map[string]tools.Executor{}
		}
		rc.ToolExecutors[heartbeattool.ToolName] = heartbeattool.New()
		rc.ToolSpecs = append(rc.ToolSpecs, heartbeattool.Spec)

		err := next(ctx, rc)

		// memory_fragments 持久化（post-next）
		if err == nil &&
			rc.HeartbeatToolOutcome != nil &&
			len(rc.HeartbeatToolOutcome.Fragments) > 0 &&
			rc.MemoryProvider != nil &&
			rc.UserID != nil &&
			rc.Run.AccountID != uuid.Nil {
			commitHeartbeatFragments(ctx, rc)
		}

		return err
	}
}

func commitHeartbeatFragments(ctx context.Context, rc *RunContext) {
	agentID := "default"
	if rc.PersonaDefinition != nil && rc.PersonaDefinition.ID != "" {
		agentID = rc.PersonaDefinition.ID
	}
	ident := memory.MemoryIdentity{
		AccountID: rc.Run.AccountID,
		UserID:    *rc.UserID,
		AgentID:   agentID,
	}
	body := strings.Join(rc.HeartbeatToolOutcome.Fragments, "\n\n")
	msgs := []memory.MemoryMessage{
		{Role: "user", Content: body},
		{Role: "assistant", Content: "Noted."},
	}
	sessionID := rc.Run.ThreadID.String()
	if err := rc.MemoryProvider.AppendSessionMessages(ctx, ident, sessionID, msgs); err != nil {
		return
	}
	_ = rc.MemoryProvider.CommitSession(ctx, ident, sessionID)
}
