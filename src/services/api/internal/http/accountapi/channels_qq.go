package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/pgnotify"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --- config ---

type qqChannelConfig struct {
	AllowedUserIDs  []string `json:"allowed_user_ids,omitempty"`
	AllowedGroupIDs []string `json:"allowed_group_ids,omitempty"`
	AllowAllUsers   bool     `json:"allow_all_users,omitempty"`
	DefaultModel    string   `json:"default_model,omitempty"`
	OneBotWSURL     string   `json:"onebot_ws_url,omitempty"`
	OneBotHTTPURL   string   `json:"onebot_http_url,omitempty"`
	OneBotToken     string   `json:"onebot_token,omitempty"`
	BotQQ           string   `json:"bot_qq,omitempty"`
}

func resolveQQChannelConfig(raw json.RawMessage) (qqChannelConfig, error) {
	if len(raw) == 0 {
		return qqChannelConfig{AllowAllUsers: true}, nil
	}
	var cfg qqChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return qqChannelConfig{}, fmt.Errorf("invalid qq channel config: %w", err)
	}
	if len(cfg.AllowedUserIDs) == 0 && len(cfg.AllowedGroupIDs) == 0 {
		cfg.AllowAllUsers = true
	}
	return cfg, nil
}

func qqUserAllowed(cfg qqChannelConfig, userID, groupID string) bool {
	if cfg.AllowAllUsers {
		return true
	}
	if groupID != "" {
		for _, id := range cfg.AllowedGroupIDs {
			if id == groupID {
				return true
			}
		}
	}
	for _, id := range cfg.AllowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// --- incoming message ---

type qqIncomingMessage struct {
	PlatformChatID string
	PlatformMsgID  string
	PlatformUserID string
	ChatType       string // "private" / "group"
	Text           string
	MentionsBot    bool
	IsReplyToBot   bool
	ReplyToMsgID   *string
}

func (m qqIncomingMessage) IsPrivate() bool {
	return m.ChatType == "private"
}

func (m qqIncomingMessage) ShouldCreateRun() bool {
	return m.IsPrivate() || m.MentionsBot || m.IsReplyToBot
}

// --- connector ---

type qqConnector struct {
	channelsRepo             *data.ChannelsRepository
	channelIdentitiesRepo    *data.ChannelIdentitiesRepository
	channelBindCodesRepo     *data.ChannelBindCodesRepository
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	channelDMThreadsRepo     *data.ChannelDMThreadsRepository
	channelGroupThreadsRepo  *data.ChannelGroupThreadsRepository
	channelReceiptsRepo      *data.ChannelMessageReceiptsRepository
	channelLedgerRepo        *data.ChannelMessageLedgerRepository
	personasRepo             *data.PersonasRepository
	threadRepo               *data.ThreadRepository
	messageRepo              *data.MessageRepository
	runEventRepo             *data.RunEventRepository
	jobRepo                  *data.JobRepository
	pool                     data.DB
	inputNotify              func(ctx context.Context, runID uuid.UUID)
}

// HandleEvent 处理来自 OneBot11 的入站事件
func (c *qqConnector) HandleEvent(ctx context.Context, traceID string, ch data.Channel, event onebotclient.Event) error {
	if !event.IsMessageEvent() {
		return nil
	}

	cfg, err := resolveQQChannelConfig(ch.ConfigJSON)
	if err != nil {
		return fmt.Errorf("invalid qq channel config: %w", err)
	}

	userID := event.UserID.String()
	groupID := event.GroupID.String()
	if groupID == "0" {
		groupID = ""
	}

	if !qqUserAllowed(cfg, userID, groupID) {
		return nil
	}

	text := strings.TrimSpace(event.PlainText())
	if text == "" {
		return nil
	}

	// bot self ID: 优先 event.SelfID，备选 config.BotQQ
	selfID := strings.TrimSpace(event.SelfID.String())
	if selfID == "" || selfID == "0" {
		selfID = strings.TrimSpace(cfg.BotQQ)
	}

	isPrivate := event.IsPrivateMessage()
	platformChatID := userID
	chatType := "private"
	if !isPrivate {
		platformChatID = groupID
		chatType = "group"
	}

	// 构建归一化入站消息
	incoming := qqIncomingMessage{
		PlatformChatID: platformChatID,
		PlatformMsgID:  event.MessageID.String(),
		PlatformUserID: userID,
		ChatType:       chatType,
		Text:           text,
		MentionsBot:    !isPrivate && event.MentionsQQ(selfID),
	}

	// 回复检测：通过 GetMsg 精确判断是否回复了 bot 消息
	if !isPrivate && event.IsReplyToMessage() {
		replyMsgID := event.ReplyMessageID()
		if replyMsgID != "" {
			incoming.ReplyToMsgID = &replyMsgID
			incoming.IsReplyToBot = c.checkReplyToBot(ctx, cfg, replyMsgID, selfID)
		}
	}

	persona, personaRef, err := c.resolveQQPersona(ctx, ch)
	if err != nil {
		return err
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	accepted, err := c.channelReceiptsRepo.WithTx(tx).Record(ctx, ch.ID, platformChatID, incoming.PlatformMsgID)
	if err != nil {
		return err
	}
	if !accepted {
		return tx.Commit(ctx)
	}

	displayName := event.SenderDisplayName()
	if displayName == "" {
		displayName = userID
	}
	identity, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, "qq", userID, &displayName, nil, nil)
	if err != nil {
		return err
	}

	// 群聊 upsert group identity（heartbeat 依赖）
	if !isPrivate {
		if _, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, ch.ChannelType, platformChatID, nil, nil, nil); err != nil {
			return err
		}
	}

	// --- 私聊路径 ---
	if isPrivate {
		// bind 访问控制
		if c.channelIdentityLinksRepo != nil && !qqLinkBootstrapAllowed(text) {
			hasLink, err := c.channelIdentityLinksRepo.WithTx(tx).HasLink(ctx, ch.ID, identity.ID)
			if err != nil {
				return err
			}
			if !hasLink {
				if err := tx.Commit(ctx); err != nil {
					return err
				}
				c.sendQQReply(ctx, cfg, "private", platformChatID, "当前账号未关联此接入。请使用 /bind <code> 关联。")
				return nil
			}
		}

		// 私聊命令处理
		if handled, replyText, err := c.handleQQPrivateCommand(ctx, tx, &ch, identity, text); err != nil {
			return err
		} else if handled {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if replyText != "" {
				c.sendQQReply(ctx, cfg, "private", platformChatID, replyText)
			}
			return nil
		}
	}

	// --- 群聊命令路径 ---
	if !isPrivate {
		if cmd, ok := qqCommandBase(text); ok {
			switch {
			case cmd == "/new":
				replyText := c.handleQQGroupNew(ctx, tx, ch, cfg, identity, platformChatID)
				if err := tx.Commit(ctx); err != nil {
					return err
				}
				c.sendQQReply(ctx, cfg, "group", platformChatID, replyText)
				return nil

			case cmd == "/stop":
				replyText, cancelRunID := c.handleQQGroupStop(ctx, tx, ch, cfg, identity, platformChatID, traceID)
				if err := tx.Commit(ctx); err != nil {
					return err
				}
				if cancelRunID != uuid.Nil {
					_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, cancelRunID.String())
				}
				c.sendQQReply(ctx, cfg, "group", platformChatID, replyText)
				return nil

			case strings.HasPrefix(cmd, "/heartbeat"):
				replyText, err := c.handleQQGroupHeartbeat(ctx, tx, ch, cfg, identity, platformChatID, text)
				if err != nil {
					return err
				}
				if err := tx.Commit(ctx); err != nil {
					return err
				}
				c.sendQQReply(ctx, cfg, "group", platformChatID, replyText)
				return nil
			}
		}
	}

	// --- Passive persist（群消息无 @/回复 bot） ---
	if !isPrivate && !incoming.ShouldCreateRun() {
		slog.InfoContext(ctx, "qq_inbound_processed",
			"stage", "passive_persisted",
			"channel_id", ch.ID,
			"platform_chat_id", platformChatID,
			"mentions_bot", incoming.MentionsBot,
			"is_reply_to_bot", incoming.IsReplyToBot,
		)
		if err := c.persistQQGroupPassiveMessage(ctx, tx, ch, persona, identity, incoming, displayName, event.Time); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// --- Active 路径（创建/复用 Run）---
	if c.channelLedgerRepo != nil {
		ledgerMeta, _ := json.Marshal(map[string]any{
			"source":            "qq",
			"conversation_type": chatType,
			"mentions_bot":      incoming.MentionsBot,
			"is_reply_to_bot":   incoming.IsReplyToBot,
		})
		if _, err := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			PlatformConversationID:  platformChatID,
			PlatformMessageID:       incoming.PlatformMsgID,
			PlatformParentMessageID: incoming.ReplyToMsgID,
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMeta,
		}); err != nil {
			return err
		}
	}

	projection := buildQQEnvelopeText(identity.ID, displayName, chatType, text, event.Time, incoming)
	content, err := messagecontent.Normalize(messagecontent.FromText(projection).Parts)
	if err != nil {
		return err
	}
	contentJSON, err := content.JSON()
	if err != nil {
		return err
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"source":              "qq",
		"channel_identity_id": identity.ID.String(),
		"display_name":        displayName,
		"platform_chat_id":    platformChatID,
		"platform_message_id": incoming.PlatformMsgID,
		"platform_user_id":    userID,
		"chat_type":           chatType,
		"mentions_bot":        incoming.MentionsBot,
		"is_reply_to_bot":     incoming.IsReplyToBot,
		"reply_to_message_id": incoming.ReplyToMsgID,
	})

	threadProjectID := derefUUID(persona.ProjectID)
	if threadProjectID == uuid.Nil {
		ownerUserID := uuid.Nil
		if ch.OwnerUserID != nil {
			ownerUserID = *ch.OwnerUserID
		}
		if ownerUserID == uuid.Nil {
			if identity.UserID != nil {
				ownerUserID = *identity.UserID
			}
		}
		if ownerUserID != uuid.Nil {
			if pid, err := c.personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, ownerUserID); err == nil {
				threadProjectID = pid
			}
		}
	}
	if threadProjectID == uuid.Nil {
		return fmt.Errorf("cannot resolve project for persona %s", persona.ID)
	}
	threadID, err := c.resolveQQThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, isPrivate, platformChatID)
	if err != nil {
		return err
	}

	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(
		ctx, ch.AccountID, threadID, "user", projection, contentJSON, metadataJSON, identity.UserID,
	); err != nil {
		return err
	}

	runRepoTx := c.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, threadID); err != nil {
		return err
	}
	if activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, threadID); err != nil {
		return err
	} else if activeRun != nil {
		delivered, err := c.deliverToActiveRun(ctx, runRepoTx, activeRun, projection, traceID)
		if err != nil {
			return err
		}
		if delivered {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			slog.InfoContext(ctx, "qq_inbound_processed",
				"stage", "delivered_to_existing_run",
				"channel_id", ch.ID, "run_id", activeRun.ID, "thread_id", threadID,
			)
			c.notifyInput(ctx, activeRun.ID)
			return nil
		}
	}

	if !channelAgentTriggerConsume(ch.ID) {
		return tx.Commit(ctx)
	}

	runData := map[string]any{"persona_id": personaRef}
	if m := strings.TrimSpace(cfg.DefaultModel); m != "" {
		runData["model"] = m
	}
	run, _, err := runRepoTx.CreateRunWithStartedEvent(ctx, ch.AccountID, threadID, identity.UserID, "run.started", runData)
	if err != nil {
		return err
	}

	jobPayload := map[string]any{
		"source": "qq",
		"channel_delivery": map[string]any{
			"channel_id":   ch.ID.String(),
			"channel_type": "qq",
			"conversation_ref": map[string]any{
				"target": platformChatID,
			},
			"inbound_message_ref": map[string]any{
				"message_id": incoming.PlatformMsgID,
			},
			"trigger_message_ref": map[string]any{
				"message_id": incoming.PlatformMsgID,
			},
			"platform_chat_id":           platformChatID,
			"platform_message_id":        incoming.PlatformMsgID,
			"sender_channel_identity_id": identity.ID.String(),
			"conversation_type":          chatType,
			"message_type":               chatType,
		},
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(ctx, ch.AccountID, run.ID, traceID, data.RunExecuteJobType, jobPayload, nil); err != nil {
		return err
	}

	slog.InfoContext(ctx, "qq_inbound_processed",
		"stage", "new_run_enqueued",
		"channel_id", ch.ID, "run_id", run.ID, "thread_id", threadID,
	)

	return tx.Commit(ctx)
}

