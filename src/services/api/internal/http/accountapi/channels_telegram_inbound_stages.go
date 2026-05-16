package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errInboundDispatchDeferred = errors.New("channel inbound dispatch deferred")

type telegramInboundStageAResult struct {
	finalState  string
	replyText   string
	replyMarkup *telegrambot.InlineKeyboardMarkup
	cancelRunID uuid.UUID
}

func telegramInboundBaseMetadata(incoming telegramIncomingMessage) map[string]any {
	return map[string]any{
		inboundLedgerKeySource:           "telegram",
		inboundLedgerKeyConversationType: incoming.ChatType,
		inboundLedgerKeyMentionsBot:      incoming.MentionsBot,
		inboundLedgerKeyIsReplyToBot:     incoming.IsReplyToBot,
	}
}

func (c telegramConnector) persistTelegramInboundStageA(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	token string,
	cfg telegramChannelConfig,
	update telegramUpdate,
	incoming telegramIncomingMessage,
	persona *data.Persona,
) (*telegramInboundStageAResult, error) {
	now := time.Now().UTC()
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	committed := false
	commitTx := func() error {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		committed = true
		return nil
	}

	identity, err := upsertTelegramIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), update.Message.From)
	if err != nil {
		return nil, err
	}
	baseMetadata := telegramInboundBaseMetadata(incoming)
	dispatchAfterUnixMs := nextInboundBurstDispatchAfter(now)

	var groupIdentity *data.ChannelIdentity
	if !incoming.IsPrivate() && isTelegramGroupLikeChatType(incoming.ChatType) {
		gi, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(
			ctx,
			incoming.ChannelType,
			incoming.PlatformChatID,
			nil,
			nil,
			nil,
		)
		if err != nil {
			return nil, err
		}
		groupIdentity = &gi
	}

	if !incoming.HasContent() {
		if err := commitTx(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	claimed, stageResult, err := c.claimTelegramInboundStageA(ctx, tx, ch, incoming, &identity.ID, baseMetadata, dispatchAfterUnixMs)
	if err != nil {
		return nil, err
	}
	if !claimed {
		if err := commitTx(); err != nil {
			return nil, err
		}
		return stageResult, nil
	}

	// 消息到达 -> 递减 delay 重置 heartbeat cooldown（仅群聊）
	var pendingHeartbeatNotify bool
	var pendingInboundBurstNotify bool
	defer func() {
		if !committed || c.bus == nil {
			return
		}
		if pendingHeartbeatNotify {
			_ = c.bus.Publish(ctx, pgnotify.ChannelHeartbeat, "")
		}
		if pendingInboundBurstNotify {
			notifyChannelInboundBurst(ctx, c.bus)
		}
	}()
	if !incoming.IsPrivate() && groupIdentity != nil {
		reset, resetErr := resetGroupHeartbeatCooldownForMessage(
			ctx, tx,
			c.scheduledTriggersRepo,
			c.channelGroupThreadsRepo,
			ch,
			ch.PersonaID,
			incoming.PlatformChatID,
			now,
		)
		if resetErr != nil {
			slog.WarnContext(ctx, "heartbeat_cooldown_reset_failed", "error", resetErr, "channel_id", ch.ID, "identity_id", groupIdentity.ID)
		} else if reset {
			if c.bus != nil {
				pendingHeartbeatNotify = true
			} else {
				_, _ = tx.Exec(ctx, "SELECT pg_notify($1, '')", pgnotify.ChannelHeartbeat)
			}
		}
	}

	if incoming.IsPrivate() {
		trimmedCommandText := strings.TrimSpace(incoming.CommandText)
		allowedPrivateLink, err := allowTelegramPrivateChannelLink(ctx, tx, ch.ID, identity, trimmedCommandText, c.channelIdentityLinksRepo)
		if err != nil {
			return nil, err
		}
		if !allowedPrivateLink {
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &identity.ID, nil, nil, inboundStateIgnoredUnlinked, baseMetadata); err != nil {
				return nil, err
			}
			if err := commitTx(); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{finalState: inboundStateIgnoredUnlinked}, nil
		}
		handled, replyText, prefResult, personaResult, cancelRunID, err := DispatchChannelCommand(
			ctx, tx, ch, *persona, identity,
			trimmedCommandText, true, incoming.PlatformChatID,
			c.entitlementSvc,
			ChannelCommandResolver{
				ResolveThreadID: func(ctx context.Context, tx pgx.Tx, personaID, projectID uuid.UUID, isPrivate bool, chatID string) (uuid.UUID, error) {
					return c.resolveTelegramThreadID(ctx, tx, ch, personaID, projectID, identity, incoming)
				},
				ResolveStartPayload: func() string {
					parts := strings.Fields(trimmedCommandText)
					if len(parts) > 1 && parts[0] == "/start" {
						return parts[1]
					}
					return ""
				},
				BindCode: func() string {
					parts := strings.Fields(trimmedCommandText)
					if len(parts) >= 2 && parts[0] == "/bind" {
						return parts[1]
					}
					return ""
				},
			},
			ChannelCommandDeps{
				ChannelIdentitiesRepo:    c.channelIdentitiesRepo,
				ChannelDMThreadsRepo:     c.channelDMThreadsRepo,
				ChannelGroupThreadsRepo:  c.channelGroupThreadsRepo,
				PersonasRepo:             c.personasRepo,
				RunEventRepo:             c.runEventRepo,
				ChannelBindCodesRepo:     c.channelBindCodesRepo,
				ChannelIdentityLinksRepo: c.channelIdentityLinksRepo,
				ThreadRepo:               c.threadRepo,
			},
			"Telegram",
		)
		if err != nil {
			return nil, err
		}
		if handled {
			var replyMarkup *telegrambot.InlineKeyboardMarkup
			if prefResult != nil {
				replyMarkup = buildPreferenceKeyboard(prefResult)
			} else if personaResult != nil {
				replyMarkup = buildPersonaKeyboard(personaResult)
			}
			if cancelRunID != uuid.Nil {
				_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, cancelRunID.String())
			}
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &identity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
				return nil, err
			}
			if err := commitTx(); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{
				finalState:  inboundStateCommandHandled,
				replyText:   replyText,
				replyMarkup: replyMarkup,
			}, nil
		}
	}

	if !incoming.IsPrivate() && isTelegramGroupLikeChatType(incoming.ChatType) && c.channelGroupThreadsRepo != nil {
		cmd, cmdText, ok := adaptTelegramGroupCommandText(strings.TrimSpace(incoming.CommandText), cfg.BotUsername)
		if ok {
			if cmd == "/reset" {
				cmdText = "/new"
			}
			commandIdentity := identity
			handled, replyText, prefResult, personaResult, cancelRunID, err := DispatchChannelCommand(
				ctx, tx, ch, *persona, commandIdentity,
				cmdText, false, incoming.PlatformChatID,
				c.entitlementSvc,
				ChannelCommandResolver{
					ResolveThreadID: func(ctx context.Context, tx pgx.Tx, personaID, projectID uuid.UUID, isPrivate bool, chatID string) (uuid.UUID, error) {
						return c.resolveTelegramThreadID(ctx, tx, ch, personaID, projectID, identity, incoming)
					},
					ResolveHeartbeatIdentity: func(ctx context.Context, tx pgx.Tx) (*data.ChannelIdentity, error) {
						return groupIdentity, nil
					},
					IsBoundAdmin: func(ctx context.Context) bool {
						return telegramChannelIdentityIsBound(ctx, tx, ch.ID, identity, c.channelIdentityLinksRepo)
					},
					BindCode: func() string {
						parts := strings.Fields(cmdText)
						if len(parts) >= 2 && parts[0] == "/bind" {
							return parts[1]
						}
						return ""
					},
				},
				ChannelCommandDeps{
					ChannelIdentitiesRepo:    c.channelIdentitiesRepo,
					ChannelDMThreadsRepo:     c.channelDMThreadsRepo,
					ChannelGroupThreadsRepo:  c.channelGroupThreadsRepo,
					PersonasRepo:             c.personasRepo,
					RunEventRepo:             c.runEventRepo,
					ChannelBindCodesRepo:     c.channelBindCodesRepo,
					ChannelIdentityLinksRepo: c.channelIdentityLinksRepo,
					ThreadRepo:               c.threadRepo,
				},
				"Telegram",
			)
			if err != nil {
				return nil, err
			}
			if handled {
				var replyMarkup *telegrambot.InlineKeyboardMarkup
				if prefResult != nil {
					replyMarkup = buildPreferenceKeyboard(prefResult)
				} else if personaResult != nil {
					replyMarkup = buildPersonaKeyboard(personaResult)
				}
				if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &commandIdentity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
					return nil, err
				}
				if err := commitTx(); err != nil {
					return nil, err
				}
				if cancelRunID != uuid.Nil {
					_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, cancelRunID.String())
				}
				return &telegramInboundStageAResult{finalState: inboundStateCommandHandled, replyText: replyText, replyMarkup: replyMarkup}, nil
			}
		}
		var finalState string
		// keyword / @mention / reply-to-bot: fall through to run creation path
		if incoming.ShouldCreateRun() {
			goto createRun
		}
		_, finalState, err = c.persistTelegramGroupPassiveMessageTx(ctx, tx, ch, token, incoming, identity, persona, baseMetadata)
		if err != nil {
			return nil, err
		}
		if finalState == inboundStatePendingDispatch {
			pendingInboundBurstNotify = true
			if err := commitTx(); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{finalState: inboundStatePendingDispatch}, nil
		}
		slog.InfoContext(ctx, "telegram_inbound_processed",
			"stage", finalState,
			"channel_id", ch.ID.String(),
			"account_id", ch.AccountID.String(),
			"platform_chat_id", incoming.PlatformChatID,
			"platform_message_id", incoming.PlatformMsgID,
			"conversation_type", incoming.ChatType,
			"mentions_bot", incoming.MentionsBot,
			"is_reply_to_bot", incoming.IsReplyToBot,
		)
		if err := commitTx(); err != nil {
			return nil, err
		}
		return &telegramInboundStageAResult{finalState: finalState}, nil
	}

