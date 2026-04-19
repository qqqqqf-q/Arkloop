//go:build !desktop

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChannelDeliveryDrainOptions configures the background drainer.
type ChannelDeliveryDrainOptions struct {
	Telegram       *telegrambot.Client
	Discord        DiscordHTTPDoer
	DiscordAPIBase string
}

// ChannelDeliveryDrainer drains pending channel_delivery_outbox rows in the background.
type ChannelDeliveryDrainer struct {
	pool         *pgxpool.Pool
	opts         ChannelDeliveryDrainOptions
	stop         chan struct{}
	wg           sync.WaitGroup
	cleanupCount int
}

// derefUUID 将 *uuid.UUID 退化为 uuid.UUID（nil 视为 uuid.Nil）。
func derefUUID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

// NewChannelDeliveryDrainer creates a new drainer.
func NewChannelDeliveryDrainer(pool *pgxpool.Pool, opts ChannelDeliveryDrainOptions) *ChannelDeliveryDrainer {
	return &ChannelDeliveryDrainer{
		pool: pool,
		opts: opts,
		stop: make(chan struct{}),
	}
}

// Start begins the background drain loop.
func (d *ChannelDeliveryDrainer) Start(ctx context.Context) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.loop(ctx)
	}()
}

// Stop signals the drain loop to exit and waits for it to finish.
func (d *ChannelDeliveryDrainer) Stop() {
	close(d.stop)
	d.wg.Wait()
}

func (d *ChannelDeliveryDrainer) loop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stop:
			return
		case <-ticker.C:
			d.drain(ctx)
		}
	}
}

func (d *ChannelDeliveryDrainer) drain(ctx context.Context) {
	if d.pool == nil {
		return
	}
	outboxRepo := data.ChannelDeliveryOutboxRepository{}
	rows, err := outboxRepo.ListPendingForDrain(ctx, d.pool, 10)
	if err != nil {
		slog.WarnContext(ctx, "channel delivery drain list failed", "err", err.Error())
		return
	}

	repo := data.ChannelDeliveryRepository{}
	for _, row := range rows {
		payload, err := row.Payload()
		if err != nil {
			slog.WarnContext(ctx, "channel delivery drain payload parse failed", "outbox_id", row.ID, "err", err.Error())
			continue
		}
		channel, err := repo.GetChannel(ctx, d.pool, row.ChannelID)
		if err != nil || channel == nil {
			slog.WarnContext(ctx, "channel delivery drain channel lookup failed", "outbox_id", row.ID, "channel_id", row.ChannelID, "err", err)
			if handleErr := d.handleFailure(ctx, row, fmt.Errorf("channel not found: %v", row.ChannelID), outboxRepo); handleErr != nil {
				slog.WarnContext(ctx, "channel delivery drain handle failure failed", "outbox_id", row.ID, "err", handleErr.Error())
			}
			continue
		}
		if err := d.drainRecord(ctx, row, payload, channel, outboxRepo); err != nil {
			slog.WarnContext(ctx, "channel delivery drain record failed", "outbox_id", row.ID, "err", err.Error())
		}
	}

	d.cleanupCount++
	if d.cleanupCount >= outboxCleanupEveryRounds {
		d.cleanupCount = 0
		now := time.Now().UTC()
		if n, cerr := outboxRepo.Cleanup(ctx, d.pool, "sent", now.Add(-outboxSentRetention)); cerr != nil {
			slog.WarnContext(ctx, "channel delivery drain cleanup sent failed", "err", cerr.Error())
		} else if n > 0 {
			slog.InfoContext(ctx, "channel delivery drain cleanup sent", "deleted", n)
		}
		if n, cerr := outboxRepo.Cleanup(ctx, d.pool, "dead", now.Add(-outboxDeadRetention)); cerr != nil {
			slog.WarnContext(ctx, "channel delivery drain cleanup dead failed", "err", cerr.Error())
		} else if n > 0 {
			slog.InfoContext(ctx, "channel delivery drain cleanup dead", "deleted", n)
		}
	}
}