// --- reply detection ---

// checkReplyToBot 通过 GetMsg API 精确判断被回复消息是否来自 bot
func (c *qqConnector) checkReplyToBot(ctx context.Context, cfg qqChannelConfig, replyMsgID, selfID string) bool {
	if selfID == "" {
		return false
	}
	client := c.buildOneBotClient(cfg)
	if client == nil {
		return false
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	msg, err := client.GetMsg(reqCtx, replyMsgID)
	if err != nil {
		slog.Warn("qq_check_reply_to_bot_failed", "reply_msg_id", replyMsgID, "error", err)
		return false
	}
	if msg.Sender != nil && msg.Sender.UserID.String() == selfID {
		return true
	}
	return false
}

// --- commands ---

func qqCommandBase(text string) (cmd string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false
	}
	return strings.ToLower(fields[0]), true
}

func qqLinkBootstrapAllowed(text string) bool {
	cmd, ok := qqCommandBase(text)
	if !ok {
		return false
	}
	return cmd == "/help" || cmd == "/bind" || cmd == "/start"
}

func (c *qqConnector) handleQQPrivateCommand(
	ctx context.Context,
	tx pgx.Tx,
	channel *data.Channel,
	identity data.ChannelIdentity,
	text string,
) (bool, string, error) {
	cmd, ok := qqCommandBase(text)
	if !ok {
		return false, "", nil
	}
	parts := strings.Fields(text)

	switch {
	case cmd == "/help":
		return true, "/bind <code> - 绑定你的账号\n/new - 开启新会话\n/stop - 停止当前任务\n/help - 显示此帮助", nil

	case cmd == "/start":
		if len(parts) > 1 && strings.HasPrefix(parts[1], "bind_") {
			if c.channelBindCodesRepo == nil {
				return true, "绑定功能不可用。", nil
			}
			replyText, err := bindTelegramIdentity(ctx, tx, channel, identity, strings.TrimPrefix(parts[1], "bind_"),
				c.channelBindCodesRepo, c.channelIdentitiesRepo, c.channelIdentityLinksRepo, c.channelDMThreadsRepo, c.threadRepo)
			return true, replyText, err
		}
		return true, "已连接 Arkloop\n\n使用 /bind <code> 绑定账号\n私聊直接发消息开始对话，/new 开启新会话", nil

	case cmd == "/bind":
		if len(parts) < 2 {
			return true, "用法: /bind <code>", nil
		}
		if c.channelBindCodesRepo == nil {
			return true, "绑定功能不可用。", nil
		}
		replyText, err := bindTelegramIdentity(ctx, tx, channel, identity, parts[1],
			c.channelBindCodesRepo, c.channelIdentitiesRepo, c.channelIdentityLinksRepo, c.channelDMThreadsRepo, c.threadRepo)
		return true, replyText, err

	case cmd == "/new":
		if channel == nil || channel.PersonaID == nil || *channel.PersonaID == uuid.Nil {
			return true, "当前会话未配置 persona。", nil
		}
		if err := c.channelDMThreadsRepo.WithTx(tx).DeleteByBinding(ctx, channel.ID, identity.ID, *channel.PersonaID); err != nil {
			return true, "", err
		}
		return true, "已开启新会话。", nil

	case cmd == "/stop":
		if channel == nil || channel.PersonaID == nil || *channel.PersonaID == uuid.Nil {
			return true, "当前没有运行中的任务。", nil
		}
		dmThread, err := c.channelDMThreadsRepo.GetByBinding(ctx, channel.ID, identity.ID, *channel.PersonaID)
		if err != nil {
			return true, "", err
		}
		if dmThread == nil {
			return true, "当前没有运行中的任务。", nil
		}
		activeRun, err := c.runEventRepo.GetActiveRootRunForThread(ctx, dmThread.ThreadID)
		if err != nil {
			return true, "", err
		}
		if activeRun == nil {
			return true, "当前没有运行中的任务。", nil
		}
		if _, err := c.runEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, identity.UserID, "", 0, nil); err != nil {
			return true, "", err
		}
		_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, activeRun.ID.String())
		return true, "已请求停止当前任务。", nil

	default:
		return false, "", nil
	}
}

