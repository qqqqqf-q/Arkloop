package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnqueueDeliveries 在 run 终态时为所有订阅了该事件的端点各创建一条投递记录并入队。
func EnqueueDeliveries(
	ctx context.Context,
	pool *pgxpool.Pool,
	q queue.JobQueue,
	orgID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	eventType string,
	runPayload map[string]any,
) error {
	endpoints, err := listEndpointsForEvent(ctx, pool, orgID, eventType)
	if err != nil {
		return fmt.Errorf("list webhook endpoints: %w", err)
	}
	if len(endpoints) == 0 {
		return nil
	}

	payloadBytes, err := json.Marshal(runPayload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	for _, ep := range endpoints {
		deliveryID, err := insertDelivery(ctx, pool, ep.ID, orgID, eventType, payloadBytes)
		if err != nil {
			slog.ErrorContext(ctx, "webhook: insert delivery record failed",
				"endpoint_id", ep.ID.String(),
				"org_id", orgID.String(),
				"error", err.Error(),
			)
			continue
		}

		jobPayload := map[string]any{
			"endpoint_id": ep.ID.String(),
			"delivery_id": deliveryID.String(),
			"event_type":  eventType,
			"payload":     runPayload,
		}
		if _, err := q.EnqueueRun(ctx, orgID, runID, traceID, DeliverJobType, jobPayload, nil); err != nil {
			slog.ErrorContext(ctx, "webhook: enqueue delivery job failed",
				"delivery_id", deliveryID.String(),
				"endpoint_id", ep.ID.String(),
				"error", err.Error(),
			)
			// 入队失败时将 delivery 标记为 failed，避免孤儿记录停留在 pending
			if markErr := markDeliveryFailed(ctx, pool, deliveryID, 0, nil, nil); markErr != nil {
				slog.ErrorContext(ctx, "webhook: mark delivery failed error",
					"delivery_id", deliveryID.String(),
					"error", markErr.Error(),
				)
			}
		}
	}
	return nil
}
