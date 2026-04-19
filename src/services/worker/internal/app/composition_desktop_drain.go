//go:build desktop

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
)

func StartDesktopChannelDeliveryDrain(ctx context.Context, db data.DesktopDB) {
	go desktopChannelDeliveryDrainLoop(ctx, db)
}

func desktopChannelDeliveryDrainLoop(ctx context.Context, db data.DesktopDB) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	cleanupCount := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drainDesktopPending(ctx, db)
			cleanupCount++
			if cleanupCount >= outboxCleanupEveryRounds {
				cleanupCount = 0
				runDesktopOutboxCleanup(ctx, db)
			}
		}
	}
}

// drainDesktopPending 不再跨 HTTP 持有全局写锁；SQLite 写入由 pool 的 per-call
// write guard 管理，HTTP 发送阶段释放所有写入资源。
func drainDesktopPending(ctx context.Context, db data.DesktopDB) {
	outboxRepo := data.ChannelDeliveryOutboxRepository{}
	rows, err := outboxRepo.ListPendingForDrain(ctx, db, 10)
	if err != nil {
		slog.WarnContext(ctx, "desktop channel delivery drain list failed", "err", err.Error())
		return
	}
	if len(rows) == 0 {
		return
	}

	for _, row := range rows {
		payload, parseErr := row.Payload()
		if parseErr != nil {
			slog.WarnContext(ctx, "desktop channel delivery drain parse payload failed", "run_id", row.RunID, "err", parseErr.Error())
			continue
		}
		drainErr := drainDesktopOutboxRecord(ctx, db, row, payload, outboxRepo)
		if drainErr != nil {
			slog.WarnContext(ctx, "desktop channel delivery drain failed", "run_id", row.RunID, "err", drainErr.Error())
		}
	}
}

func runDesktopOutboxCleanup(ctx context.Context, db data.DesktopDB) {
	outboxRepo := data.ChannelDeliveryOutboxRepository{}
	now := time.Now().UTC()
	if n, err := outboxRepo.Cleanup(ctx, db, "sent", now.Add(-outboxSentRetention)); err != nil {
		slog.WarnContext(ctx, "desktop channel delivery cleanup sent failed", "err", err.Error())
	} else if n > 0 {
		slog.InfoContext(ctx, "desktop channel delivery cleanup sent", "deleted", n)
	}
	if n, err := outboxRepo.Cleanup(ctx, db, "dead", now.Add(-outboxDeadRetention)); err != nil {
		slog.WarnContext(ctx, "desktop channel delivery cleanup dead failed", "err", err.Error())
	} else if n > 0 {
		slog.InfoContext(ctx, "desktop channel delivery cleanup dead", "deleted", n)
	}
}

func drainDesktopOutboxRecord(
	ctx context.Context,
	db data.DesktopDB,
	row data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	switch strings.ToLower(strings.TrimSpace(row.ChannelType)) {
	case "telegram":
		return drainDesktopTelegramOutbox(ctx, db, row, payload, outboxRepo)
	case "discord":
		return drainDesktopDiscordOutbox(ctx, db, row, payload, outboxRepo)
	case "qq":
		return drainDesktopOneBotOutbox(ctx, db, row, payload, outboxRepo)
	default:
		return fmt.Errorf("unsupported channel type: %s", row.ChannelType)
	}
}

func drainDesktopTelegramOutbox(
	ctx context.Context,
	db data.DesktopDB,
	row data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	channel, err := loadDesktopDeliveryChannel(ctx, db, row.ChannelID)
	if err != nil || channel == nil {
		lastErr := errors.New("channel not found or inactive")
		if err != nil {
			lastErr = err
		}
		return handleDesktopDrainFailure(ctx, db, row, lastErr, outboxRepo)
	}

	client := telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	sender := pipeline.NewTelegramChannelSenderWithClient(client, channel.Token, 50*time.Millisecond)
	replyTo := telegramReplyReferenceFromPayload(payload)

	for i := row.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, db, row.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  row.ChannelType,
			Conversation: pipeline.ChannelConversationRef{Target: payload.PlatformChatID, ThreadID: payload.PlatformThreadID},
			ReplyTo:      ref,
		}, trimmed)
		if sendErr != nil {
			return handleDesktopDrainFailure(ctx, db, row, sendErr, outboxRepo)
		}
		if err := recordDesktopChannelDelivery(
			ctx, db, row.RunID, derefOutboxThreadID(row.ThreadID), row.ChannelID, row.ChannelType,
			payload.PlatformChatID, ref, payload.PlatformThreadID, messageIDs,
		); err != nil {
			return handleDesktopDrainFailure(ctx, db, row, err, outboxRepo)
		}
		if err := outboxRepo.UpdateProgress(ctx, db, row.ID, i+1); err != nil {
			return err
		}
		row.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, db, row.ID)
}