func (c *qqConnector) handleQQGroupNew(
	ctx context.Context, tx pgx.Tx,
	ch data.Channel, cfg qqChannelConfig,
	identity data.ChannelIdentity, platformChatID string,
) string {
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return "当前会话未配置 persona。"
	}
	if !c.isQQGroupAdmin(ctx, cfg, platformChatID, identity.PlatformSubjectID) {
		return "无权限。"
	}
	if err := c.channelGroupThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, platformChatID, *ch.PersonaID); err != nil {
		slog.Warn("qq_group_new_failed", "error", err)
		return "操作失败。"
	}
	return "已开启新会话。"
}

func (c *qqConnector) handleQQGroupStop(
	ctx context.Context, tx pgx.Tx,
	ch data.Channel, cfg qqChannelConfig,
	identity data.ChannelIdentity, platformChatID, traceID string,
) (string, uuid.UUID) {
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return "当前没有运行中的任务。", uuid.Nil
	}
	if !c.isQQGroupAdmin(ctx, cfg, platformChatID, identity.PlatformSubjectID) {
		return "无权限。", uuid.Nil
	}
	threadMap, err := c.channelGroupThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, platformChatID, *ch.PersonaID)
	if err != nil || threadMap == nil {
		return "当前没有运行中的任务。", uuid.Nil
	}
	activeRun, err := c.runEventRepo.GetActiveRootRunForThread(ctx, threadMap.ThreadID)
	if err != nil || activeRun == nil {
		return "当前没有运行中的任务。", uuid.Nil
	}
	if _, err := c.runEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, identity.UserID, traceID, 0, nil); err != nil {
		return "操作失败。", uuid.Nil
	}
	return "已请求停止当前任务。", activeRun.ID
}