func (d *ChannelDeliveryDrainer) drainRecord(ctx context.Context, row data.ChannelDeliveryOutboxRecord, payload data.OutboxPayload, channel *data.DeliveryChannelRecord, outboxRepo data.ChannelDeliveryOutboxRepository) error {
	switch row.ChannelType {
	case "telegram":
		return d.drainTelegram(ctx, row, payload, channel, outboxRepo)
	case "discord":
		return d.drainDiscord(ctx, row, payload, channel, outboxRepo)
	case "qq":
		return d.drainQQ(ctx, row, payload, channel, outboxRepo)
	default:
		return fmt.Errorf("unsupported channel type: %s", row.ChannelType)
	}
}

func (d *ChannelDeliveryDrainer) drainTelegram(ctx context.Context, row data.ChannelDeliveryOutboxRecord, payload data.OutboxPayload, channel *data.DeliveryChannelRecord, outboxRepo data.ChannelDeliveryOutboxRepository) error {
	client := d.opts.Telegram
	if client == nil {
		client = telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	}
	sender := NewTelegramChannelSenderWithClient(client, channel.Token, resolveSegmentDelay())

	replyTo := d.replyRefFromPayload(payload)
	for i := row.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, d.pool, row.ID, i+1); err != nil {
				return fmt.Errorf("update progress failed: %w", err)
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  row.ChannelType,
			Conversation: ChannelConversationRef{Target: payload.PlatformChatID, ThreadID: payload.PlatformThreadID},
			ReplyTo:      ref,
		}, trimmed)
		if sendErr != nil {
			return d.handleFailure(ctx, row, sendErr, outboxRepo)
		}
		if len(messageIDs) > 0 {
			if payload.IsTerminalNotice {
				if _, err := d.recordTerminalNoticeSuccess(ctx, payload, row.ChannelID, row.ChannelType, ref, messageIDs, trimmed); err != nil {
					return d.handleFailure(ctx, row, err, outboxRepo)
				}
			} else {
				if err := d.recordDeliverySuccess(ctx, payload, row.ChannelID, row.ChannelType, ref, messageIDs); err != nil {
					return d.handleFailure(ctx, row, err, outboxRepo)
				}
			}
		}
		if err := outboxRepo.UpdateProgress(ctx, d.pool, row.ID, i+1); err != nil {
			return fmt.Errorf("update progress failed: %w", err)
		}
		row.SegmentsSent = i + 1
	}
	if err := outboxRepo.UpdateSent(ctx, d.pool, row.ID); err != nil {
		return fmt.Errorf("update sent failed: %w", err)
	}
	return nil
}

func (d *ChannelDeliveryDrainer) drainDiscord(ctx context.Context, row data.ChannelDeliveryOutboxRecord, payload data.OutboxPayload, channel *data.DeliveryChannelRecord, outboxRepo data.ChannelDeliveryOutboxRepository) error {
	client := d.opts.Discord
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	baseURL := strings.TrimSpace(d.opts.DiscordAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"))
	}
	sender := NewDiscordChannelSenderWithClient(client, baseURL, channel.Token, resolveSegmentDelay())

	replyTo := d.replyRefFromPayload(payload)
	for i := row.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, d.pool, row.ID, i+1); err != nil {
				return fmt.Errorf("update progress failed: %w", err)
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  row.ChannelType,
			Conversation: ChannelConversationRef{Target: payload.PlatformChatID, ThreadID: payload.PlatformThreadID},
			ReplyTo:      ref,
		}, trimmed)
		if sendErr != nil {
			return d.handleFailure(ctx, row, sendErr, outboxRepo)
		}
		if len(messageIDs) > 0 {
			if payload.IsTerminalNotice {
				if _, err := d.recordTerminalNoticeSuccess(ctx, payload, row.ChannelID, row.ChannelType, ref, messageIDs, trimmed); err != nil {
					return d.handleFailure(ctx, row, err, outboxRepo)
				}
			} else {
				if err := d.recordDeliverySuccess(ctx, payload, row.ChannelID, row.ChannelType, ref, messageIDs); err != nil {
					return d.handleFailure(ctx, row, err, outboxRepo)
				}
			}
		}
		if err := outboxRepo.UpdateProgress(ctx, d.pool, row.ID, i+1); err != nil {
			return fmt.Errorf("update progress failed: %w", err)
		}
		row.SegmentsSent = i + 1
	}
	if err := outboxRepo.UpdateSent(ctx, d.pool, row.ID); err != nil {
		return fmt.Errorf("update sent failed: %w", err)
	}
	return nil
}

