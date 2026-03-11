package runengine

import (
	"context"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/subagentctl"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func newSubAgentControl(pool *pgxpool.Pool, rdb *redis.Client, jobQueue queue.JobQueue, parentRun data.Run, traceID string) subagentctl.Control {
	return subagentctl.NewService(pool, rdb, jobQueue, parentRun, traceID)
}

func createAndEnqueueChildRun(
	ctx context.Context,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	jobQueue queue.JobQueue,
	childRunID uuid.UUID,
	parentRun data.Run,
	traceID string,
	personaID string,
	input string,
) error {
	return subagentctl.CreateInitialRun(ctx, pool, rdb, jobQueue, parentRun, traceID, childRunID, personaID, input)
}

func markChildRunFailed(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, childRunID uuid.UUID) {
	subagentctl.MarkRunFailed(ctx, pool, rdb, childRunID)
}

func markSubAgentRunning(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID) error {
	return subagentctl.MarkRunning(ctx, pool, runID)
}
