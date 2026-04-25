package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChannelDeliveryMiddlewareOptions overrides Telegram HTTP client (tests inject httptest base URL).
type ChannelDeliveryMiddlewareOptions struct {
	Telegram       *telegrambot.Client
	Discord        DiscordHTTPDoer
	DiscordAPIBase string
	StickerStore   interface {
		Get(ctx context.Context, key string) ([]byte, error)
	}
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
	discordClient := opts.Discord
	if discordClient == nil {
		discordClient = &http.Client{Timeout: 10 * time.Second}
	}
	discordAPIBase := strings.TrimSpace(opts.DiscordAPIBase)
	if discordAPIBase == "" {
		discordAPIBase = strings.TrimSpace(os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"))
	}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		var preloaded *data.DeliveryChannelRecord
		var ux TelegramChannelUX
		var obUX OneBotChannelUX
		channelType := normalizedChannelTypeFromContext(rc)
		if pool != nil && rc != nil && rc.ChannelContext != nil && (channelType == "telegram" || channelType == "discord" || channelType == "qq") {
			ch, prefetchErr := repo.GetChannel(ctx, pool, rc.ChannelContext.ChannelID)
			if prefetchErr != nil {
				slog.WarnContext(ctx, "channel delivery prefetch failed", "run_id", rc.Run.ID, "err", prefetchErr.Error())
			} else if ch != nil {
				preloaded = ch
				if channelType == "telegram" {
					ux = ParseTelegramChannelUX(ch.ConfigJSON)
				}
				if channelType == "qq" {
					obUX = ParseOneBotChannelUX(ch.ConfigJSON)
				}
			}
		}

		streamMidCount := 0
		messagesRepo := data.MessagesRepository{}
		var streamFlush func(context.Context, string) error
		if preloaded != nil && pool != nil && rc != nil && rc.ChannelContext != nil && channelType == "telegram" &&
			tgClient != nil && strings.TrimSpace(preloaded.Token) != "" {
			sender := NewTelegramChannelSenderWithClient(tgClient, preloaded.Token, resolveSegmentDelay())
			streamFlush = func(ctx2 context.Context, text string) error {
				// heartbeat Turn 1 阶段不 stream
				if rc.HeartbeatRun && (rc.HeartbeatToolOutcome == nil || !rc.HeartbeatToolOutcome.Reply) {
					return nil
				}
				ids, sendErr := sender.SendText(ctx2, ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					ReplyTo:      nil,
				}, text)
				if sendErr != nil {
					return sendErr
				}
				if err := recordChannelDeliverySuccess(ctx2, pool, repo, ledgerRepo, rc, nil, ids, nil); err != nil {
					return err
				}
				if err := persistStreamChunkMessage(ctx2, pool, messagesRepo, rc, text); err != nil {
					slog.WarnContext(ctx2, "persist stream chunk message failed", "run_id", rc.Run.ID, "err", err.Error())
				}
				streamMidCount++
				return nil
			}
			rc.TelegramToolBoundaryFlush = streamFlush

			if ShouldShowTelegramProgress(rc) {
				tracker := NewTelegramProgressTracker(tgClient, preloaded.Token, ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
				}, telegramReplyReference(ctx, pool, rc))
				rc.TelegramProgressTracker = tracker
			}
		}

		// QQ 渠道流式投递
		if preloaded != nil && pool != nil && rc != nil && rc.ChannelContext != nil && channelType == "qq" {
			obBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_ONEBOT_API_BASE_URL"))
			if obBaseURL == "" {
				obBaseURL = fmt.Sprintf("http://127.0.0.1:%d", resolveOneBotAPIPort(preloaded))
			}
			obToken := strings.TrimSpace(preloaded.Token)
			obClient := onebotclient.NewClient(obBaseURL, obToken, nil)
			obSender := NewOneBotChannelSender(obClient, resolveSegmentDelay())

			metadata := map[string]any{}
			if rc.ChannelContext.ConversationType == "group" {
				metadata["message_type"] = "group"
			}

			streamFlush = func(ctx2 context.Context, text string) error {
				if rc.HeartbeatRun && (rc.HeartbeatToolOutcome == nil || !rc.HeartbeatToolOutcome.Reply) {
					return nil
				}
				ids, sendErr := obSender.SendText(ctx2, ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					Metadata:     metadata,
				}, text)
				if sendErr != nil {
					return sendErr
				}
				if err := recordChannelDeliverySuccess(ctx2, pool, repo, ledgerRepo, rc, nil, ids, nil); err != nil {
					return err
				}
				if err := persistStreamChunkMessage(ctx2, pool, messagesRepo, rc, text); err != nil {
					slog.WarnContext(ctx2, "persist stream chunk message failed", "run_id", rc.Run.ID, "err", err.Error())
				}
				streamMidCount++
				return nil
			}
			rc.TelegramToolBoundaryFlush = streamFlush
		}

		var stopTyping context.CancelFunc
		if preloaded != nil && ux.TypingIndicator && strings.TrimSpace(preloaded.Token) != "" && tgClient != nil && !IsHeartbeatRunContext(rc) {
			stopTyping = StartTelegramTypingRefresh(ctx, tgClient, preloaded.Token, rc.ChannelContext.Conversation.Target)
		}

		err := next(ctx, rc)
		if rc != nil {
			rc.TelegramToolBoundaryFlush = nil
			if rc.TelegramProgressTracker != nil {
				rc.TelegramProgressTracker.Finalize(ctx)
			}
		}
		if stopTyping != nil {
			stopTyping()
		}

		if err != nil || rc == nil || rc.ChannelContext == nil {
			return err
		}
		channelType = normalizedChannelTypeFromContext(rc)
		if pool == nil || (channelType != "telegram" && channelType != "discord" && channelType != "qq") {
			return err
		}
		finalOutput := strings.TrimSpace(rc.FinalAssistantOutput)
		finalOutputs := normalizedAssistantOutputs(rc.FinalAssistantOutputs, finalOutput)
		if ShouldSuppressHeartbeatOutput(rc, finalOutput) {
			return err
		}

		fullOut := finalOutput
		remainder := strings.TrimSpace(rc.TelegramStreamDeliveryRemainder)
		notice := strings.TrimSpace(rc.ChannelTerminalNotice)
		if fullOut == "" && remainder == "" && streamMidCount == 0 && notice == "" && len(rc.ChannelDeliverySegments) == 0 {
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
		if streamFlush != nil && streamMidCount > 0 {
			finalOutputs = normalizedAssistantOutputs(rc.FinalAssistantOutputs, "")
		}

		isTerminalNoticeOnly := strings.TrimSpace(finalOutput) == "" && notice != "" && strings.TrimSpace(output) != ""

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
		if rc.ChannelOutputDelivered {
			return err
		}
		if streamFlush != nil && streamMidCount > 0 && remainder == "" {
			return err
		}

		outboxRepo := data.ChannelDeliveryOutboxRepository{}
		payload := buildOutboxPayload(rc, channelType, output, finalOutputs, isTerminalNoticeOnly)
		outboxRecord, insertErr := outboxRepo.InsertPending(ctx, pool, rc.Run.ID, rc.ChannelContext.ChannelID, uuidPtr(rc.Run.ThreadID), channelType, data.OutboxKindMessage, payload)
		if insertErr != nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, insertErr)
			slog.WarnContext(ctx, "channel delivery outbox insert failed", "run_id", rc.Run.ID, "err", insertErr.Error())
			return err
		}

		var deliverErr error
		switch channelType {
		case "telegram":
			uxSend := ParseTelegramChannelUX(channel.ConfigJSON)
			deliverErr = inlineDeliverTelegramOutbox(ctx, pool, rc, tgClient, channel, outboxRecord, payload, outboxRepo, repo, ledgerRepo, messagesRepo, opts.StickerStore)
			if deliverErr == nil && strings.TrimSpace(uxSend.ReactionEmoji) != "" && tgClient != nil {
				MaybeTelegramInboundReaction(ctx, tgClient, channel.Token, rc, uxSend.ReactionEmoji)
			}
		case "discord":
			deliverErr = inlineDeliverDiscordOutbox(ctx, pool, rc, discordClient, discordAPIBase, channel, outboxRecord, payload, outboxRepo, repo, ledgerRepo, messagesRepo)
		case "qq":
			deliverErr = inlineDeliverOneBotOutbox(ctx, pool, rc, channel, outboxRecord, payload, outboxRepo, repo, ledgerRepo, messagesRepo)
			if deliverErr == nil && strings.TrimSpace(obUX.ReactionEmojiID) != "" {
				MaybeOneBotInboundReaction(ctx, channel, rc, obUX.ReactionEmojiID)
			}
		}

		if deliverErr != nil {
			recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, deliverErr)
			slog.WarnContext(ctx, "channel delivery inline try failed, will retry via drain", "run_id", rc.Run.ID, "outbox_id", outboxRecord.ID, "err", deliverErr.Error())
		}
		return err
	}
}