func (c *qqConnector) handleQQGroupHeartbeat(
	ctx context.Context, tx pgx.Tx,
	ch data.Channel, cfg qqChannelConfig,
	identity data.ChannelIdentity, platformChatID, rawText string,
) (string, error) {
	// heartbeat 挂载在 group identity 上
	groupIdentity, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, ch.ChannelType, platformChatID, nil, nil, nil)
	if err != nil {
		return "", err
	}
	return handleTelegramHeartbeatCommand(
		ctx, tx,
		ch.ID, ch.AccountID, ch.PersonaID,
		cfg.DefaultModel,
		groupIdentity,
		rawText,
		c.channelIdentitiesRepo,
		c.personasRepo,
		nil, // entitlement service -- desktop 模式下不需要
	)
}

// isQQGroupAdmin 通过 OneBot API 校验群管理员权限
func (c *qqConnector) isQQGroupAdmin(ctx context.Context, cfg qqChannelConfig, groupID, userID string) bool {
	client := c.buildOneBotClient(cfg)
	if client == nil {
		return true // 无法校验时放行
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := client.GetGroupMemberInfo(reqCtx, groupID, userID)
	if err != nil {
		slog.Warn("qq_admin_check_failed", "group_id", groupID, "user_id", userID, "error", err)
		return true // API 失败时放行
	}
	return info.Role == "owner" || info.Role == "admin"
}

// --- passive persist ---

func (c *qqConnector) persistQQGroupPassiveMessage(
	ctx context.Context, tx pgx.Tx,
	ch data.Channel, persona *data.Persona,
	identity data.ChannelIdentity,
	incoming qqIncomingMessage,
	displayName string, unixTS int64,
) error {
	threadProjectID := derefUUID(persona.ProjectID)
	if threadProjectID == uuid.Nil {
		ownerUserID := uuid.Nil
		if ch.OwnerUserID != nil {
			ownerUserID = *ch.OwnerUserID
		}
		if ownerUserID == uuid.Nil && identity.UserID != nil {
			ownerUserID = *identity.UserID
		}
		if ownerUserID != uuid.Nil {
			if pid, err := c.personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, ownerUserID); err == nil {
				threadProjectID = pid
			}
		}
	}
	if threadProjectID == uuid.Nil {
		return fmt.Errorf("cannot resolve project for passive persist")
	}

	threadID, err := c.resolveQQThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, false, incoming.PlatformChatID)
	if err != nil {
		return err
	}

	if c.channelLedgerRepo != nil {
		ledgerMeta, _ := json.Marshal(map[string]any{
			"source":            "qq",
			"conversation_type": incoming.ChatType,
			"mentions_bot":      incoming.MentionsBot,
			"is_reply_to_bot":   incoming.IsReplyToBot,
			"passive":           true,
		})
		if _, err := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			PlatformConversationID:  incoming.PlatformChatID,
			PlatformMessageID:       incoming.PlatformMsgID,
			PlatformParentMessageID: incoming.ReplyToMsgID,
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMeta,
		}); err != nil {
			return err
		}
	}

	projection := buildQQEnvelopeText(identity.ID, displayName, incoming.ChatType, incoming.Text, unixTS, incoming)
	content, err := messagecontent.Normalize(messagecontent.FromText(projection).Parts)
	if err != nil {
		return err
	}
	contentJSON, err := content.JSON()
	if err != nil {
		return err
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"source":              "qq",
		"channel_identity_id": identity.ID.String(),
		"display_name":        displayName,
		"platform_chat_id":    incoming.PlatformChatID,
		"platform_message_id": incoming.PlatformMsgID,
		"platform_user_id":    incoming.PlatformUserID,
		"chat_type":           incoming.ChatType,
		"mentions_bot":        incoming.MentionsBot,
		"is_reply_to_bot":     incoming.IsReplyToBot,
		"passive":             true,
	})

	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(
		ctx, ch.AccountID, threadID, "user", projection, contentJSON, metadataJSON, identity.UserID,
	); err != nil {
		return err
	}
	return nil
}