func (d *ChannelDeliveryDrainer) drainQQ(ctx context.Context, row data.ChannelDeliveryOutboxRecord, payload data.OutboxPayload, channel *data.DeliveryChannelRecord, outboxRepo data.ChannelDeliveryOutboxRepository) error {
	obBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_ONEBOT_API_BASE_URL"))
	if obBaseURL == "" {
		obBaseURL = fmt.Sprintf("http://127.0.0.1:%d", resolveOneBotAPIPort(channel))
	}
	obToken := strings.TrimSpace(channel.Token)
	client := onebotclient.NewClient(obBaseURL, obToken, nil)
	sender := NewOneBotChannelSender(client, resolveSegmentDelay())

	metadata := map[string]any{}
	if payload.ConversationType == "group" {
		metadata["message_type"] = "group"
	}
	if payload.Metadata != nil {
		if t, ok := payload.Metadata["message_type"].(string); ok && t == "group" {
			metadata["message_type"] = "group"
		}
	}

	replyTo := d.replyRefFromPayload(payload)
	for i := row.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, d.pool, row.ID, i+1); err != nil {
				return fmt.Errorf("update progress failed: %w", err)
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  row.ChannelType,
			Conversation: ChannelConversationRef{Target: payload.PlatformChatID, ThreadID: payload.PlatformThreadID},
			ReplyTo:      ref,
			Metadata:     metadata,
		}, trimmed)
		if sendErr != nil {
			return d.handleFailure(ctx, row, sendErr, outboxRepo)
		}
		if len(messageIDs) > 0 {
			if payload.IsTerminalNotice {
				if _, err := d.recordTerminalNoticeSuccess(ctx, payload, row.ChannelID, row.ChannelType, ref, messageIDs, trimmed); err != nil {
					return d.handleFailure(ctx, row, err, outboxRepo)
				}
			} else {
				if err := d.recordDeliverySuccess(ctx, payload, row.ChannelID, row.ChannelType, ref, messageIDs); err != nil {
					return d.handleFailure(ctx, row, err, outboxRepo)
				}
			}
		}
		if err := outboxRepo.UpdateProgress(ctx, d.pool, row.ID, i+1); err != nil {
			return fmt.Errorf("update progress failed: %w", err)
		}
		row.SegmentsSent = i + 1
	}
	if err := outboxRepo.UpdateSent(ctx, d.pool, row.ID); err != nil {
		return fmt.Errorf("update sent failed: %w", err)
	}
	return nil
}

func (d *ChannelDeliveryDrainer) handleFailure(ctx context.Context, row data.ChannelDeliveryOutboxRecord, lastErr error, outboxRepo data.ChannelDeliveryOutboxRepository) error {
	attempts := row.Attempts + 1
	nextRetry := time.Now().UTC().Add(data.OutboxBackoffDelay(attempts))
	if attempts >= data.OutboxMaxAttempts {
		if err := outboxRepo.MarkDead(ctx, d.pool, row.ID, lastErr.Error()); err != nil {
			slog.ErrorContext(ctx, "channel delivery drain mark dead failed",
				"outbox_id", row.ID, "run_id", row.RunID, "err", err)
			return fmt.Errorf("mark dead: %w", errors.Join(lastErr, err))
		}
		slog.WarnContext(ctx, "channel delivery drain marked dead",
			"outbox_id", row.ID, "run_id", row.RunID, "attempts", attempts, "err", lastErr.Error())
		return lastErr
	}
	if err := outboxRepo.UpdateFailure(ctx, d.pool, row.ID, attempts, lastErr.Error(), nextRetry); err != nil {
		slog.ErrorContext(ctx, "channel delivery drain update failure failed",
			"outbox_id", row.ID, "run_id", row.RunID, "attempts", attempts, "err", err)
		return fmt.Errorf("update failure: %w", errors.Join(lastErr, err))
	}
	return fmt.Errorf("send failed: %w", lastErr)
}