func buildOutboxPayload(rc *RunContext, channelType, output string, outputs []string, isTerminalNotice bool) data.OutboxPayload {
	payload := data.OutboxPayload{
		AccountID:        rc.Run.AccountID,
		RunID:            rc.Run.ID,
		ThreadID:         uuidPtr(rc.Run.ThreadID),
		PlatformChatID:   rc.ChannelContext.Conversation.Target,
		ConversationType: rc.ChannelContext.ConversationType,
		IsTerminalNotice: isTerminalNotice,
	}
	if rc.ChannelContext.Conversation.ThreadID != nil {
		payload.PlatformThreadID = rc.ChannelContext.Conversation.ThreadID
	}
	if rc.ChannelReplyOverride != nil {
		payload.ChannelReplyOverrideID = rc.ChannelReplyOverride.MessageID
	}

	switch channelType {
	case "telegram":
		if rc.ChannelReplyOverride != nil {
			payload.ReplyToMessageID = rc.ChannelReplyOverride.MessageID
		}
	case "discord":
		if rc.ChannelContext.TriggerMessage != nil && rc.ChannelContext.TriggerMessage.MessageID != "" {
			payload.ReplyToMessageID = rc.ChannelContext.TriggerMessage.MessageID
		} else if rc.ChannelContext.InboundMessage.MessageID != "" {
			payload.ReplyToMessageID = rc.ChannelContext.InboundMessage.MessageID
		}
	case "qq":
		if !rc.HeartbeatRun && !isPrivateChannelConversation(rc.ChannelContext.ConversationType) {
			if rc.ChannelContext.TriggerMessage != nil && rc.ChannelContext.TriggerMessage.MessageID != "" {
				payload.ReplyToMessageID = rc.ChannelContext.TriggerMessage.MessageID
			} else if rc.ChannelContext.InboundMessage.MessageID != "" {
				payload.ReplyToMessageID = rc.ChannelContext.InboundMessage.MessageID
			}
		}
	}

	if isTerminalNotice {
		payload.Outputs = []string{output}
	} else {
		payload.Outputs = outputs
	}
	if len(rc.ChannelDeliverySegments) > 0 {
		payload.Segments = append([]data.OutboxSegment(nil), rc.ChannelDeliverySegments...)
	}

	if channelType == "qq" {
		metadata := map[string]any{}
		if rc.ChannelContext.ConversationType == "group" {
			metadata["message_type"] = "group"
		}
		payload.Metadata = metadata
	}

	return payload
}