// --- send reply ---

func (c *qqConnector) buildOneBotClient(cfg qqChannelConfig) *onebotclient.Client {
	httpURL := strings.TrimSpace(cfg.OneBotHTTPURL)
	if httpURL == "" {
		return nil
	}
	token := strings.TrimSpace(cfg.OneBotToken)
	if token == "" {
		if mgr := getNapCatManagerIfExists(); mgr != nil {
			_, token = mgr.WSEndpoint()
		}
	}
	return onebotclient.NewClient(httpURL, token, nil)
}

func (c *qqConnector) sendQQReply(ctx context.Context, cfg qqChannelConfig, msgType, target, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	client := c.buildOneBotClient(cfg)
	if client == nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	msg := onebotclient.TextSegments(text)
	switch msgType {
	case "group":
		_, _ = client.SendGroupMsg(sendCtx, target, msg)
	default:
		_, _ = client.SendPrivateMsg(sendCtx, target, msg)
	}
}

// --- helpers ---

func (c *qqConnector) resolveQQPersona(ctx context.Context, ch data.Channel) (*data.Persona, string, error) {
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return nil, "", fmt.Errorf("qq channel requires persona_id")
	}
	persona, err := c.personasRepo.GetByIDForAccount(ctx, ch.AccountID, *ch.PersonaID)
	if err != nil {
		return nil, "", err
	}
	if persona == nil || !persona.IsActive {
		return nil, "", fmt.Errorf("persona not found or inactive")
	}
	return persona, buildPersonaRef(*persona), nil
}

