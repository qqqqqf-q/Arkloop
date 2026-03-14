package summarizethread

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	errorArgsInvalid = "tool.args_invalid"
	errorDBFailed    = "tool.db_failed"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "summarize_thread",
	Version:     "1",
	Description: "update the thread title with a short summary",
	RiskLevel:   tools.RiskLevelLow,
}

var LlmSpec = llm.ToolSpec{
	Name:        "summarize_thread",
	Description: stringPtr(sharedtoolmeta.Must("summarize_thread").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"maxLength":   50,
				"description": "new thread title, 5-10 words",
			},
		},
		"required":             []string{"title"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	Pool *pgxpool.Pool
	RDB  *redis.Client
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	if e.Pool == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorDBFailed,
				Message:    "database not available",
			},
			DurationMs: durationMs(started),
		}
	}

	title, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{
			Error:      argErr,
			DurationMs: durationMs(started),
		}
	}

	threadID := execCtx.ThreadID
	if threadID == nil || *threadID == uuid.Nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "thread context not available",
			},
			DurationMs: durationMs(started),
		}
	}

	_, err := e.Pool.Exec(ctx,
		`UPDATE threads SET title = $1 WHERE id = $2 AND deleted_at IS NULL`,
		title, *threadID,
	)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorDBFailed,
				Message:    "failed to update thread title",
			},
			DurationMs: durationMs(started),
		}
	}

	// 通过 run_events 表推送 SSE 通知
	emitTitleEvent(ctx, e.Pool, e.RDB, execCtx.RunID, *threadID, title)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"title": title,
		},
		DurationMs: durationMs(started),
	}
}

func emitTitleEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	runID uuid.UUID,
	threadID uuid.UUID,
	title string,
) {
	dataJSON := map[string]any{
		"thread_id": threadID.String(),
		"title":     title,
	}
	encoded, err := json.Marshal(dataJSON)
	if err != nil {
		return
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	var seq int64
	if _, err = tx.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		return
	}
	if err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq); err != nil {
		return
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, $2, $3, $4::jsonb)`,
		runID, seq, "thread.title.updated", string(encoded),
	)
	if err != nil {
		return
	}

	if err = tx.Commit(ctx); err != nil {
		return
	}

	pgChannel := fmt.Sprintf(`"run_events:%s"`, runID.String())
	_, _ = pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgChannel, "ping")
	if rdb != nil {
		rdbChannel := fmt.Sprintf("arkloop:sse:run_events:%s", runID.String())
		_, _ = rdb.Publish(ctx, rdbChannel, "ping").Result()
	}
}

func parseArgs(args map[string]any) (string, *tools.ExecutionError) {
	for key := range args {
		if key != "title" {
			return "", &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    fmt.Sprintf("unknown parameter: %s", key),
			}
		}
	}

	title, ok := args["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return "", &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "title must be a non-empty string",
		}
	}

	title = strings.TrimSpace(title)
	if len([]rune(title)) > 50 {
		title = string([]rune(title)[:50])
	}

	return title, nil
}

func stringPtr(s string) *string { return &s }

func durationMs(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}