func inlineDeliverTelegramOutbox(
	ctx context.Context,
	pool *pgxpool.Pool,
	rc *RunContext,
	client *telegrambot.Client,
	channel *data.DeliveryChannelRecord,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	messagesRepo data.MessagesRepository,
	stickerStore interface {
		Get(ctx context.Context, key string) ([]byte, error)
	},
) error {
	if err := validateOutboxPayload(payload); err != nil {
		return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
	}
	sender := NewTelegramChannelSenderWithClient(client, channel.Token, resolveSegmentDelay())
	replyTo := telegramReplyReference(ctx, pool, rc)

	if len(payload.Segments) > 0 {
		for i := outboxRec.SegmentsSent; i < len(payload.Segments); i++ {
			segment := payload.Segments[i]
			ref := replyTo
			if i > 0 {
				ref = nil
			}
			var (
				messageIDs []string
				sendErr    error
				textBody   string
			)
			switch segment.Kind {
			case "sticker":
				messageIDs, sendErr = SendTelegramStickerByID(ctx, pool, stickerStore, client, channel.Token, ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					ReplyTo:      ref,
				}, payload.AccountID, segment.StickerID)
			default:
				textBody = strings.TrimSpace(segment.Text)
				if textBody == "" {
					if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
						return err
					}
					continue
				}
				messageIDs, sendErr = sender.SendText(ctx, ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					ReplyTo:      ref,
				}, textBody)
			}
			if sendErr != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, sendErr, outboxRepo)
			}
			if payload.IsTerminalNotice {
				_, err := recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, ref, messageIDs, textBody)
				if err != nil {
					return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
				}
			} else {
				if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, ref, messageIDs, nil); err != nil {
					return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
				}
			}
			if err := AdvanceOutboxProgress(ctx, pool, outboxRepo, outboxRec.ID, i+1, payload.AccountID, segment.StickerID); err != nil {
				return err
			}
			outboxRec.SegmentsSent = i + 1
		}
		return outboxRepo.UpdateSent(ctx, pool, outboxRec.ID)
	}

	for i := outboxRec.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
		}, trimmed)
		if sendErr != nil {
			return handleInlineOutboxFailure(ctx, pool, outboxRec, sendErr, outboxRepo)
		}
		if payload.IsTerminalNotice {
			_, err := recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, ref, messageIDs, trimmed)
			if err != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
			}
		} else {
			if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, ref, messageIDs, nil); err != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
			}
		}
		if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
			return err
		}
		outboxRec.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, pool, outboxRec.ID)
}