func (c *qqConnector) resolveQQThreadID(
	ctx context.Context, tx pgx.Tx, ch data.Channel,
	personaID, projectID uuid.UUID, identity data.ChannelIdentity,
	isPrivate bool, platformChatID string,
) (uuid.UUID, error) {
	if isPrivate {
		threadMap, err := c.channelDMThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, identity.ID, personaID)
		if err != nil {
			return uuid.Nil, err
		}
		if threadMap != nil {
			return threadMap.ThreadID, nil
		}
		thread, err := c.threadRepo.WithTx(tx).Create(ctx, ch.AccountID, identity.UserID, projectID, nil, false)
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := c.channelDMThreadsRepo.WithTx(tx).Create(ctx, ch.ID, identity.ID, personaID, thread.ID); err != nil {
			return uuid.Nil, err
		}
		return thread.ID, nil
	}

	threadMap, err := c.channelGroupThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, platformChatID, personaID)
	if err != nil {
		return uuid.Nil, err
	}
	if threadMap != nil {
		return threadMap.ThreadID, nil
	}
	thread, err := c.threadRepo.WithTx(tx).Create(ctx, ch.AccountID, nil, projectID, nil, false)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := c.channelGroupThreadsRepo.WithTx(tx).Create(ctx, ch.ID, platformChatID, personaID, thread.ID); err != nil {
		return uuid.Nil, err
	}
	return thread.ID, nil
}

