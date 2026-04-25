package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
)

// Outbox 清理与重试相关常量。
const (
	// outboxCleanupEveryRounds drainer 每过多少轮 drain 执行一次清理。
	// Drain 周期 10s，360 轮约等于 1 小时。
	outboxCleanupEveryRounds = 360
	// outboxSentRetention 已发送行保留时长。
	outboxSentRetention = 7 * 24 * time.Hour
	// outboxDeadRetention 死信行保留时长。
	outboxDeadRetention = 30 * 24 * time.Hour
)

var errOutboxPayloadEmpty = errors.New("channel delivery outbox payload has no deliverable content")

func validateOutboxPayload(payload data.OutboxPayload) error {
	if payload.HasDeliverableContent() {
		return nil
	}
	return errOutboxPayloadEmpty
}

func isDeliverableSegment(segment data.OutboxSegment) bool {
	if segment.Kind == "sticker" {
		return strings.TrimSpace(segment.StickerID) != ""
	}
	return strings.TrimSpace(segment.Text) != ""
}

func handleInlineOutboxFailure(
	ctx context.Context,
	pool data.ChannelDeliveryOutboxExecer,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	err error,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	attempts := outboxRec.Attempts + 1
	nextRetry := time.Now().UTC().Add(data.OutboxBackoffDelay(attempts))
	if attempts >= data.OutboxMaxAttempts {
		if deadErr := outboxRepo.MarkDead(ctx, pool, outboxRec.ID, err.Error()); deadErr != nil {
			slog.ErrorContext(ctx, "channel delivery outbox mark dead failed",
				"outbox_id", outboxRec.ID, "run_id", outboxRec.RunID, "err", deadErr)
			return fmt.Errorf("mark dead: %w", errors.Join(err, deadErr))
		}
		slog.WarnContext(ctx, "channel delivery outbox dead",
			"outbox_id", outboxRec.ID, "run_id", outboxRec.RunID, "attempts", attempts, "err", err.Error())
		return err
	}
	if updateErr := outboxRepo.UpdateFailure(ctx, pool, outboxRec.ID, attempts, err.Error(), nextRetry); updateErr != nil {
		slog.ErrorContext(ctx, "channel delivery outbox update failure failed",
			"outbox_id", outboxRec.ID, "run_id", outboxRec.RunID, "attempts", attempts, "err", updateErr)
		return fmt.Errorf("update failure: %w", errors.Join(err, updateErr))
	}
	return fmt.Errorf("%w; will retry via drain", err)
}