func inlineDeliverDiscordOutbox(
	ctx context.Context,
	pool *pgxpool.Pool,
	rc *RunContext,
	client DiscordHTTPDoer,
	baseURL string,
	channel *data.DeliveryChannelRecord,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	messagesRepo data.MessagesRepository,
) error {
	if err := validateOutboxPayload(payload); err != nil {
		return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
	}
	sender := NewDiscordChannelSenderWithClient(client, baseURL, channel.Token, resolveSegmentDelay())
	replyTo := discordReplyReference(rc)

	for i := outboxRec.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
		}, trimmed)
		if sendErr != nil {
			return handleInlineOutboxFailure(ctx, pool, outboxRec, sendErr, outboxRepo)
		}
		if payload.IsTerminalNotice {
			_, err := recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, ref, messageIDs, trimmed)
			if err != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
			}
		} else {
			if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, ref, messageIDs, nil); err != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
			}
		}
		if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
			return err
		}
		outboxRec.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, pool, outboxRec.ID)
}

func inlineDeliverOneBotOutbox(
	ctx context.Context,
	pool *pgxpool.Pool,
	rc *RunContext,
	channel *data.DeliveryChannelRecord,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	messagesRepo data.MessagesRepository,
) error {
	if err := validateOutboxPayload(payload); err != nil {
		return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
	}
	obBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_ONEBOT_API_BASE_URL"))
	if obBaseURL == "" {
		obBaseURL = fmt.Sprintf("http://127.0.0.1:%d", resolveOneBotAPIPort(channel))
	}
	obToken := strings.TrimSpace(channel.Token)
	client := onebotclient.NewClient(obBaseURL, obToken, nil)
	sender := NewOneBotChannelSender(client, resolveSegmentDelay())
	replyTo := onebotReplyReference(rc)
	metadata := payload.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	for i := outboxRec.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, sendErr := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
			Metadata:     metadata,
		}, trimmed)
		if sendErr != nil {
			return handleInlineOutboxFailure(ctx, pool, outboxRec, sendErr, outboxRepo)
		}
		if payload.IsTerminalNotice {
			_, err := recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, ref, messageIDs, trimmed)
			if err != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
			}
		} else {
			if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, ref, messageIDs, nil); err != nil {
				return handleInlineOutboxFailure(ctx, pool, outboxRec, err, outboxRepo)
			}
		}
		if err := outboxRepo.UpdateProgress(ctx, pool, outboxRec.ID, i+1); err != nil {
			return err
		}
		outboxRec.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, pool, outboxRec.ID)
}

func normalizedChannelTypeFromContext(rc *RunContext) string {
	if rc == nil || rc.ChannelContext == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(rc.ChannelContext.ChannelType))
}

