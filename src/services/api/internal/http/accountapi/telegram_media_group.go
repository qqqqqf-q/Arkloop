package accountapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const telegramMediaGroupDebounce = 750 * time.Millisecond

var (
	mediaGroupMu      sync.Mutex
	mediaGroupBuckets = map[string]*mediaGroupBucket{}
)

type mediaGroupBucket struct {
	mu      sync.Mutex
	conn    telegramConnector
	ch      data.Channel
	token   string
	traceID string
	items   []telegramUpdate
	timer   *time.Timer
	persona *data.Persona
}

func telegramMediaGroupBucketKey(ch data.Channel, update telegramUpdate) string {
	if update.Message == nil {
		return ""
	}
	gid := strings.TrimSpace(update.Message.MediaGroupID)
	if gid == "" {
		return ""
	}
	return ch.ID.String() + ":" + strconv.FormatInt(update.Message.Chat.ID, 10) + ":" + gid
}

// tryScheduleTelegramMediaGroup 将相册多条 Webhook 合并为一次落库；返回 true 时表示本请求已入队，HandleUpdate 应直接结束。
func (c telegramConnector) tryScheduleTelegramMediaGroup(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	token string,
	update telegramUpdate,
	incoming telegramIncomingMessage,
	persona *data.Persona,
) bool {
	_ = ctx
	if persona == nil || update.Message == nil {
		return false
	}
	if strings.TrimSpace(update.Message.MediaGroupID) == "" {
		return false
	}
	if len(incoming.MediaAttachments) == 0 {
		return false
	}
	key := telegramMediaGroupBucketKey(ch, update)
	if key == "" {
		return false
	}

	mediaGroupMu.Lock()
	b, ok := mediaGroupBuckets[key]
	if !ok {
		b = &mediaGroupBucket{}
		mediaGroupBuckets[key] = b
	}
	mediaGroupMu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	b.conn = c
	b.ch = ch
	b.token = token
	b.traceID = traceID
	b.persona = persona
	b.items = append(b.items, update)
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(telegramMediaGroupDebounce, func() {
		flushTelegramMediaGroupBucket(key)
	})
	return true
}

func flushTelegramMediaGroupBucket(key string) {
	mediaGroupMu.Lock()
	b := mediaGroupBuckets[key]
	delete(mediaGroupBuckets, key)
	mediaGroupMu.Unlock()
	if b == nil {
		return
	}
	b.mu.Lock()
	items := b.items
	b.items = nil
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	conn := b.conn
	ch := b.ch
	token := b.token
	traceID := b.traceID
	persona := b.persona
	b.mu.Unlock()

	if len(items) == 0 || persona == nil {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		var mi, mj int64
		if items[i].Message != nil {
			mi = items[i].Message.MessageID
		}
		if items[j].Message != nil {
			mj = items[j].Message.MessageID
		}
		return mi < mj
	})

	cfg, err := resolveTelegramConfig("telegram", ch.ConfigJSON)
	if err != nil {
		slog.Error("telegram_media_group_flush", "phase", "config", "err", err)
		return
	}
	merged, err := mergeTelegramAlbumIncoming(ch.ID, ch.ChannelType, items, cfg.BotUsername, cfg.TelegramBotUserID, buildTelegramTriggerKeywords(cfg))
	if err != nil || merged == nil {
		slog.Error("telegram_media_group_flush", "phase", "merge", "err", err)
		return
	}
	bgCtx := context.Background()
	if err := conn.processTelegramMediaGroupMerged(bgCtx, traceID, ch, token, cfg.BotUsername, items, *merged, persona); err != nil {
		slog.Error("telegram_media_group_flush", "phase", "persist", "err", err)
	}
}

func mergeTelegramAlbumIncoming(
	channelID uuid.UUID,
	channelType string,
	items []telegramUpdate,
	botUsername string,
	botUID int64,
	triggerKeywords []string,
) (*telegramIncomingMessage, error) {
	var merged *telegramIncomingMessage
	for _, u := range items {
		raw, err := json.Marshal(u)
		if err != nil {
			return nil, err
		}
		inc, err := normalizeTelegramIncomingMessage(channelID, channelType, raw, u, botUsername, botUID, triggerKeywords)
		if err != nil {
			return nil, err
		}
		if inc == nil {
			continue
		}
		if merged == nil {
			cp := *inc
			merged = &cp
			continue
		}
		if t := strings.TrimSpace(inc.Text); t != "" {
			if strings.TrimSpace(merged.Text) != "" {
				merged.Text = strings.TrimSpace(merged.Text) + "\n" + t
			} else {
				merged.Text = t
			}
		}
		merged.MediaAttachments = append(merged.MediaAttachments, inc.MediaAttachments...)
		if a, errA := strconv.ParseInt(inc.PlatformMsgID, 10, 64); errA == nil {
			if b, errB := strconv.ParseInt(merged.PlatformMsgID, 10, 64); errB == nil && a > b {
				merged.PlatformMsgID = inc.PlatformMsgID
			}
		}
		if inc.ReplyToMsgID != nil {
			merged.ReplyToMsgID = inc.ReplyToMsgID
		}
		if inc.MessageThreadID != nil {
			merged.MessageThreadID = inc.MessageThreadID
		}
	}
	if merged == nil || !merged.HasContent() {
		return nil, nil
	}
	return merged, nil
}

