package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/llm"
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

// IsHeartbeatRunContext reports whether the run payload marks this turn as heartbeat.
func IsHeartbeatRunContext(rc *RunContext) bool {
	if rc == nil {
		return false
	}
	return rc.HeartbeatRun || isHeartbeatRun(rc.InputJSON, rc.JobPayload)
}

func IsHeartbeatDecisionToolName(toolName string) bool {
	return llm.CanonicalToolName(toolName) == heartbeattool.ToolName
}

// NewHeartbeatPrepareMiddleware 为心跳 run 构建尾部 user message（包含完整 heartbeat 指令），
// 注册 heartbeat_decision 工具并设置 tool_choice=specific。
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

		// 构建 runtime tail：保持控制面提示独立，避免污染会话历史与 prompt-cache snapshot。
		var sb strings.Builder
		sb.WriteString("[SYSTEM_HEARTBEAT_CHECK]\n")
		sb.WriteString(fmt.Sprintf("time_utc: %s\n", time.Now().UTC().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("interval_minutes: %d\n", interval))
		sb.WriteString(fmt.Sprintf("new_user_messages: %d\n", newUserMessages))
		if rc.PersonaDefinition != nil && strings.TrimSpace(rc.PersonaDefinition.HeartbeatMD) != "" {
			sb.WriteString("\n")
			sb.WriteString(strings.TrimSpace(rc.PersonaDefinition.HeartbeatMD))
			sb.WriteString("\n")
		}
		sb.WriteString("[/SYSTEM_HEARTBEAT_CHECK]")

		rc.UpsertPromptSegment(PromptSegment{
			Name:          "heartbeat.check",
			Target:        PromptTargetRuntimeTail,
			Role:          "user",
			Text:          sb.String(),
			Stability:     PromptStabilityVolatileTail,
			CacheEligible: false,
		})

		// SystemProtocolSnippet 保留在 system prefix：机制约束放 system 层
		rc.UpsertPromptSegment(PromptSegment{
			Name:          "heartbeat.system_protocol",
			Target:        PromptTargetSystemPrefix,
			Role:          "system",
			Text:          heartbeattool.SystemProtocolSnippet(),
			Stability:     PromptStabilitySessionPrefix,
			CacheEligible: true,
		})

		if rc.AllowlistSet == nil {
			rc.AllowlistSet = map[string]struct{}{}
		}
		rc.AllowlistSet[heartbeattool.ToolName] = struct{}{}

		// heartbeat_decision 必须在 core 层，否则被 splitToolSpecs 踢到 searchable 层 LLM 看不到
		if rc.PersonaDefinition != nil && len(rc.PersonaDefinition.CoreTools) > 0 && !containsToolName(rc.PersonaDefinition.CoreTools, heartbeattool.ToolName) {
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
		if !containsToolSpecName(rc.ToolSpecs, heartbeattool.ToolName) {
			rc.ToolSpecs = append(rc.ToolSpecs, heartbeattool.Spec)
		}

		rc.ToolChoice = &llm.ToolChoice{
			Mode:     "specific",
			ToolName: heartbeattool.ToolName,
		}

		return next(ctx, rc)
	}
}

func containsToolName(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}

func containsToolSpecName(specs []llm.ToolSpec, target string) bool {
	for _, spec := range specs {
		if spec.Name == target {
			return true
		}
	}
	return false
}
