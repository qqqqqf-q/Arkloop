package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errInboundDispatchDeferred = errors.New("channel inbound dispatch deferred")

type telegramInboundStageAResult struct {
	finalState  string
	replyText   string
	cancelRunID uuid.UUID
}

func telegramInboundBaseMetadata(incoming telegramIncomingMessage) map[string]any {
	return map[string]any{
		"source":            "telegram",
		"conversation_type": incoming.ChatType,
		"mentions_bot":      incoming.MentionsBot,
		"is_reply_to_bot":   incoming.IsReplyToBot,
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
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}

	claimed, stageResult, err := c.claimTelegramInboundStageA(ctx, tx, ch, incoming, &identity.ID, baseMetadata, dispatchAfterUnixMs)
	if err != nil {
		return nil, err
	}
	if !claimed {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return stageResult, nil
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
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{finalState: inboundStateIgnoredUnlinked}, nil
		}
		if handled, replyText, err := handleTelegramCommand(
			ctx,
			tx,
			&ch,
			identity,
			trimmedCommandText,
			telegramDMPlatformThreadID(incoming),
			ch.AccountID,
			c.entitlementSvc,
			c.channelBindCodesRepo,
			c.channelIdentitiesRepo,
			c.channelIdentityLinksRepo,
			c.channelDMThreadsRepo,
			c.threadRepo,
			c.runEventRepo.WithTx(tx),
			c.pool,
		); err != nil {
			return nil, err
		} else if handled {
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &identity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{
				finalState: inboundStateCommandHandled,
				replyText:  replyText,
			}, nil
		}
	}

	if !incoming.IsPrivate() && isTelegramGroupLikeChatType(incoming.ChatType) && c.channelGroupThreadsRepo != nil {
		cmd, ok := telegramCommandBase(strings.TrimSpace(incoming.CommandText), cfg.BotUsername)
		if ok && cmd == "/new" {
			var replyText string
			if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
				replyText = "当前会话未配置 persona。"
			} else if identity.UserID == nil {
				replyText = "无权限。"
			} else if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				tgUserID, _ := strconv.ParseInt(incoming.PlatformUserID, 10, 64)
				member, err := c.telegramClient.GetChatMember(ctx, token, telegrambot.GetChatMemberRequest{
					ChatID: incoming.PlatformChatID,
					UserID: tgUserID,
				})
				if err != nil || member == nil || (member.Status != "creator" && member.Status != "administrator") {
					replyText = "无权限。"
				} else if err := c.channelGroupThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, incoming.PlatformChatID, *ch.PersonaID); err != nil {
					return nil, err
				} else {
					replyText = "已开启新会话。"
				}
			} else {
				replyText = "已开启新会话。"
			}
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &identity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{finalState: inboundStateCommandHandled, replyText: replyText}, nil
		}
		if ok && strings.HasPrefix(cmd, "/heartbeat") {
			heartbeatIdentity := identity
			if groupIdentity != nil {
				heartbeatIdentity = *groupIdentity
			}
			replyText, err := handleTelegramHeartbeatCommand(
				ctx,
				tx,
				ch.ID,
				ch.AccountID,
				ch.PersonaID,
				cfg.DefaultModel,
				heartbeatIdentity,
				incoming.CommandText,
				c.channelIdentitiesRepo,
				c.personasRepo,
				c.entitlementSvc,
			)
			if err != nil {
				return nil, err
			}
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &heartbeatIdentity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{finalState: inboundStateCommandHandled, replyText: replyText}, nil
		}
		if ok && cmd == "/stop" {
			var replyText string
			var cancelRunID uuid.UUID
			if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
				replyText = "当前没有运行中的任务。"
			} else if identity.UserID == nil {
				replyText = "无权限。"
			} else if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				tgUserID, _ := strconv.ParseInt(incoming.PlatformUserID, 10, 64)
				member, err := c.telegramClient.GetChatMember(ctx, token, telegrambot.GetChatMemberRequest{
					ChatID: incoming.PlatformChatID,
					UserID: tgUserID,
				})
				if err != nil || member == nil || (member.Status != "creator" && member.Status != "administrator") {
					replyText = "无权限。"
				} else {
					threadMap, err := c.channelGroupThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, incoming.PlatformChatID, *ch.PersonaID)
					if err != nil {
						return nil, err
					}
					if threadMap == nil {
						replyText = "当前没有运行中的任务。"
					} else {
						activeRun, err := c.runEventRepo.GetActiveRootRunForThread(ctx, threadMap.ThreadID)
						if err != nil {
							return nil, err
						}
						if activeRun == nil {
							replyText = "当前没有运行中的任务。"
						} else {
							if _, err := c.runEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, identity.UserID, traceID, 0, nil); err != nil {
								return nil, err
							}
							cancelRunID = activeRun.ID
							replyText = "已请求停止当前任务。"
						}
					}
				}
			} else {
				replyText = "当前没有运行中的任务。"
			}
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &identity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{
				finalState:  inboundStateCommandHandled,
				replyText:   replyText,
				cancelRunID: cancelRunID,
			}, nil
		}
		if ok && (cmd == "/model" || strings.HasPrefix(cmd, "/think")) {
			modelIdentity := identity
			if groupIdentity != nil {
				modelIdentity = *groupIdentity
			}
			replyText, err := handleTelegramPreferenceCommand(
				ctx, tx, ch.AccountID, modelIdentity, incoming.CommandText, c.channelIdentitiesRepo, c.entitlementSvc,
			)
			if err != nil {
				return nil, err
			}
			if err := c.recordTelegramInboundFinalState(ctx, tx, ch, incoming, &modelIdentity.ID, nil, nil, inboundStateCommandHandled, baseMetadata); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &telegramInboundStageAResult{finalState: inboundStateCommandHandled, replyText: replyText}, nil
		}
		_, finalState, err := c.persistTelegramGroupPassiveMessageTx(ctx, tx, ch, token, incoming, identity, persona, baseMetadata)
		if err != nil {
			return nil, err
		}
		if finalState == inboundStatePendingDispatch {
			if err := tx.Commit(ctx); err != nil {
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
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &telegramInboundStageAResult{finalState: finalState}, nil
	}

	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, incoming)
	if err != nil {
		return nil, err
	}
	timeCtx := c.resolveInboundTimeContext(ctx, ch, identity, incoming)
	content, contentJSON, metadataJSON, err := buildTelegramStructuredMessageWithMedia(
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
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &telegramInboundStageAResult{finalState: inboundStatePendingDispatch}, nil
}

