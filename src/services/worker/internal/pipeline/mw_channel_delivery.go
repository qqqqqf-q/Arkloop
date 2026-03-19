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

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewChannelDeliveryMiddleware(pool *pgxpool.Pool) RunMiddleware {
	repo := data.ChannelDeliveryRepository{}
	client := telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	segmentDelay := resolveSegmentDelay()

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

		segments := splitTelegramMessage(escapeTelegramMarkdownV2(output), 4096)
		for _, segment := range segments {
			req := telegrambot.SendMessageRequest{
				ChatID:    rc.ChannelContext.PlatformChatID,
				Text:      segment,
				ParseMode: "MarkdownV2",
			}
			if rc.ChannelContext.ReplyToMessageID != nil {
				req.ReplyToMessageID = *rc.ChannelContext.ReplyToMessageID
			}
			if rc.ChannelContext.MessageThreadID != nil {
				req.MessageThreadID = *rc.ChannelContext.MessageThreadID
			}
			sent, sendErr := client.SendMessage(ctx, channel.Token, req)
			if sendErr != nil {
				recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, sendErr)
				slog.WarnContext(ctx, "telegram channel delivery failed", "run_id", rc.Run.ID, "err", sendErr.Error())
				return err
			}
			if sent != nil && sent.MessageID != 0 {
				if recordErr := repo.RecordDelivery(
					ctx,
					pool,
					rc.Run.ID,
					rc.Run.ThreadID,
					rc.ChannelContext.ChannelID,
					rc.ChannelContext.PlatformChatID,
					strconv.FormatInt(sent.MessageID, 10),
				); recordErr != nil {
					recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, recordErr)
					slog.WarnContext(ctx, "telegram channel delivery record failed", "run_id", rc.Run.ID, "err", recordErr.Error())
				}
			}
			if segmentDelay > 0 {
				time.Sleep(segmentDelay)
			}
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