func deliverTelegramChannelOutput(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	client *telegrambot.Client,
	channel *data.DeliveryChannelRecord,
	output string,
	threadMessageID *uuid.UUID,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	sender := NewTelegramChannelSenderWithClient(client, channel.Token, resolveSegmentDelay())
	replyTo := telegramReplyReference(ctx, pool, rc)
	messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
	}, output)
	if err != nil {
		return err
	}
	if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, replyTo, messageIDs, threadMessageID); err != nil {
		slog.WarnContext(ctx, "telegram channel delivery record failed", "run_id", rc.Run.ID, "err", err.Error())
		return err
	}
	return nil
}

func deliverTelegramTerminalNotice(
	ctx context.Context,
	pool *pgxpool.Pool,
	messagesRepo data.MessagesRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	client *telegrambot.Client,
	channel *data.DeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	sender := NewTelegramChannelSenderWithClient(client, channel.Token, resolveSegmentDelay())
	replyTo := telegramReplyReference(ctx, pool, rc)
	messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
	}, output)
	if err != nil {
		return err
	}
	_, err = recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, replyTo, messageIDs, output)
	return err
}

func deliverTelegramChannelOutputs(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	client *telegrambot.Client,
	channel *data.DeliveryChannelRecord,
	output string,
	outputs []string,
	threadMessageID *uuid.UUID,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	if len(outputs) <= 1 {
		return deliverTelegramChannelOutput(ctx, pool, deliveryRepo, ledgerRepo, rc, client, channel, output, threadMessageID)
	}
	sender := NewTelegramChannelSenderWithClient(client, channel.Token, resolveSegmentDelay())
	replyTo := telegramReplyReference(ctx, pool, rc)
	for i, item := range outputs {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
		}, trimmed)
		if err != nil {
			return err
		}
		if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, ref, messageIDs, threadMessageID); err != nil {
			slog.WarnContext(ctx, "telegram channel delivery record failed", "run_id", rc.Run.ID, "err", err.Error())
			return err
		}
	}
	return nil
}

func deliverDiscordChannelOutput(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	client DiscordHTTPDoer,
	baseURL string,
	channel *data.DeliveryChannelRecord,
	output string,
	threadMessageID *uuid.UUID,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	replyTo := discordReplyReference(rc)
	sender := NewDiscordChannelSenderWithClient(client, baseURL, channel.Token, resolveSegmentDelay())
	messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
	}, output)
	if err != nil {
		return err
	}
	if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, replyTo, messageIDs, threadMessageID); err != nil {
		slog.WarnContext(ctx, "discord channel delivery record failed", "run_id", rc.Run.ID, "err", err.Error())
		return err
	}
	return nil
}

func deliverDiscordTerminalNotice(
	ctx context.Context,
	pool *pgxpool.Pool,
	messagesRepo data.MessagesRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	client DiscordHTTPDoer,
	baseURL string,
	channel *data.DeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	replyTo := discordReplyReference(rc)
	sender := NewDiscordChannelSenderWithClient(client, baseURL, channel.Token, resolveSegmentDelay())
	messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
	}, output)
	if err != nil {
		return err
	}
	_, err = recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, replyTo, messageIDs, output)
	return err
}

func deliverOneBotChannelOutput(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	channel *data.DeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	obBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_ONEBOT_API_BASE_URL"))
	if obBaseURL == "" {
		obBaseURL = fmt.Sprintf("http://127.0.0.1:%d", resolveOneBotAPIPort(channel))
	}
	obToken := strings.TrimSpace(channel.Token)
	client := onebotclient.NewClient(obBaseURL, obToken, nil)
	sender := NewOneBotChannelSender(client, resolveSegmentDelay())

	replyTo := onebotReplyReference(rc)
	metadata := map[string]any{}
	if rc.ChannelContext.ConversationType == "group" {
		metadata["message_type"] = "group"
	}

	messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
		Metadata:     metadata,
	}, output)
	if err != nil {
		return err
	}
	if err := recordChannelDeliverySuccess(ctx, pool, deliveryRepo, ledgerRepo, rc, replyTo, messageIDs, nil); err != nil {
		slog.WarnContext(ctx, "qq channel delivery record failed", "run_id", rc.Run.ID, "err", err.Error())
		return err
	}
	return nil
}

