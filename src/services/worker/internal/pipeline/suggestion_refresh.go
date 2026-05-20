package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"

	"arkloop/services/shared/runkind"

	"github.com/google/uuid"
)

// NewSuggestionRefreshFunc 构建一个通用的 SuggestionRefreshFunc。
func NewSuggestionRefreshFunc(deps ImpressionRefreshDeps) SuggestionRefreshFunc {
	return func(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) {
		go doSuggestionRefresh(context.Background(), deps, accountID, userID, mode)
	}
}

func doSuggestionRefresh(ctx context.Context, deps ImpressionRefreshDeps, accountID, userID uuid.UUID, mode string) {
	threadID := uuid.New()
	runID := uuid.New()
	traceID := uuid.NewString()

	var projectID uuid.UUID
	if err := deps.QueryRowScan(ctx,
		`SELECT id FROM projects WHERE account_id = $1 ORDER BY created_at ASC LIMIT 1`,
		[]any{accountID}, &projectID,
	); err != nil {
		slog.WarnContext(ctx, "suggestion: project lookup failed", "err", err.Error())
		return
	}

	if err := deps.ExecSQL(ctx,
		`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
		threadID, accountID, projectID,
	); err != nil {
		slog.WarnContext(ctx, "suggestion: create thread failed", "err", err.Error())
		return
	}

	startedData := map[string]any{
		"run_kind":   runkind.Suggestion,
		"persona_id": "suggestion-builder",
		"mode":       mode,
	}
	startedJSON, _ := json.Marshal(startedData)
	if err := deps.ExecSQL(ctx,
		`INSERT INTO runs (id, account_id, thread_id, status, created_by_user_id) VALUES ($1, $2, $3, 'running', $4)`,
		runID, accountID, threadID, userID,
	); err != nil {
		slog.WarnContext(ctx, "suggestion: create run failed", "err", err.Error())
		return
	}

	if err := deps.ExecSQL(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2)`,
		runID, string(startedJSON),
	); err != nil {
		slog.WarnContext(ctx, "suggestion: create run event failed", "err", err.Error())
		return
	}

	payload := map[string]any{
		"source":   "suggestion_refresh",
		"run_kind": runkind.Suggestion,
		"mode":     mode,
	}
	if err := deps.EnqueueRun(ctx, accountID, runID, traceID, "run.execute", payload); err != nil {
		slog.WarnContext(ctx, "suggestion: enqueue job failed", "err", err.Error())
		return
	}

	slog.InfoContext(ctx, "suggestion: refresh triggered",
		"account_id", accountID.String(),
		"user_id", userID.String(),
		"run_id", runID.String(),
		"mode", mode,
	)
}
