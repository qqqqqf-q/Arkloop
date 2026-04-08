package accountapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"arkloop/services/api/internal/data"
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

	claimed, stageResult, err := c.claimTelegramInboundStageA(ctx, tx, ch, incoming, &identity.ID, baseMetadata)
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
	}

	if !incoming.IsPrivate() && !incoming.ShouldCreateRun() {
		if _, err := c.persistTelegramGroupPassiveMessageTx(ctx, tx, ch, token, incoming, identity, persona, baseMetadata); err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "telegram_inbound_processed",
			"stage", inboundStatePassivePersisted,
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
		return &telegramInboundStageAResult{finalState: inboundStatePassivePersisted}, nil
	}

	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, incoming)
	if err != nil {
		return nil, err
	}
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
		inboundLedgerMetadata(baseMetadata, inboundStatePendingDispatch),
	); err != nil {
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
	incoming telegramIncomingMessage,
	personaRef string,
	defaultModel string,
) error {
	if c.channelLedgerRepo == nil {
		return nil
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ledger, err := c.channelLedgerRepo.WithTx(tx).GetInboundEntryForUpdate(ctx, ch.ID, incoming.PlatformChatID, incoming.PlatformMsgID)
	if err != nil || ledger == nil {
		return err
	}
	state := inboundLedgerState(ledger.MetadataJSON)
	if ledger.RunID != nil || state == inboundStateIgnoredUnlinked || state == inboundStatePassivePersisted || state == inboundStateCommandHandled || state == inboundStateAbsorbedHeartbeat || state == inboundStateDeliveredToRun || state == inboundStateEnqueuedNewRun {
		return nil
	}
	if ledger.ThreadID == nil || ledger.MessageID == nil || ledger.SenderChannelIdentityID == nil {
		return fmt.Errorf("telegram inbound ledger incomplete for dispatch")
	}

	msg, err := c.messageRepo.GetByID(ctx, ch.AccountID, *ledger.ThreadID, *ledger.MessageID)
	if err != nil {
		return err
	}
	if msg == nil {
		return fmt.Errorf("telegram inbound message missing")
	}

	runRepoTx := c.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, *ledger.ThreadID); err != nil {
		return err
	}
	baseMetadata := telegramInboundBaseMetadata(incoming)
	if preTailMessageID, ok := inboundLedgerString(ledger.MetadataJSON, inboundMetadataPreTailKey); ok {
		baseMetadata[inboundMetadataPreTailKey] = preTailMessageID
	}
	preTailMessageID, _ := inboundLedgerString(ledger.MetadataJSON, inboundMetadataPreTailKey)

	if activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, *ledger.ThreadID); err != nil {
		return err
	} else if activeRun != nil {
		delivered, absorbed, err := c.deliverTelegramMessageToActiveRun(ctx, runRepoTx, activeRun, incoming, msg.Content, traceID, preTailMessageID)
		if err != nil {
			return err
		}
		if absorbed {
			if _, err := c.channelLedgerRepo.WithTx(tx).UpdateInboundEntry(
				ctx,
				ch.ID,
				incoming.PlatformChatID,
				incoming.PlatformMsgID,
				ledger.ThreadID,
				&activeRun.ID,
				ledger.MessageID,
				inboundLedgerMetadata(baseMetadata, inboundStateAbsorbedHeartbeat),
			); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			slog.InfoContext(ctx, "telegram_inbound_processed",
				"stage", inboundStateAbsorbedHeartbeat,
				"channel_id", ch.ID.String(),
				"account_id", ch.AccountID.String(),
				"run_id", activeRun.ID.String(),
				"thread_id", ledger.ThreadID.String(),
				"platform_chat_id", incoming.PlatformChatID,
				"platform_message_id", incoming.PlatformMsgID,
			)
			return nil
		}
		if delivered {
			if _, err := c.channelLedgerRepo.WithTx(tx).UpdateInboundEntry(
				ctx,
				ch.ID,
				incoming.PlatformChatID,
				incoming.PlatformMsgID,
				ledger.ThreadID,
				&activeRun.ID,
				ledger.MessageID,
				inboundLedgerMetadata(baseMetadata, inboundStateDeliveredToRun),
			); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			slog.InfoContext(ctx, "telegram_inbound_processed",
				"stage", inboundStateDeliveredToRun,
				"channel_id", ch.ID.String(),
				"account_id", ch.AccountID.String(),
				"run_id", activeRun.ID.String(),
				"thread_id", ledger.ThreadID.String(),
				"platform_chat_id", incoming.PlatformChatID,
				"platform_message_id", incoming.PlatformMsgID,
				"default_model", strings.TrimSpace(defaultModel),
			)
			c.notifyActiveRunInput(ctx, activeRun.ID)
			return nil
		}
	}

	if !channelAgentTriggerConsume(ch.ID) {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.WarnContext(ctx, "telegram_inbound_processed",
			"stage", inboundStateThrottledNoRun,
			"channel_id", ch.ID.String(),
			"account_id", ch.AccountID.String(),
			"thread_id", ledger.ThreadID.String(),
			"platform_chat_id", incoming.PlatformChatID,
			"platform_message_id", incoming.PlatformMsgID,
			"default_model", strings.TrimSpace(defaultModel),
		)
		return errInboundDispatchDeferred
	}

	runStartedData := buildTelegramRunStartedData(personaRef, defaultModel)
	run, _, err := c.runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		*ledger.ThreadID,
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
			"channel_delivery": buildTelegramChannelDeliveryPayload(ch.ID, *ledger.SenderChannelIdentityID, incoming),
		},
		nil,
	); err != nil {
		return err
	}
	if _, err := c.channelLedgerRepo.WithTx(tx).UpdateInboundEntry(
		ctx,
		ch.ID,
		incoming.PlatformChatID,
		incoming.PlatformMsgID,
		ledger.ThreadID,
		&run.ID,
		ledger.MessageID,
		inboundLedgerMetadata(baseMetadata, inboundStateEnqueuedNewRun),
	); err != nil {
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
		"thread_id", ledger.ThreadID.String(),
		"platform_chat_id", incoming.PlatformChatID,
		"platform_message_id", incoming.PlatformMsgID,
		"default_model", strings.TrimSpace(defaultModel),
	)
	return nil
}

func (c telegramConnector) claimTelegramInboundStageA(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	incoming telegramIncomingMessage,
	identityID *uuid.UUID,
	baseMetadata map[string]any,
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
		MetadataJSON:            inboundLedgerMetadata(baseMetadata, inboundStatePendingDispatch),
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