func deliverOneBotTerminalNotice(
	ctx context.Context,
	pool *pgxpool.Pool,
	messagesRepo data.MessagesRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	channel *data.DeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	obBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_ONEBOT_API_BASE_URL"))
	if obBaseURL == "" {
		obBaseURL = fmt.Sprintf("http://127.0.0.1:%d", resolveOneBotAPIPort(channel))
	}
	obToken := strings.TrimSpace(channel.Token)
	client := onebotclient.NewClient(obBaseURL, obToken, nil)
	sender := NewOneBotChannelSender(client, resolveSegmentDelay())

	replyTo := onebotReplyReference(rc)
	metadata := map[string]any{}
	if rc.ChannelContext.ConversationType == "group" {
		metadata["message_type"] = "group"
	}

	messageIDs, err := sender.SendText(ctx, ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
		Metadata:     metadata,
	}, output)
	if err != nil {
		return err
	}
	_, err = recordChannelTerminalNoticeSuccess(ctx, pool, messagesRepo, deliveryRepo, ledgerRepo, rc, replyTo, messageIDs, output)
	return err
}

func normalizedAssistantOutputs(outputs []string, fallback string) []string {
	normalized := make([]string, 0, len(outputs))
	for _, item := range outputs {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) > 0 {
		return normalized
	}
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return []string{trimmed}
	}
	return nil
}