createRun:
	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, incoming)
	if err != nil {
		return nil, err
	}
	if err := ensureInboundThreadChatModel(ctx, tx, ch.AccountID, threadID, extractChannelDefaultModel(ch)); err != nil {
		return nil, err
	}
	timeCtx := c.resolveInboundTimeContext(ctx, ch, identity, incoming)
	content, contentJSON, metadataJSON, stickers, err := buildTelegramStructuredMessageWithMediaAndStickers(
		ctx,
		c.telegramClient,
		c.attachmentStore,
		token,
		ch.AccountID,
		threadID,
		identity.UserID,
		identity,
		incoming,
		timeCtx,
	)
	if err != nil {
		return nil, err
	}
	preTailMsg, err := c.messageRepo.WithTx(tx).GetLatestVisibleMessage(ctx, ch.AccountID, threadID)
	if err != nil {
		return nil, err
	}
	if preTailMsg != nil {
		baseMetadata[inboundMetadataPreTailKey] = preTailMsg.ID.String()
	}
	msg, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(
		ctx,
		ch.AccountID,
		threadID,
		"user",
		content,
		contentJSON,
		metadataJSON,
		identity.UserID,
	)
	if err != nil {
		return nil, err
	}
	if err := c.maybeCollectTelegramStickersTx(ctx, tx, ch, &identity.ID, stickers); err != nil {
		slog.WarnContext(ctx, "telegram_sticker_collect_failed",
			"channel_id", ch.ID,
			"thread_id", threadID,
			"message_id", incoming.PlatformMsgID,
			"err", err,
		)
	}
	if _, err := c.channelLedgerRepo.WithTx(tx).UpdateInboundEntry(
		ctx,
		ch.ID,
		incoming.PlatformChatID,
		incoming.PlatformMsgID,
		&threadID,
		nil,
		&msg.ID,
		applyInboundBurstMetadata(inboundLedgerMetadata(baseMetadata, inboundStatePendingDispatch), dispatchAfterUnixMs),
	); err != nil {
		return nil, err
	}
	ledgerRepoTx := c.channelLedgerRepo.WithTx(tx)
	if err := promoteRecentPassiveInboundToPendingTx(ctx, ledgerRepoTx, ch.ID, threadID, now); err != nil {
		return nil, err
	}
	if err := extendPendingInboundBurstWindowTx(ctx, ledgerRepoTx, ch.ID, threadID, now); err != nil {
		return nil, err
	}
	pendingInboundBurstNotify = true
	if err := commitTx(); err != nil {
		return nil, err
	}
	return &telegramInboundStageAResult{finalState: inboundStatePendingDispatch}, nil
}