func (d *ChannelDeliveryDrainer) replyRefFromPayload(payload data.OutboxPayload) *ChannelMessageRef {
	if payload.ChannelReplyOverrideID != "" {
		return &ChannelMessageRef{MessageID: payload.ChannelReplyOverrideID}
	}
	if payload.ReplyToMessageID != "" {
		return &ChannelMessageRef{MessageID: payload.ReplyToMessageID}
	}
	return nil
}

func (d *ChannelDeliveryDrainer) recordDeliverySuccess(ctx context.Context, payload data.OutboxPayload, channelID uuid.UUID, channelType string, ledgerParent *ChannelMessageRef, messageIDs []string) error {
	if d.pool == nil || len(messageIDs) == 0 {
		return nil
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	deliveryRepo := data.ChannelDeliveryRepository{}
	ledgerRepo := data.ChannelMessageLedgerRepository{}
	for _, messageID := range messageIDs {
		if err := deliveryRepo.RecordDelivery(
			ctx, tx,
			payload.RunID,
			derefUUID(payload.ThreadID),
			channelID,
			payload.PlatformChatID,
			messageID,
		); err != nil {
			return err
		}
		if err := ledgerRepo.Record(ctx, tx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               channelID,
			ChannelType:             channelType,
			Direction:               data.ChannelMessageDirectionOutbound,
			ThreadID:                payload.ThreadID,
			RunID:                   uuidPtr(payload.RunID),
			PlatformConversationID:  payload.PlatformChatID,
			PlatformMessageID:       messageID,
			PlatformParentMessageID: channelMessageIDPtr(ledgerParent),
			PlatformThreadID:        payload.PlatformThreadID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (d *ChannelDeliveryDrainer) recordTerminalNoticeSuccess(ctx context.Context, payload data.OutboxPayload, channelID uuid.UUID, channelType string, ledgerParent *ChannelMessageRef, messageIDs []string, text string) (*uuid.UUID, error) {
	if d.pool == nil || len(messageIDs) == 0 {
		return nil, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	messagesRepo := data.MessagesRepository{}
	deliveryRepo := data.ChannelDeliveryRepository{}
	ledgerRepo := data.ChannelMessageLedgerRepository{}

	messageID, err := messagesRepo.InsertAssistantMessageWithMetadata(
		ctx, tx,
		payload.AccountID, derefUUID(payload.ThreadID), payload.RunID,
		text, nil, false,
		map[string]any{
			"delivery_notice":     true,
			"terminal_notice":     true,
			"exclude_from_prompt": true,
		},
	)
	if err != nil {
		return nil, err
	}
	threadMessageID := uuidPtr(messageID)

	for _, platformMessageID := range messageIDs {
		if err := deliveryRepo.RecordDelivery(
			ctx, tx,
			payload.RunID,
			derefUUID(payload.ThreadID),
			channelID,
			payload.PlatformChatID,
			platformMessageID,
		); err != nil {
			return nil, err
		}
		if err := ledgerRepo.Record(ctx, tx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               channelID,
			ChannelType:             channelType,
			Direction:               data.ChannelMessageDirectionOutbound,
			ThreadID:                payload.ThreadID,
			RunID:                   uuidPtr(payload.RunID),
			PlatformConversationID:  payload.PlatformChatID,
			PlatformMessageID:       platformMessageID,
			PlatformParentMessageID: channelMessageIDPtr(ledgerParent),
			PlatformThreadID:        payload.PlatformThreadID,
			MessageID:               threadMessageID,
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return threadMessageID, nil
}

