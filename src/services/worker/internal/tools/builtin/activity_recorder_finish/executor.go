package activityrecorderfinish

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"
)

type ToolExecutor struct {
	DB data.DB
}

func NewToolExecutor(db data.DB) *ToolExecutor {
	return &ToolExecutor{DB: db}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	if e.DB == nil {
		return errResult("tool.not_configured", "activity recorder state database is not configured", started)
	}
	if execCtx.PersonaID != "activity-recorder-builder" {
		return errResult("tool.forbidden", "activity_recorder_finish is only available to Activity Recorder Builder", started)
	}
	if execCtx.AccountID == nil || execCtx.UserID == nil || execCtx.ProfileRef == "" || execCtx.WorkspaceRef == "" {
		return errResult("tool.execution_failed", "activity recorder run context is incomplete", started)
	}

	outcome, err := parseOutcome(args)
	if err != nil {
		return errResult("tool.args_invalid", err.Error(), started)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	checkedRaw, _ := json.Marshal(outcome.SourcesChecked)
	unavailableRaw, _ := json.Marshal(outcome.SourcesUnavailable)
	tag, err := e.DB.Exec(ctx, `
		UPDATE activity_recorder_builder_state
		   SET last_finish_status = $1,
		       last_finish_reason = $2,
		       last_sources_checked = $3,
		       last_sources_unavailable = $4,
		       last_memory_write_count = $5,
		       last_finished_at = $6,
		       updated_at = $7
		 WHERE account_id = $8
		   AND user_id = $9
		   AND profile_ref = $10
		   AND workspace_ref = $11
		   AND (running_run_id = $12 OR last_run_id = $12)`,
		outcome.Status,
		outcome.Reason,
		string(checkedRaw),
		string(unavailableRaw),
		outcome.MemoryWriteCount,
		nowText,
		now,
		execCtx.AccountID.String(),
		execCtx.UserID.String(),
		execCtx.ProfileRef,
		execCtx.WorkspaceRef,
		execCtx.RunID.String(),
	)
	if err != nil {
		return errResult("tool.execution_failed", err.Error(), started)
	}
	if tag.RowsAffected() == 0 {
		return errResult("tool.execution_failed", "activity recorder builder state row was not found", started)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok":                    true,
			"status":                outcome.Status,
			"reason":                outcome.Reason,
			"sources_checked":       outcome.SourcesChecked,
			"sources_unavailable":   outcome.SourcesUnavailable,
			"memory_write_count":    outcome.MemoryWriteCount,
			"activity_recorder_run": execCtx.RunID.String(),
		},
		DurationMs: durationMs(started),
	}
}

type finishOutcome struct {
	Status             string
	Reason             string
	SourcesChecked     []string
	SourcesUnavailable []string
	MemoryWriteCount   int
}

func parseOutcome(args map[string]any) (finishOutcome, error) {
	status := strings.TrimSpace(stringValue(args["status"]))
	if !validStatus(status) {
		return finishOutcome{}, fmt.Errorf("status must be one of memory_written, no_durable_memory, partial, source_unavailable, failed")
	}
	reason := strings.TrimSpace(stringValue(args["reason"]))
	if reason == "" {
		return finishOutcome{}, fmt.Errorf("reason is required")
	}
	checked, err := stringArray(args["sources_checked"])
	if err != nil {
		return finishOutcome{}, fmt.Errorf("sources_checked %w", err)
	}
	unavailable, err := stringArray(args["sources_unavailable"])
	if err != nil {
		return finishOutcome{}, fmt.Errorf("sources_unavailable %w", err)
	}
	count, err := intValue(args["memory_write_count"])
	if err != nil {
		return finishOutcome{}, fmt.Errorf("memory_write_count %w", err)
	}
	if count < 0 {
		return finishOutcome{}, fmt.Errorf("memory_write_count must be >= 0")
	}
	return finishOutcome{
		Status:             status,
		Reason:             reason,
		SourcesChecked:     checked,
		SourcesUnavailable: unavailable,
		MemoryWriteCount:   count,
	}, nil
}

func validStatus(status string) bool {
	switch status {
	case "memory_written", "no_durable_memory", "partial", "source_unavailable", "failed":
		return true
	default:
		return false
	}
}

func stringValue(raw any) string {
	if s, ok := raw.(string); ok {
		return s
	}
	return ""
}

func stringArray(raw any) ([]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		value := strings.TrimSpace(stringValue(item))
		if value == "" {
			return nil, fmt.Errorf("must contain only non-empty strings")
		}
		out = append(out, value)
	}
	return out, nil
}

func intValue(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		if value != float64(int(value)) {
			return 0, fmt.Errorf("must be an integer")
		}
		return int(value), nil
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}

func errResult(class, msg string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: class, Message: msg},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}
