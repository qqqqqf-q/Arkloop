package pipeline

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func IsActivityRecorderRun(rc *RunContext) bool {
	return isActivityRecorderRun(rc)
}

func isActivityRecorderRun(rc *RunContext) bool {
	if rc == nil {
		return false
	}
	if s, ok := stringField(rc.InputJSON, "run_kind"); ok && strings.EqualFold(s, runkind.ActivityRecorder) {
		return true
	}
	if s, ok := stringField(rc.JobPayload, "run_kind"); ok && strings.EqualFold(s, runkind.ActivityRecorder) {
		return true
	}
	return false
}

func NewActivityRecorderPrepareMiddleware(pool CompactPersistDB, auxGateway llm.Gateway, emitDebugEvents bool, configLoader *routing.ConfigLoader) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || !isActivityRecorderRun(rc) {
			return next(ctx, rc)
		}

		if pool != nil && configLoader != nil {
			if resolution, ok := resolveEntitlementRoute(ctx, pool, rc.Run.AccountID, "spawn.profile.tool", auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes, configLoader, rc.RoutingByokEnabled); ok {
				rc.Gateway = resolution.Gateway
				rc.SelectedRoute = resolution.Selected
			}
		}

		content := strings.TrimSpace(stringValue(rc.JobPayload["instruction"]))
		if content == "" {
			content = "执行 activity recorder 后台扫描。加载可用的数据源 skill 和 MCP 工具，读取近期桌面活动，只把具有长期价值的事实、偏好、项目上下文或重要事件写入 memory_write。完成前必须调用 activity_recorder_finish 记录本轮状态。不要写 Notebook，不要输出用户可见说明。"
		}
		rc.Messages = append(rc.Messages, llm.Message{
			Role:    "user",
			Content: []llm.ContentPart{{Type: "text", Text: content}},
		})
		rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)

		err := next(ctx, rc)
		updateActivityRecorderBuilderState(ctx, pool, rc, err)
		return err
	}
}

type activityRecorderStateExecDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func updateActivityRecorderBuilderState(ctx context.Context, pool CompactPersistDB, rc *RunContext, runErr error) {
	if pool == nil || rc == nil {
		return
	}
	execDB, ok := pool.(activityRecorderStateExecDB)
	if !ok {
		return
	}

	status := "completed"
	errorText := ""
	if runErr != nil {
		status = "failed"
		errorText = runErr.Error()
	}
	now := time.Now().UTC()
	args := []any{
		status,
		errorText,
		now,
		rc.Run.ID,
	}
	sql := `
		UPDATE activity_recorder_builder_state
		   SET running_run_id = NULL,
		       running_started_at = NULL,
		       last_run_status = $1,
		       last_error = $2,
		       updated_at = $3`
	if status == "completed" {
		if windowEnd, ok := activityRecorderWindowEnd(rc); ok {
			args = append(args, windowEnd)
			sql += `,
		       last_window_end_at = CASE
		           WHEN last_window_end_at IS NULL OR last_window_end_at < $5 THEN $5
		           ELSE last_window_end_at
		       END`
		}
	}
	sql += `
		 WHERE running_run_id = $4
		    OR last_run_id = $4`
	if _, err := execDB.Exec(ctx, sql, args...); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_state_update_failed", "error", err.Error(), "run_id", rc.Run.ID.String())
	}
}

func activityRecorderWindowEnd(rc *RunContext) (time.Time, bool) {
	for _, source := range []map[string]any{rc.JobPayload, rc.InputJSON} {
		raw := strings.TrimSpace(stringValue(source["window_end"]))
		if raw == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed.UTC(), true
			}
		}
	}
	return time.Time{}, false
}