func (c telegramConnector) processTelegramMediaGroupMerged(
	ctx context.Context,
	_ string,
	ch data.Channel,
	token string,
	botUsername string,
	originals []telegramUpdate,
	incoming telegramIncomingMessage,
	persona *data.Persona,
) error {
	now := time.Now().UTC()
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, u := range originals {
		if u.Message == nil {
			continue
		}
		pid := strconv.FormatInt(u.Message.MessageID, 10)
		if _, err := c.channelReceiptsRepo.WithTx(tx).Record(ctx, ch.ID, incoming.PlatformChatID, pid); err != nil {
			return err
		}
	}

	last := originals[len(originals)-1].Message
	if last == nil || last.From == nil {
		return nil
	}
	identity, err := upsertTelegramIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), last.From)
	if err != nil {
		return err
	}

	if incoming.IsPrivate() {
		trimmedCommandText := strings.TrimSpace(incoming.CommandText)
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
			return err
		} else if handled {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
				if _, err := c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				}); err != nil {
					slog.Warn("telegram: failed to send command reply", "chat_id", incoming.PlatformChatID, "err", err)
				}
				sendCancel()
			}
			return nil
		}
	}

	if !incoming.IsPrivate() && isTelegramGroupLikeChatType(incoming.ChatType) && c.channelGroupThreadsRepo != nil {
		cmd, ok := telegramCommandBase(strings.TrimSpace(incoming.CommandText), botUsername)
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
					return err
				} else {
					replyText = "已开启新会话。"
				}
			} else {
				replyText = "已开启新会话。"
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
				if _, err := c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				}); err != nil {
					slog.Warn("telegram: failed to send /new command reply", "chat_id", incoming.PlatformChatID, "err", err)
				}
				sendCancel()
			}
			return nil
		}
	}

	if !incoming.HasContent() {
		return tx.Commit(ctx)
	}

	if !incoming.IsPrivate() && !incoming.ShouldCreateRun() {
		baseMetadata := telegramInboundBaseMetadata(incoming)
		baseMetadata["media_group"] = true
		_, finalState, err := c.persistTelegramGroupPassiveMessageTx(ctx, tx, ch, token, incoming, identity, persona, baseMetadata)
		if err != nil {
			return err
		}
		if finalState == inboundStatePendingDispatch {
			return tx.Commit(ctx)
		}
		return tx.Commit(ctx)
	}

	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, incoming)
	if err != nil {
		return err
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
		return err
	}
	preTailMsg, err := c.messageRepo.WithTx(tx).GetLatestVisibleMessage(ctx, ch.AccountID, threadID)
	if err != nil {
		return err
	}
	preTailMessageID := ""
	if preTailMsg != nil {
		preTailMessageID = preTailMsg.ID.String()
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
		return err
	}
	var ledgerRepoTx *data.ChannelMessageLedgerRepository
	if c.channelLedgerRepo != nil {
		ledgerRepoTx = c.channelLedgerRepo.WithTx(tx)
		ledgerBaseMetadata := map[string]any{
			"source":            "telegram",
			"conversation_type": incoming.ChatType,
			"mentions_bot":      incoming.MentionsBot,
			"is_reply_to_bot":   incoming.IsReplyToBot,
			"media_group":       true,
		}
		if preTailMessageID != "" {
			ledgerBaseMetadata[inboundMetadataPreTailKey] = preTailMessageID
		}
		ledgerMetadata, metaErr := json.Marshal(ledgerBaseMetadata)
		if metaErr != nil {
			return metaErr
		}
		if incoming.ShouldCreateRun() {
			ledgerMetadata = applyInboundBurstMetadata(inboundLedgerMetadata(ledgerBaseMetadata, inboundStatePendingDispatch), nextInboundBurstDispatchAfter(now))
		}
		if _, ledgerErr := ledgerRepoTx.Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			ThreadID:                &threadID,
			PlatformConversationID:  incoming.PlatformChatID,
			PlatformMessageID:       incoming.PlatformMsgID,
			PlatformParentMessageID: incoming.ReplyToMsgID,
			PlatformThreadID:        incoming.MessageThreadID,
			SenderChannelIdentityID: &identity.ID,
			MessageID:               &msg.ID,
			MetadataJSON:            ledgerMetadata,
		}); ledgerErr != nil {
			return ledgerErr
		}
	}
	if !incoming.ShouldCreateRun() {
		return tx.Commit(ctx)
	}
	if ledgerRepoTx != nil {
		if err := promoteRecentPassiveInboundToPendingTx(ctx, ledgerRepoTx, ch.ID, threadID, now); err != nil {
			return err
		}
		if err := extendPendingInboundBurstWindowTx(ctx, ledgerRepoTx, ch.ID, threadID, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