func (c telegramConnector) continueTelegramInboundDispatch(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	personaRef string,
	defaultModel string,
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

	if !channelAgentTriggerConsume(ch.ID) {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.WarnContext(ctx, "telegram_inbound_processed",
			"stage", inboundStateThrottledNoRun,
			"channel_id", ch.ID.String(),
			"account_id", ch.AccountID.String(),
			"thread_id", latestEntry.ThreadID.String(),
			"platform_chat_id", latestEntry.PlatformConversationID,
			"platform_message_id", latestEntry.PlatformMessageID,
			"default_model", strings.TrimSpace(defaultModel),
		)
		return errInboundDispatchDeferred
	}

	preferredModel, reasoningMode, err := c.channelIdentitiesRepo.GetPreferenceConfig(ctx, *latestEntry.SenderChannelIdentityID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(preferredModel) != "" {
		defaultModel = preferredModel
	}

	runStartedData := buildTelegramRunStartedData(
		personaRef,
		defaultModel,
		reasoningMode,
		ch.ID,
		*latestEntry.SenderChannelIdentityID,
		buildTelegramIncomingFromLedger(latestEntry),
	)
	runStartedData["thread_tail_message_id"] = latestEntry.MessageID.String()
	run, _, err := c.runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		*latestEntry.ThreadID,
		msg.CreatedByUserID,
		"run.started",
		runStartedData,
	)
	if err != nil {
		return err
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(
		ctx,
		ch.AccountID,
		run.ID,
		traceID,
		data.RunExecuteJobType,
		map[string]any{
			"source":           "telegram",
			"channel_delivery": buildTelegramChannelDeliveryPayload(ch.ID, *latestEntry.SenderChannelIdentityID, buildTelegramIncomingFromLedger(latestEntry)),
		},
		nil,
	); err != nil {
		return err
	}
	if err := markPendingBatchEnqueuedTx(ctx, c.channelLedgerRepo.WithTx(tx), ch.ID, entries, run.ID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.InfoContext(ctx, "telegram_inbound_processed",
		"stage", inboundStateEnqueuedNewRun,
		"channel_id", ch.ID.String(),
		"account_id", ch.AccountID.String(),
		"run_id", run.ID.String(),
		"thread_id", latestEntry.ThreadID.String(),
		"platform_chat_id", latestEntry.PlatformConversationID,
		"platform_message_id", latestEntry.PlatformMessageID,
		"default_model", strings.TrimSpace(defaultModel),
	)
	return nil
}

func buildTelegramIncomingFromLedger(entry data.ChannelInboundLedgerEntry) telegramIncomingMessage {
	incoming := telegramIncomingMessage{
		PlatformChatID:  strings.TrimSpace(entry.PlatformConversationID),
		PlatformMsgID:   strings.TrimSpace(entry.PlatformMessageID),
		ReplyToMsgID:    entry.PlatformParentMessageID,
		MessageThreadID: entry.PlatformThreadID,
	}
	if chatType, ok := inboundLedgerString(entry.MetadataJSON, "conversation_type"); ok {
		incoming.ChatType = strings.TrimSpace(chatType)
	}
	if mentionsBot, ok := inboundLedgerBool(entry.MetadataJSON, "mentions_bot"); ok {
		incoming.MentionsBot = mentionsBot
	}
	if replyToBot, ok := inboundLedgerBool(entry.MetadataJSON, "is_reply_to_bot"); ok {
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
	cfg, err := resolveTelegramConfig(ch.ChannelType, ch.ConfigJSON)
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

		if err := c.continueTelegramInboundDispatch(ctx, observability.NewTraceID(), *ch, personaRef, cfg.DefaultModel, latest); err != nil {
			if errors.Is(err, errInboundDispatchDeferred) {
				continue
			}
			return err
		}
	}
	return nil
}