func (c telegramConnector) continueTelegramInboundDispatch(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	personaRef string,
	entry data.ChannelInboundLedgerEntry,
) error {
	if c.channelLedgerRepo == nil {
		return nil
	}
	if entry.ThreadID == nil {
		return nil
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	entries, err := listPendingInboundBatchTx(ctx, c.channelLedgerRepo.WithTx(tx), ch.ID, *entry.ThreadID)
	if err != nil {
		return err
	}
	if !pendingBatchReady(entries, time.Now().UTC()) {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return errInboundDispatchDeferred
	}
	latestEntry, err := latestPendingBatchEntry(entries)
	if err != nil {
		return nil
	}
	if latestEntry.ThreadID == nil || latestEntry.MessageID == nil || latestEntry.SenderChannelIdentityID == nil {
		return fmt.Errorf("telegram inbound ledger incomplete for dispatch")
	}

	msg, err := c.messageRepo.GetByID(ctx, ch.AccountID, *latestEntry.ThreadID, *latestEntry.MessageID)
	if err != nil {
		return err
	}
	if msg == nil {
		return fmt.Errorf("telegram inbound message missing")
	}

	runRepoTx := c.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, *latestEntry.ThreadID); err != nil {
		return err
	}
	activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, *latestEntry.ThreadID)
	if err != nil {
		return err
	}
	if activeRun != nil {
		state, delivered, err := deliverPendingBatchToActiveRunTx(
			ctx,
			ch,
			runRepoTx,
			c.messageRepo,
			c.channelLedgerRepo.WithTx(tx),
			activeRun,
			entries,
			traceID,
		)
		if err != nil {
			return err
		}
		if state != "" {
			if err := markPendingBatchStateTx(ctx, c.channelLedgerRepo.WithTx(tx), ch.ID, entries, &activeRun.ID, state); err != nil {
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		if delivered {
			c.notifyActiveRunInput(ctx, activeRun.ID)
			return nil
		}
		return errInboundDispatchDeferred
	}

	incomingFromLedger := buildTelegramIncomingFromLedger(latestEntry)
	dispatchResult, err := DispatchInbound(ctx, tx, InboundDispatchRequest{
		TraceID:             traceID,
		Channel:             ch,
		PersonaRef:          personaRef,
		Identity:            data.ChannelIdentity{ID: *latestEntry.SenderChannelIdentityID},
		Incoming:            incomingFromLedger,
		ThreadID:            *latestEntry.ThreadID,
		MessageID:           *latestEntry.MessageID,
		InputContent:        strings.TrimSpace(msg.Content),
		ThreadTailMessageID: latestEntry.MessageID.String(),
		Source:              "telegram",
		ForceActive:         true,
		RunEventRepo:        c.runEventRepo,
		JobRepo:             c.jobRepo,
	})
	if err != nil {
		return err
	}
	if dispatchResult.FinalState != inboundStateEnqueuedNewRun {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.WarnContext(ctx, "telegram_inbound_processed",
			"stage", dispatchResult.FinalState,
			"channel_id", ch.ID.String(),
			"account_id", ch.AccountID.String(),
			"thread_id", latestEntry.ThreadID.String(),
			"platform_chat_id", latestEntry.PlatformConversationID,
			"platform_message_id", latestEntry.PlatformMessageID,
		)
		return errInboundDispatchDeferred
	}
	if err := markPendingBatchEnqueuedTx(ctx, c.channelLedgerRepo.WithTx(tx), ch.ID, entries, dispatchResult.RunID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.InfoContext(ctx, "telegram_inbound_processed",
		"stage", inboundStateEnqueuedNewRun,
		"channel_id", ch.ID.String(),
		"account_id", ch.AccountID.String(),
		"run_id", dispatchResult.RunID.String(),
		"thread_id", latestEntry.ThreadID.String(),
		"platform_chat_id", latestEntry.PlatformConversationID,
		"platform_message_id", latestEntry.PlatformMessageID,
	)
	return nil
}

func buildTelegramIncomingFromLedger(entry data.ChannelInboundLedgerEntry) telegramIncomingMessage {
	incoming := telegramIncomingMessage{
		ChannelID:       entry.ChannelID,
		ChannelType:     entry.ChannelType,
		PlatformChatID:  strings.TrimSpace(entry.PlatformConversationID),
		PlatformMsgID:   strings.TrimSpace(entry.PlatformMessageID),
		ReplyToMsgID:    entry.PlatformParentMessageID,
		MessageThreadID: entry.PlatformThreadID,
	}
	if chatType, ok := inboundLedgerString(entry.MetadataJSON, inboundLedgerKeyConversationType); ok {
		incoming.ChatType = strings.TrimSpace(chatType)
	}
	if mentionsBot, ok := inboundLedgerBool(entry.MetadataJSON, inboundLedgerKeyMentionsBot); ok {
		incoming.MentionsBot = mentionsBot
	}
	if replyToBot, ok := inboundLedgerBool(entry.MetadataJSON, inboundLedgerKeyIsReplyToBot); ok {
		incoming.IsReplyToBot = replyToBot
	}
	return incoming
}

func (c telegramConnector) maybeCancelTelegramHeartbeatRun(
	ctx context.Context,
	runRepo *data.RunEventRepository,
	runID uuid.UUID,
	metadata json.RawMessage,
) error {
	if runID == uuid.Nil {
		return nil
	}
	events, err := runRepo.ListEvents(ctx, runID, 0, 1)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	startedData, ok := events[0].DataJSON.(map[string]any)
	if !ok {
		return nil
	}
	runKind, _ := startedData["run_kind"].(string)
	if !strings.EqualFold(strings.TrimSpace(runKind), "heartbeat") {
		return nil
	}
	heartbeatTail, _ := startedData["thread_tail_message_id"].(string)
	heartbeatTail = strings.TrimSpace(heartbeatTail)
	preTail, _ := inboundLedgerString(metadata, inboundMetadataPreTailKey)
	if heartbeatTail == "" || preTail == "" || heartbeatTail != strings.TrimSpace(preTail) {
		return nil
	}
	if c.channelLedgerRepo != nil {
		hasOutbound, err := c.channelLedgerRepo.HasOutboundForRun(ctx, runID)
		if err != nil {
			return err
		}
		if hasOutbound {
			return nil
		}
	}
	_, _ = runRepo.RequestCancel(ctx, runID, nil, "heartbeat_superseded", 0, nil)
	return nil
}

func (c telegramConnector) claimTelegramInboundStageA(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	incoming telegramIncomingMessage,
	identityID *uuid.UUID,
	baseMetadata map[string]any,
	dispatchAfterUnixMs int64,
) (bool, *telegramInboundStageAResult, error) {
	accepted, err := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:               ch.ID,
		ChannelType:             ch.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		PlatformConversationID:  incoming.PlatformChatID,
		PlatformMessageID:       incoming.PlatformMsgID,
		PlatformParentMessageID: incoming.ReplyToMsgID,
		PlatformThreadID:        incoming.MessageThreadID,
		SenderChannelIdentityID: identityID,
		MetadataJSON:            applyInboundBurstMetadata(inboundLedgerMetadata(baseMetadata, inboundStatePendingDispatch), dispatchAfterUnixMs),
	})
	if err != nil {
		return false, nil, err
	}
	if accepted {
		return true, nil, nil
	}
	existing, err := c.channelLedgerRepo.WithTx(tx).GetInboundEntryForUpdate(ctx, ch.ID, incoming.PlatformChatID, incoming.PlatformMsgID)
	if err != nil {
		return false, nil, err
	}
	if existing == nil {
		return false, &telegramInboundStageAResult{finalState: inboundStatePendingDispatch}, nil
	}
	return false, &telegramInboundStageAResult{finalState: inboundLedgerState(existing.MetadataJSON)}, nil
}

