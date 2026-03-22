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

// ChannelDeliveryMiddlewareOptions overrides Telegram HTTP client (tests inject httptest base URL).
type ChannelDeliveryMiddlewareOptions struct {
	Telegram *telegrambot.Client
}

// NewChannelDeliveryMiddleware posts assistant output to Telegram and records deliveries.
func NewChannelDeliveryMiddleware(pool *pgxpool.Pool) RunMiddleware {
	return NewChannelDeliveryMiddlewareWithOptions(pool, ChannelDeliveryMiddlewareOptions{})
}

// NewChannelDeliveryMiddlewareWithOptions is like NewChannelDeliveryMiddleware but allows a custom Telegram client.
func NewChannelDeliveryMiddlewareWithOptions(pool *pgxpool.Pool, opts ChannelDeliveryMiddlewareOptions) RunMiddleware {
	repo := data.ChannelDeliveryRepository{}
	ledgerRepo := data.ChannelMessageLedgerRepository{}
	tgClient := opts.Telegram
	if tgClient == nil {
		tgClient = telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		var preloaded *data.DeliveryChannelRecord
		var ux TelegramChannelUX
		if pool != nil && rc != nil && rc.ChannelContext != nil && rc.ChannelContext.ChannelType == "telegram" {
			ch, prefetchErr := repo.GetChannel(ctx, pool, rc.ChannelContext.ChannelID)
			if prefetchErr != nil {
				slog.WarnContext(ctx, "channel delivery prefetch failed", "run_id", rc.Run.ID, "err", prefetchErr.Error())
			} else if ch != nil {
				preloaded = ch
				ux = ParseTelegramChannelUX(ch.ConfigJSON)
			}
		}

		streamMidCount := 0
		var streamFlush func(context.Context, string) error
		if preloaded != nil && pool != nil && rc != nil && rc.ChannelContext != nil && rc.ChannelContext.ChannelType == "telegram" &&
			tgClient != nil && strings.TrimSpace(preloaded.Token) != "" {
			sender := NewTelegramChannelSenderWithClient(tgClient, preloaded.Token, resolveSegmentDelay())
			streamFlush = func(ctx2 context.Context, text string) error {
				ids, sendErr := sender.SendText(ctx2, ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					ReplyTo:      nil,
				}, text)
				if sendErr != nil {
					return sendErr
				}
				if err := recordChannelDeliverySuccess(ctx2, pool, repo, ledgerRepo, rc, nil, ids); err != nil {
					return err
				}
				streamMidCount++
				return nil
			}
			rc.TelegramToolBoundaryFlush = streamFlush
		}

		var stopTyping context.CancelFunc
		if preloaded != nil && ux.TypingIndicator && strings.TrimSpace(preloaded.Token) != "" && tgClient != nil {
			stopTyping = StartTelegramTypingRefresh(ctx, tgClient, preloaded.Token, rc.ChannelContext.Conversation.Target)
		}

		err := next(ctx, rc)
		if rc != nil {
			rc.TelegramToolBoundaryFlush = nil
		}
		if stopTyping != nil {
			stopTyping()
		}

		if err != nil || rc == nil || rc.ChannelContext == nil {
			return err
		}
		if pool == nil || rc.ChannelContext.ChannelType != "telegram" {
			return err
		}

		fullOut := strings.TrimSpace(rc.FinalAssistantOutput)
		remainder := strings.TrimSpace(rc.TelegramStreamDeliveryRemainder)
		notice := strings.TrimSpace(rc.ChannelTerminalNotice)
		if fullOut == "" && remainder == "" && streamMidCount == 0 && notice == "" {
			return err
		}

		output := fullOut
		if streamFlush != nil {
			if remainder != "" {
				output = remainder
			} else if streamMidCount > 0 {
				output = ""
			} else {
				// Desktop 等仍用 desktopEventWriter：未写 remainder 时不能用空串覆盖整段输出
				output = fullOut
			}
		}
		if strings.TrimSpace(output) == "" && notice != "" {
			output = notice
		}

		channel := preloaded
		var lookupErr error
		if channel == nil {
			channel, lookupErr = repo.GetChannel(ctx, pool, rc.ChannelContext.ChannelID)
		}
		if lookupErr != nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, lookupErr)
			slog.WarnContext(ctx, "channel delivery lookup failed", "run_id", rc.Run.ID, "err", lookupErr.Error())
			return err
		}
		if channel == nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, fmt.Errorf("channel not found or inactive"))
			return err
		}

		uxSend := ParseTelegramChannelUX(channel.ConfigJSON)

		var finalRecordErr error
		if output != "" {
			sender := NewTelegramChannelSenderWithClient(tgClient, channel.Token, resolveSegmentDelay())
			messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
				ChannelType:  rc.ChannelContext.ChannelType,
				Conversation: rc.ChannelContext.Conversation,
				ReplyTo:      nil,
			}, output)
			if sendErr != nil {
				recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, sendErr)
				slog.WarnContext(ctx, "telegram channel delivery failed", "run_id", rc.Run.ID, "err", sendErr.Error())
				return err
			}
			finalRecordErr = recordChannelDeliverySuccess(ctx, pool, repo, ledgerRepo, rc, nil, messageIDs)
			if finalRecordErr != nil {
				recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, finalRecordErr)
				slog.WarnContext(ctx, "telegram channel delivery record failed", "run_id", rc.Run.ID, "err", finalRecordErr.Error())
			}
		}

		if finalRecordErr == nil && strings.TrimSpace(uxSend.ReactionEmoji) != "" && tgClient != nil {
			MaybeTelegramInboundReaction(ctx, tgClient, channel.Token, rc, uxSend.ReactionEmoji)
		}
		return err
	}
}

// StartTelegramTypingRefresh sends Telegram typing actions until cancel (about every 4s, first immediately).
func StartTelegramTypingRefresh(ctx context.Context, client *telegrambot.Client, token, chatID string) context.CancelFunc {
	if client == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(chatID) == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		send := func() {
			_ = client.SendChatAction(ctx, token, telegrambot.SendChatActionRequest{
				ChatID: strings.TrimSpace(chatID),
				Action: "typing",
			})
		}
		send()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				send()
			}
		}
	}()
	return cancel
}

// MaybeTelegramInboundReaction reacts to the triggering user message (best effort).
func MaybeTelegramInboundReaction(ctx context.Context, client *telegrambot.Client, token string, rc *RunContext, emoji string) {
	if client == nil || rc == nil || rc.ChannelContext == nil || strings.TrimSpace(emoji) == "" || strings.TrimSpace(token) == "" {
		return
	}
	midStr := strings.TrimSpace(rc.ChannelContext.InboundMessage.MessageID)
	if midStr == "" {
		return
	}
	mid, convErr := strconv.ParseInt(midStr, 10, 64)
	if convErr != nil {
		return
	}
	chatID := strings.TrimSpace(rc.ChannelContext.Conversation.Target)
	if chatID == "" {
		return
	}
	if err := client.SetMessageReaction(ctx, token, telegrambot.SetMessageReactionRequest{
		ChatID:    chatID,
		MessageID: mid,
		Reaction:  []telegrambot.MessageReactionEmoji{{Type: "emoji", Emoji: strings.TrimSpace(emoji)}},
	}); err != nil {
		slog.WarnContext(ctx, "telegram inbound reaction failed", "run_id", rc.Run.ID, "err", err.Error())
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
	ledgerParent *ChannelMessageRef,
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
			PlatformParentMessageID: channelMessageIDPtr(ledgerParent),
			PlatformThreadID:        rc.ChannelContext.Conversation.ThreadID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