func drainDesktopDiscordOutbox(
	ctx context.Context,
	db data.DesktopDB,
	row data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	channel, err := loadDesktopDeliveryChannel(ctx, db, row.ChannelID)
	if err != nil || channel == nil {
		lastErr := errors.New("channel not found or inactive")
		if err != nil {
			lastErr = err
		}
		return handleDesktopDrainFailure(ctx, db, row, lastErr, outboxRepo)
	}

	discordClient := &http.Client{Timeout: 10 * time.Second}
	discordAPIBase := strings.TrimSpace(os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"))
	sender := pipeline.NewDiscordChannelSenderWithClient(discordClient, discordAPIBase, channel.Token, 50*time.Millisecond)
	replyTo := discordReplyReferenceFromPayload(payload)

	for i := row.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, db, row.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  row.ChannelType,
			Conversation: pipeline.ChannelConversationRef{Target: payload.PlatformChatID, ThreadID: payload.PlatformThreadID},
			ReplyTo:      ref,
		}, trimmed)
		if sendErr != nil {
			return handleDesktopDrainFailure(ctx, db, row, sendErr, outboxRepo)
		}
		if err := recordDesktopChannelDelivery(
			ctx, db, row.RunID, derefOutboxThreadID(row.ThreadID), row.ChannelID, row.ChannelType,
			payload.PlatformChatID, ref, payload.PlatformThreadID, messageIDs,
		); err != nil {
			return handleDesktopDrainFailure(ctx, db, row, err, outboxRepo)
		}
		if err := outboxRepo.UpdateProgress(ctx, db, row.ID, i+1); err != nil {
			return err
		}
		row.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, db, row.ID)
}

func drainDesktopOneBotOutbox(
	ctx context.Context,
	db data.DesktopDB,
	row data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	qCh, err := loadDesktopQQDeliveryChannel(ctx, db, row.ChannelID)
	if err != nil || qCh == nil {
		lastErr := errors.New("qq channel config not found")
		if err != nil {
			lastErr = err
		}
		return handleDesktopDrainFailure(ctx, db, row, lastErr, outboxRepo)
	}

	client := onebotclient.NewClient(qCh.OneBotHTTPURL, qCh.OneBotToken, nil)
	sender := pipeline.NewOneBotChannelSender(client, 50*time.Millisecond)
	replyTo := onebotReplyReferenceFromPayload(payload)
	metadata := payload.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	for i := row.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, db, row.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  row.ChannelType,
			Conversation: pipeline.ChannelConversationRef{Target: payload.PlatformChatID, ThreadID: payload.PlatformThreadID},
			ReplyTo:      ref,
			Metadata:     metadata,
		}, trimmed)
		if sendErr != nil {
			return handleDesktopDrainFailure(ctx, db, row, sendErr, outboxRepo)
		}
		if err := recordDesktopChannelDelivery(
			ctx, db, row.RunID, derefOutboxThreadID(row.ThreadID), row.ChannelID, row.ChannelType,
			payload.PlatformChatID, ref, payload.PlatformThreadID, messageIDs,
		); err != nil {
			return handleDesktopDrainFailure(ctx, db, row, err, outboxRepo)
		}
		if err := outboxRepo.UpdateProgress(ctx, db, row.ID, i+1); err != nil {
			return err
		}
		row.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, db, row.ID)
}

// handleDesktopDrainFailure 向调用方传播发送失败的真实错误；bookkeeping 失败时
// 用 errors.Join 合并两段错误，便于日志和上层定位。
func handleDesktopDrainFailure(
	ctx context.Context,
	db data.DesktopDB,
	row data.ChannelDeliveryOutboxRecord,
	lastErr error,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	attempts := row.Attempts + 1
	nextRetry := time.Now().UTC().Add(data.OutboxBackoffDelay(attempts))
	if attempts >= data.OutboxMaxAttempts {
		if err := outboxRepo.MarkDead(ctx, db, row.ID, lastErr.Error()); err != nil {
			slog.ErrorContext(ctx, "desktop drain mark dead failed",
				"outbox_id", row.ID, "run_id", row.RunID, "err", err.Error())
			return fmt.Errorf("mark dead: %w", errors.Join(lastErr, err))
		}
		slog.WarnContext(ctx, "desktop outbox marked dead",
			"outbox_id", row.ID, "run_id", row.RunID, "attempts", attempts, "err", lastErr.Error())
		return lastErr
	}
	if err := outboxRepo.UpdateFailure(ctx, db, row.ID, attempts, lastErr.Error(), nextRetry); err != nil {
		slog.ErrorContext(ctx, "desktop drain update failure failed",
			"outbox_id", row.ID, "run_id", row.RunID, "attempts", attempts, "err", err.Error())
		return fmt.Errorf("update failure: %w", errors.Join(lastErr, err))
	}
	return fmt.Errorf("send failed: %w", lastErr)
}

func derefOutboxThreadID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

func telegramReplyReferenceFromPayload(payload data.OutboxPayload) *pipeline.ChannelMessageRef {
	if strings.TrimSpace(payload.ReplyToMessageID) == "" {
		return nil
	}
	return &pipeline.ChannelMessageRef{MessageID: strings.TrimSpace(payload.ReplyToMessageID)}
}

func discordReplyReferenceFromPayload(payload data.OutboxPayload) *pipeline.ChannelMessageRef {
	if strings.TrimSpace(payload.ReplyToMessageID) == "" {
		return nil
	}
	return &pipeline.ChannelMessageRef{MessageID: strings.TrimSpace(payload.ReplyToMessageID)}
}

func onebotReplyReferenceFromPayload(payload data.OutboxPayload) *pipeline.ChannelMessageRef {
	if strings.TrimSpace(payload.ReplyToMessageID) == "" {
		return nil
	}
	return &pipeline.ChannelMessageRef{MessageID: strings.TrimSpace(payload.ReplyToMessageID)}
}
