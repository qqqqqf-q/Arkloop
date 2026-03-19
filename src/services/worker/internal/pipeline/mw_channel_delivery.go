package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewChannelDeliveryMiddleware(pool *pgxpool.Pool) RunMiddleware {
	repo := data.ChannelDeliveryRepository{}
	ledgerRepo := data.ChannelMessageLedgerRepository{}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		err := next(ctx, rc)
		if err != nil || rc == nil || rc.ChannelContext == nil {
			return err
		}
		output := strings.TrimSpace(rc.FinalAssistantOutput)
		if output == "" || pool == nil {
			return err
		}
		if rc.ChannelContext.ChannelType != "telegram" {
			return err
		}

		channel, lookupErr := repo.GetChannel(ctx, pool, rc.ChannelContext.ChannelID)
		if lookupErr != nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, lookupErr)
			slog.WarnContext(ctx, "channel delivery lookup failed", "run_id", rc.Run.ID, "err", lookupErr.Error())
			return err
		}
		if channel == nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, fmt.Errorf("channel not found or inactive"))
			return err
		}

		sender := NewTelegramChannelSender(channel.Token)
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      rc.ChannelContext.TriggerMessage,
		}, output)
		if sendErr != nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, sendErr)
			slog.WarnContext(ctx, "telegram channel delivery failed", "run_id", rc.Run.ID, "err", sendErr.Error())
			return err
		}
		if recordErr := recordChannelDeliverySuccess(ctx, pool, repo, ledgerRepo, rc, messageIDs); recordErr != nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, recordErr)
			slog.WarnContext(ctx, "telegram channel delivery record failed", "run_id", rc.Run.ID, "err", recordErr.Error())
		}
		return err
	}
}

func resolveSegmentDelay() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_MESSAGE_SEGMENT_DELAY_MS"))
	if raw == "" {
		return 50 * time.Millisecond
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 50 * time.Millisecond
	}
	return time.Duration(value) * time.Millisecond
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func channelMessageIDPtr(ref *ChannelMessageRef) *string {
	if ref == nil || strings.TrimSpace(ref.MessageID) == "" {
		return nil
	}
	value := strings.TrimSpace(ref.MessageID)
	return &value
}

func EscapeTelegramMarkdownV2(text string) string {
	return escapeTelegramMarkdownV2(text)
}

func escapeTelegramMarkdownV2(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func SplitTelegramMessage(text string, limit int) []string {
	return splitTelegramMessage(text, limit)
}

func splitTelegramMessage(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return []string{text}
	}

	var segments []string
	remaining := runes
	for len(remaining) > limit {
		cut := chooseTelegramSplitPoint(remaining, limit)
		segment := strings.TrimSpace(string(remaining[:cut]))
		if segment != "" {
			segments = append(segments, segment)
		}
		remaining = []rune(strings.TrimSpace(string(remaining[cut:])))
	}
	if len(remaining) > 0 {
		segments = append(segments, string(remaining))
	}
	return segments
}

func chooseTelegramSplitPoint(text []rune, limit int) int {
	window := string(text[:limit])
	for _, marker := range []string{"\n\n", "\n", "。", ".", "!", "?"} {
		if idx := strings.LastIndex(window, marker); idx > 0 {
			return utf8.RuneCountInString(window[:idx+len(marker)])
		}
	}
	return limit
}

func recordChannelDeliveryFailure(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID, err error) {
	if pool == nil || runID == uuid.Nil || err == nil {
		return
	}
	tx, txErr := pool.BeginTx(context.Background(), pgx.TxOptions{})
	if txErr != nil {
		return
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	repo := data.RunEventsRepository{}
	if _, appendErr := repo.AppendEvent(context.Background(), tx, runID, "run.channel_delivery_failed", map[string]any{
		"error": err.Error(),
	}, nil, nil); appendErr != nil {
		return
	}
	_ = tx.Commit(context.Background())
}

func recordChannelDeliverySuccess(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	messageIDs []string,
) error {
	if pool == nil || rc == nil || rc.ChannelContext == nil || len(messageIDs) == 0 {
		return nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, messageID := range messageIDs {
		if err := deliveryRepo.RecordDelivery(
			ctx,
			tx,
			rc.Run.ID,
			rc.Run.ThreadID,
			rc.ChannelContext.ChannelID,
			rc.ChannelContext.Conversation.Target,
			messageID,
		); err != nil {
			return err
		}
		if err := ledgerRepo.Record(ctx, tx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               rc.ChannelContext.ChannelID,
			ChannelType:             rc.ChannelContext.ChannelType,
			Direction:               data.ChannelMessageDirectionOutbound,
			ThreadID:                uuidPtr(rc.Run.ThreadID),
			RunID:                   uuidPtr(rc.Run.ID),
			PlatformConversationID:  rc.ChannelContext.Conversation.Target,
			PlatformMessageID:       messageID,
			PlatformParentMessageID: channelMessageIDPtr(rc.ChannelContext.TriggerMessage),
			PlatformThreadID:        rc.ChannelContext.Conversation.ThreadID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