func (c telegramConnector) recordTelegramInboundFinalState(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	incoming telegramIncomingMessage,
	identityID *uuid.UUID,
	threadID *uuid.UUID,
	messageID *uuid.UUID,
	state string,
	baseMetadata map[string]any,
) error {
	accepted, err := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:               ch.ID,
		ChannelType:             ch.ChannelType,
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                threadID,
		PlatformConversationID:  incoming.PlatformChatID,
		PlatformMessageID:       incoming.PlatformMsgID,
		PlatformParentMessageID: incoming.ReplyToMsgID,
		PlatformThreadID:        incoming.MessageThreadID,
		SenderChannelIdentityID: identityID,
		MessageID:               messageID,
		MetadataJSON:            inboundLedgerMetadata(baseMetadata, state),
	})
	if err != nil {
		return err
	}
	if accepted {
		return nil
	}
	_, err = c.channelLedgerRepo.WithTx(tx).UpdateInboundEntry(
		ctx,
		ch.ID,
		incoming.PlatformChatID,
		incoming.PlatformMsgID,
		threadID,
		nil,
		messageID,
		inboundLedgerMetadata(baseMetadata, state),
	)
	return err
}

func (c telegramConnector) recoverPendingTelegramInboundDispatches(ctx context.Context, channelID uuid.UUID) error {
	if c.channelLedgerRepo == nil || channelID == uuid.Nil {
		return nil
	}
	ch, err := c.channelsRepo.GetByID(ctx, channelID)
	if err != nil || ch == nil || !ch.IsActive || ch.ChannelType != "telegram" {
		return err
	}
	_, personaRef, _, err := mustValidateTelegramActivation(ctx, ch.AccountID, c.personasRepo, ch.PersonaID, ch.ConfigJSON)
	if err != nil {
		return err
	}
	items, err := c.channelLedgerRepo.ListInboundEntriesByState(ctx, ch.ID, inboundStatePendingDispatch, 256)
	if err != nil {
		return err
	}
	threadSet := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if item.ThreadID != nil && *item.ThreadID != uuid.Nil {
			threadSet[*item.ThreadID] = struct{}{}
		}
	}
	threadIDs := make([]uuid.UUID, 0, len(threadSet))
	for threadID := range threadSet {
		threadIDs = append(threadIDs, threadID)
	}
	sort.Slice(threadIDs, func(i, j int) bool {
		return threadIDs[i].String() < threadIDs[j].String()
	})

	now := time.Now().UTC()
	for _, threadID := range threadIDs {
		tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}

		ledgerTx := c.channelLedgerRepo.WithTx(tx)
		runTx := c.runEventRepo.WithTx(tx)
		if err := runTx.LockThreadRow(ctx, threadID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		activeRun, err := runTx.GetActiveRootRunForThread(ctx, threadID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if activeRun != nil {
			threadBatch, batchErr := listPendingInboundBatchTx(ctx, ledgerTx, ch.ID, threadID)
			if batchErr != nil {
				_ = tx.Rollback(ctx)
				return batchErr
			}
			state, delivered, deliverErr := deliverPendingBatchToActiveRunTx(
				ctx,
				*ch,
				runTx,
				c.messageRepo,
				ledgerTx,
				activeRun,
				threadBatch,
				observability.NewTraceID(),
			)
			if deliverErr != nil {
				_ = tx.Rollback(ctx)
				return deliverErr
			}
			if state != "" {
				if err := markPendingBatchStateTx(ctx, ledgerTx, ch.ID, threadBatch, &activeRun.ID, state); err != nil {
					_ = tx.Rollback(ctx)
					return err
				}
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if delivered {
				c.notifyActiveRunInput(ctx, activeRun.ID)
			}
			continue
		}

		batch, err := listPendingInboundBatchTx(ctx, ledgerTx, ch.ID, threadID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if !pendingBatchReady(batch, now) {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			continue
		}
		latest, err := latestPendingBatchEntry(batch)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}

		if err := c.continueTelegramInboundDispatch(ctx, observability.NewTraceID(), *ch, personaRef, latest); err != nil {
			if errors.Is(err, errInboundDispatchDeferred) {
				continue
			}
			return err
		}
	}
	return nil
}
