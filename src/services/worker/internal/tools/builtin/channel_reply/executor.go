package channel_reply

import (
	"time"

	"arkloop/services/worker/internal/tools"
)

// Reply 返回 reply 工具的标准执行结果。
// 调用方负责从 args 中提取 reply_to_message_id 并做渠道特定的验证。
func Reply(replyToMessageID string, started time.Time) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }
	if replyToMessageID == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "reply_to_message_id is required",
			},
			DurationMs: ms(),
		}
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok":                  true,
			"reply_to_set":        true,
			"reply_to_message_id": replyToMessageID,
		},
		DurationMs: ms(),
	}
}