func (c *qqConnector) deliverToActiveRun(ctx context.Context, repo *data.RunEventRepository, run *data.Run, content, traceID string) (bool, error) {
	if run == nil || strings.TrimSpace(content) == "" {
		return false, nil
	}
	if _, err := repo.ProvideInput(ctx, run.ID, content, traceID); err != nil {
		var notActive data.RunNotActiveError
		if errors.As(err, &notActive) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *qqConnector) notifyInput(ctx context.Context, runID uuid.UUID) {
	if c.inputNotify == nil || runID == uuid.Nil {
		return
	}
	c.inputNotify(ctx, runID)
}

// --- envelope ---

func buildQQEnvelopeText(identityID uuid.UUID, displayName, chatType, body string, unixTS int64, incoming qqIncomingMessage) string {
	ts := ""
	if unixTS > 0 {
		ts = time.Unix(unixTS, 0).UTC().Format(time.RFC3339)
	}
	lines := []string{
		fmt.Sprintf(`display-name: "%s"`, escapeEnvelopeValue(displayName)),
		`channel: "qq"`,
		fmt.Sprintf(`conversation-type: "%s"`, chatType),
	}
	if identityID != uuid.Nil {
		lines = append(lines, fmt.Sprintf(`sender-ref: "%s"`, identityID.String()))
	}
	if incoming.PlatformMsgID != "" {
		lines = append(lines, fmt.Sprintf(`message-id: "%s"`, escapeEnvelopeValue(incoming.PlatformMsgID)))
	}
	if incoming.ReplyToMsgID != nil && *incoming.ReplyToMsgID != "" {
		lines = append(lines, fmt.Sprintf(`reply-to-message-id: "%s"`, escapeEnvelopeValue(*incoming.ReplyToMsgID)))
	}
	if incoming.MentionsBot {
		lines = append(lines, `mentions-bot: true`)
	}
	if ts != "" {
		lines = append(lines, fmt.Sprintf(`time: "%s"`, ts))
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
}

func escapeEnvelopeValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.TrimSpace(value))
}

// --- HTTP callback handler ---

func qqOneBotCallbackHandler(
	channelsRepo *data.ChannelsRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	channelReceiptsRepo *data.ChannelMessageReceiptsRepository,
	personasRepo *data.PersonasRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	runEventRepo *data.RunEventRepository,
	jobRepo *data.JobRepository,
	pool data.DB,
) nethttp.HandlerFunc {
	var channelLedgerRepo *data.ChannelMessageLedgerRepository
	if pool != nil {
		repo, err := data.NewChannelMessageLedgerRepository(pool)
		if err != nil {
			panic(err)
		}
		channelLedgerRepo = repo
	}

	connector := &qqConnector{
		channelsRepo:             channelsRepo,
		channelIdentitiesRepo:    channelIdentitiesRepo,
		channelBindCodesRepo:     channelBindCodesRepo,
		channelIdentityLinksRepo: channelIdentityLinksRepo,
		channelDMThreadsRepo:     channelDMThreadsRepo,
		channelGroupThreadsRepo:  channelGroupThreadsRepo,
		channelReceiptsRepo:      channelReceiptsRepo,
		channelLedgerRepo:        channelLedgerRepo,
		personasRepo:             personasRepo,
		threadRepo:               threadRepo,
		messageRepo:              messageRepo,
		runEventRepo:             runEventRepo,
		jobRepo:                  jobRepo,
		pool:                     pool,
		inputNotify: func(ctx context.Context, runID uuid.UUID) {
			if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String()); err != nil {
				slog.Warn("qq_active_run_notify_failed", "run_id", runID, "error", err)
			}
		},
	}

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		var event onebotclient.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid onebot event", traceID, nil)
			return
		}

		if event.IsHeartbeat() || event.IsLifecycle() {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		if !event.IsMessageEvent() {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		channels, err := channelsRepo.ListActiveByType(r.Context(), "qq")
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if len(channels) == 0 {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		ch := channels[0]
		if err := connector.HandleEvent(r.Context(), traceID, ch, event); err != nil {
			slog.ErrorContext(r.Context(), "qq_onebot_callback_error", "error", err, "channel_id", ch.ID)
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	}
}