func recordChannelTerminalNoticeSuccess(
	ctx context.Context,
	pool *pgxpool.Pool,
	messagesRepo data.MessagesRepository,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	ledgerParent *ChannelMessageRef,
	platformMessageIDs []string,
	text string,
) (*uuid.UUID, error) {
	if pool == nil || rc == nil || rc.ChannelContext == nil || len(platformMessageIDs) == 0 {
		return nil, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	messageID, err := messagesRepo.InsertAssistantMessageWithMetadata(
		ctx, tx,
		rc.Run.AccountID, rc.Run.ThreadID, rc.Run.ID,
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

	for _, platformMessageID := range platformMessageIDs {
		if err := deliveryRepo.RecordDelivery(
			ctx,
			tx,
			rc.Run.ID,
			rc.Run.ThreadID,
			rc.ChannelContext.ChannelID,
			rc.ChannelContext.Conversation.Target,
			platformMessageID,
		); err != nil {
			return nil, err
		}
		if err := ledgerRepo.Record(ctx, tx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               rc.ChannelContext.ChannelID,
			ChannelType:             rc.ChannelContext.ChannelType,
			Direction:               data.ChannelMessageDirectionOutbound,
			ThreadID:                uuidPtr(rc.Run.ThreadID),
			RunID:                   uuidPtr(rc.Run.ID),
			PlatformConversationID:  rc.ChannelContext.Conversation.Target,
			PlatformMessageID:       platformMessageID,
			PlatformParentMessageID: channelMessageIDPtr(ledgerParent),
			PlatformThreadID:        rc.ChannelContext.Conversation.ThreadID,
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

// resolveOneBotAPIPort 从 channel 配置读取 OneBot HTTP 端口，默认 3000
func resolveOneBotAPIPort(channel *data.DeliveryChannelRecord) int {
	if channel == nil || len(channel.ConfigJSON) == 0 {
		return 3000
	}
	var cfg struct {
		OneBotPort int `json:"onebot_port"`
	}
	if json.Unmarshal(channel.ConfigJSON, &cfg) == nil && cfg.OneBotPort > 0 {
		return cfg.OneBotPort
	}
	return 3000
}

func discordReplyReference(rc *RunContext) *ChannelMessageRef {
	if rc == nil || rc.ChannelContext == nil {
		return nil
	}
	if rc.ChannelContext.TriggerMessage != nil && strings.TrimSpace(rc.ChannelContext.TriggerMessage.MessageID) != "" {
		return rc.ChannelContext.TriggerMessage
	}
	if strings.TrimSpace(rc.ChannelContext.InboundMessage.MessageID) == "" {
		return nil
	}
	ref := rc.ChannelContext.InboundMessage
	return &ref
}

func onebotReplyReference(rc *RunContext) *ChannelMessageRef {
	if rc == nil || rc.ChannelContext == nil {
		return nil
	}
	if rc.HeartbeatRun {
		return nil
	}
	if isPrivateChannelConversation(rc.ChannelContext.ConversationType) {
		return nil
	}
	if rc.ChannelContext.TriggerMessage != nil && strings.TrimSpace(rc.ChannelContext.TriggerMessage.MessageID) != "" {
		return rc.ChannelContext.TriggerMessage
	}
	if strings.TrimSpace(rc.ChannelContext.InboundMessage.MessageID) == "" {
		return nil
	}
	ref := rc.ChannelContext.InboundMessage
	return &ref
}

func telegramReplyReference(ctx context.Context, pool *pgxpool.Pool, rc *RunContext) *ChannelMessageRef {
	if rc == nil || rc.ChannelContext == nil {
		return nil
	}
	if rc.ChannelReplyOverride != nil {
		return rc.ChannelReplyOverride
	}
	return nil
}

func telegramRunAlreadyDelivered(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID) bool {
	if pool == nil || runID == uuid.Nil {
		return false
	}
	var exists bool
	err := pool.QueryRow(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			  FROM channel_message_ledger
			 WHERE run_id = $1
			   AND direction = 'outbound'
		)`,
		runID,
	).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

func ShouldShowTelegramProgress(rc *RunContext) bool {
	if rc == nil || rc.ChannelContext == nil {
		return false
	}
	return isPrivateChannelConversation(rc.ChannelContext.ConversationType)
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

// MaybeOneBotInboundReaction adds emoji reaction to the inbound QQ message (best effort).
func MaybeOneBotInboundReaction(ctx context.Context, channel *data.DeliveryChannelRecord, rc *RunContext, emojiID string) {
	if channel == nil || rc == nil || rc.ChannelContext == nil || strings.TrimSpace(emojiID) == "" {
		return
	}
	midStr := strings.TrimSpace(rc.ChannelContext.InboundMessage.MessageID)
	if midStr == "" {
		return
	}
	obBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_ONEBOT_API_BASE_URL"))
	if obBaseURL == "" {
		obBaseURL = fmt.Sprintf("http://127.0.0.1:%d", resolveOneBotAPIPort(channel))
	}
	client := onebotclient.NewClient(obBaseURL, strings.TrimSpace(channel.Token), nil)
	if err := client.SetMsgEmojiLike(ctx, midStr, strings.TrimSpace(emojiID)); err != nil {
		slog.WarnContext(ctx, "qq inbound reaction failed", "run_id", rc.Run.ID, "err", err.Error())
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
	defer func() { _ = tx.Rollback(context.Background()) }() //nolint:errcheck

	repo := data.RunEventsRepository{}
	if _, appendErr := repo.AppendEvent(context.Background(), tx, runID, "run.channel_delivery_failed", map[string]any{
		"error": err.Error(),
	}, nil, nil); appendErr != nil {
		return
	}
	if err := tx.Commit(context.Background()); err != nil {
		slog.Warn("channel_delivery_failure_commit_failed", "run_id", runID, "err", err)
	}
}

func recordChannelDeliverySuccess(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryRepo data.ChannelDeliveryRepository,
	ledgerRepo data.ChannelMessageLedgerRepository,
	rc *RunContext,
	ledgerParent *ChannelMessageRef,
	messageIDs []string,
	threadMessageID *uuid.UUID,
) error {
	if pool == nil || rc == nil || rc.ChannelContext == nil || len(messageIDs) == 0 {
		return nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

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
			MessageID:               threadMessageID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// TryDeliverChannelInjectionBlockNotice 在 Pipeline 于注入拦截处提前返回、未执行 ChannelDelivery 时，仍向渠道投递拦截说明。
func TryDeliverChannelInjectionBlockNotice(ctx context.Context, pool *pgxpool.Pool, rc *RunContext, notice string) {
	if ctx == nil {
		ctx = context.Background()
	}
	text := strings.TrimSpace(notice)
	if pool == nil || rc == nil || rc.ChannelContext == nil || text == "" {
		return
	}
	channelType := normalizedChannelTypeFromContext(rc)
	if channelType != "telegram" && channelType != "discord" && channelType != "qq" {
		return
	}
	repo := data.ChannelDeliveryRepository{}
	ledgerRepo := data.ChannelMessageLedgerRepository{}
	messagesRepo := data.MessagesRepository{}
	outboxRepo := data.ChannelDeliveryOutboxRepository{}
	channel, err := repo.GetChannel(ctx, pool, rc.ChannelContext.ChannelID)
	if err != nil || channel == nil {
		return
	}

	payload := buildOutboxPayload(rc, channelType, text, nil, true)
	outboxRecord, insertErr := outboxRepo.InsertPending(ctx, pool, rc.Run.ID, rc.ChannelContext.ChannelID, uuidPtr(rc.Run.ThreadID), channelType, data.OutboxKindInjectionBlockNotice, payload)
	if insertErr != nil {
		recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, insertErr)
		slog.WarnContext(ctx, "channel injection block notice outbox insert failed", "run_id", rc.Run.ID, "err", insertErr.Error())
		return
	}

	var deliverErr error
	switch channelType {
	case "telegram":
		if strings.TrimSpace(channel.Token) == "" {
			return
		}
		tgClient := optsTelegramClientOrDefault()
		deliverErr = deliverTelegramTerminalNotice(ctx, pool, messagesRepo, repo, ledgerRepo, rc, tgClient, channel, text)
		uxSend := ParseTelegramChannelUX(channel.ConfigJSON)
		if deliverErr == nil && strings.TrimSpace(uxSend.ReactionEmoji) != "" && tgClient != nil {
			MaybeTelegramInboundReaction(ctx, tgClient, channel.Token, rc, uxSend.ReactionEmoji)
		}
	case "discord":
		client := &http.Client{Timeout: 10 * time.Second}
		baseURL := strings.TrimSpace(os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"))
		deliverErr = deliverDiscordTerminalNotice(ctx, pool, messagesRepo, repo, ledgerRepo, rc, client, baseURL, channel, text)
	case "qq":
		deliverErr = deliverOneBotTerminalNotice(ctx, pool, messagesRepo, repo, ledgerRepo, rc, channel, text)
		uxSend := ParseOneBotChannelUX(channel.ConfigJSON)
		if deliverErr == nil && strings.TrimSpace(uxSend.ReactionEmojiID) != "" {
			MaybeOneBotInboundReaction(ctx, channel, rc, uxSend.ReactionEmojiID)
		}
	}
	if deliverErr != nil {
		recordChannelDeliveryFailure(ctx, pool, rc.Run.ID, deliverErr)
		if outboxRecord != nil {
			// 走统一 backoff/dead 逻辑，供 drainer 后续接管。
			if failErr := handleInlineOutboxFailure(ctx, pool, outboxRecord, deliverErr, outboxRepo); failErr != nil {
				slog.WarnContext(ctx, "channel injection block notice inline try failed, will retry via drain",
					"run_id", rc.Run.ID, "outbox_id", outboxRecord.ID, "err", failErr.Error())
			}
		} else {
			slog.WarnContext(ctx, "channel injection block notice failed", "run_id", rc.Run.ID, "channel_type", channelType, "err", deliverErr.Error())
		}
	} else if outboxRecord != nil {
		if updateErr := outboxRepo.UpdateSent(ctx, pool, outboxRecord.ID); updateErr != nil {
			slog.WarnContext(ctx, "channel injection block notice outbox update sent failed", "run_id", rc.Run.ID, "outbox_id", outboxRecord.ID, "err", updateErr.Error())
		}
	}
}

// TryDeliverTelegramInjectionBlockNotice 兼容旧调用点，现已支持所有渠道。
func TryDeliverTelegramInjectionBlockNotice(ctx context.Context, pool *pgxpool.Pool, rc *RunContext, notice string) {
	TryDeliverChannelInjectionBlockNotice(ctx, pool, rc, notice)
}

func optsTelegramClientOrDefault() *telegrambot.Client {
	return telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
}

func persistStreamChunkMessage(ctx context.Context, pool *pgxpool.Pool, repo data.MessagesRepository, rc *RunContext, text string) error {
	if pool == nil || rc == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck
	_, err = repo.InsertAssistantMessageWithMetadata(
		ctx, tx,
		rc.Run.AccountID, rc.Run.ThreadID, rc.Run.ID,
		text, nil, false,
		map[string]any{"stream_chunk": true},
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// terminal notice messages are persisted only after successful outbound delivery, in recordChannelTerminalNoticeSuccess.
