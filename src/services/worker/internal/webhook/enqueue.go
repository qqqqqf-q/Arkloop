package webhook

import (
	"context"
	"encoding/json"
	"fmt"

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
			// 单条记录插入失败不影响其他端点
			continue
		}

		jobPayload := map[string]any{
			"endpoint_id": ep.ID.String(),
			"delivery_id": deliveryID.String(),
			"event_type":  eventType,
			"payload":     runPayload,
		}
		_, _ = q.EnqueueRun(ctx, orgID, runID, traceID, DeliverJobType, jobPayload, nil)
	}
	return nil
}
