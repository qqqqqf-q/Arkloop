package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/tools"
	heartbeattool "arkloop/services/worker/internal/tools/builtin/heartbeat_decision"

	"github.com/google/uuid"
)

const (
	eventTypeMemoryHeartbeatStarted      = "memory.heartbeat.started"
	eventTypeMemoryHeartbeatAppendFailed = "memory.heartbeat.append_failed"
	eventTypeMemoryHeartbeatCommitFailed = "memory.heartbeat.commit_failed"
	eventTypeMemoryHeartbeatCommitted    = "memory.heartbeat.committed"
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

// IsHeartbeatRunContext reports whether the run payload marks this turn as heartbeat.
func IsHeartbeatRunContext(rc *RunContext) bool {
	if rc == nil {
		return false
	}
	return rc.HeartbeatRun || isHeartbeatRun(rc.InputJSON, rc.JobPayload)
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

		// 统计上次 assistant 回复后新增的真实 user 消息数
		lastAssistantIdx := -1
		for i := len(rc.Messages) - 1; i >= 0; i-- {
			if i < len(rc.ThreadMessageIDs) && rc.ThreadMessageIDs[i] != uuid.Nil && rc.Messages[i].Role == "assistant" {
				lastAssistantIdx = i
				break
			}
		}
		newUserMessages := 0
		for i := lastAssistantIdx + 1; i < len(rc.Messages); i++ {
			if i < len(rc.ThreadMessageIDs) && rc.ThreadMessageIDs[i] != uuid.Nil && rc.Messages[i].Role == "user" {
				newUserMessages++
			}
		}

		var sb strings.Builder
		sb.WriteString("** 系统心跳 **\n")
		sb.WriteString(fmt.Sprintf("time_utc: %s\n", time.Now().UTC().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("interval_minutes: %d\n", interval))
		sb.WriteString(fmt.Sprintf("new_user_messages: %d\n", newUserMessages))

		rc.Messages = append(rc.Messages, llm.Message{
			Role:    "user",
			Content: []llm.ContentPart{{Type: "text", Text: sb.String()}},
		})
		rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)

		if rc.PersonaDefinition != nil && strings.TrimSpace(rc.PersonaDefinition.HeartbeatMD) != "" {
			rc.SystemPrompt = appendSystemPromptBlock(rc.SystemPrompt, rc.PersonaDefinition.HeartbeatMD)
		}
		rc.SystemPrompt = appendSystemPromptBlock(rc.SystemPrompt, heartbeattool.SystemProtocolSnippet())

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

func appendSystemPromptBlock(base string, block string) string {
	trimmedBlock := strings.TrimSpace(block)
	if trimmedBlock == "" {
		return base
	}
	trimmedBase := strings.TrimSpace(base)
	if trimmedBase == "" {
		return trimmedBlock
	}
	return trimmedBase + "\n\n" + trimmedBlock
}

func commitHeartbeatFragments(ctx context.Context, rc *RunContext) {
	ident := memory.MemoryIdentity{
		AccountID: rc.Run.AccountID,
		UserID:    *rc.UserID,
		AgentID:   StableAgentID(rc),
	}
	body := strings.Join(rc.HeartbeatToolOutcome.Fragments, "\n\n")
	msgs := []memory.MemoryMessage{
		{Role: "user", Content: body},
		{Role: "assistant", Content: "Noted."},
	}
	sessionID := rc.Run.ThreadID.String()
	appendAsyncRunEvent(context.Background(), rc.Pool, rc.Run.ID, events.NewEmitter(rc.TraceID).Emit(eventTypeMemoryHeartbeatStarted, map[string]any{
		"kind":          "heartbeat",
		"session_id":    sessionID,
		"message_count": 1,
	}, nil, nil))
	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), memoryFlushTimeout)
		defer cancel()

		if err := rc.MemoryProvider.AppendSessionMessages(runCtx, ident, sessionID, msgs); err != nil {
			slog.Warn("memory: heartbeat append failed",
				"account_id", rc.Run.AccountID.String(),
				"session_id", sessionID,
				"err", err.Error(),
			)
			appendAsyncRunEvent(context.Background(), rc.Pool, rc.Run.ID, events.NewEmitter(rc.TraceID).Emit(eventTypeMemoryHeartbeatAppendFailed, map[string]any{
				"kind":       "heartbeat",
				"session_id": sessionID,
				"message":    err.Error(),
			}, nil, nil))
			return
		}
		if err := rc.MemoryProvider.CommitSession(runCtx, ident, sessionID); err != nil {
			slog.Warn("memory: heartbeat commit failed",
				"account_id", rc.Run.AccountID.String(),
				"session_id", sessionID,
				"err", err.Error(),
			)
			appendAsyncRunEvent(context.Background(), rc.Pool, rc.Run.ID, events.NewEmitter(rc.TraceID).Emit(eventTypeMemoryHeartbeatCommitFailed, map[string]any{
				"kind":       "heartbeat",
				"session_id": sessionID,
				"message":    err.Error(),
			}, nil, nil))
			return
		}
		appendAsyncRunEvent(context.Background(), rc.Pool, rc.Run.ID, events.NewEmitter(rc.TraceID).Emit(eventTypeMemoryHeartbeatCommitted, map[string]any{
			"kind":       "heartbeat",
			"session_id": sessionID,
		}, nil, nil))
		if rc.Pool != nil && strings.TrimSpace(body) != "" {
			scheduleSnapshotRefresh(rc.MemoryProvider, rc.Pool, rc.Run.ID, rc.TraceID, ident, sessionID, map[string][]string{
				string(memory.MemoryScopeUser): {body},
			}, "memory.heartbeat", "heartbeat")
		}
	}()
}
