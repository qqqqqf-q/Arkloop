package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/weixinclient"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --- config ---

type weixinChannelConfig struct {
	AllowedUserIDs  []string `json:"allowed_user_ids,omitempty"`
	AllowedGroupIDs []string `json:"allowed_group_ids,omitempty"`
	AllowAllUsers   bool     `json:"allow_all_users,omitempty"`
	DefaultModel    string   `json:"default_model,omitempty"`
	BaseURL         string   `json:"base_url,omitempty"`
}

func resolveWeixinChannelConfig(raw json.RawMessage) (weixinChannelConfig, error) {
	if len(raw) == 0 {
		return weixinChannelConfig{AllowAllUsers: true}, nil
	}
	var cfg weixinChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return weixinChannelConfig{}, fmt.Errorf("invalid weixin channel config: %w", err)
	}
	if len(cfg.AllowedUserIDs) == 0 && len(cfg.AllowedGroupIDs) == 0 {
		cfg.AllowAllUsers = true
	}
	return cfg, nil
}

func weixinUserAllowed(cfg weixinChannelConfig, userID, groupID string) bool {
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

// --- connector ---

type weixinConnector struct {
	channelsRepo            *data.ChannelsRepository
	channelIdentitiesRepo   *data.ChannelIdentitiesRepository
	channelDMThreadsRepo    *data.ChannelDMThreadsRepository
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository
	channelReceiptsRepo     *data.ChannelMessageReceiptsRepository
	channelLedgerRepo       *data.ChannelMessageLedgerRepository
	personasRepo            *data.PersonasRepository
	threadRepo              *data.ThreadRepository
	messageRepo             *data.MessageRepository
	runEventRepo            *data.RunEventRepository
	jobRepo                 *data.JobRepository
	pool                    data.DB
	inputNotify             func(ctx context.Context, runID uuid.UUID)
	weixinClient            *weixinclient.Client
}

// HandleWeChatMessage 处理一条微信 iLink 消息。
func (c *weixinConnector) HandleWeChatMessage(ctx context.Context, traceID string, ch data.Channel, msg weixinclient.WeixinMessage) error {
	if msg.MessageType != 1 || msg.MessageState != 2 {
		return nil
	}

	var text string
	for _, item := range msg.ItemList {
		if item.Type == 1 && item.TextItem != nil {
			text = strings.TrimSpace(item.TextItem.Text)
			break
		}
	}
	if text == "" {
		return nil
	}

	freshChannel, ok, err := c.currentWeixinChannel(ctx, ch)
	if err != nil || !ok {
		return err
	}
	ch = freshChannel

	cfg, err := resolveWeixinChannelConfig(ch.ConfigJSON)
	if err != nil {
		return fmt.Errorf("invalid weixin channel config: %w", err)
	}

	userID := msg.FromUserID
	groupID := strings.TrimSpace(msg.GroupID)
	isPrivate := groupID == ""
	chatType := "private"
	platformChatID := userID
	if !isPrivate {
		chatType = "group"
		platformChatID = groupID
	}

	if !weixinUserAllowed(cfg, userID, groupID) {
		return nil
	}

	slog.InfoContext(ctx, "weixin_inbound",
		"channel_id", ch.ID,
		"chat_type", chatType,
		"platform_chat_id", platformChatID,
		"from_user_id", userID,
		"text_len", len(text),
	)

	persona, personaRef, err := c.resolveWeixinPersona(ctx, ch)
	if err != nil {
		return err
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	commitTx := func() error {
		return tx.Commit(ctx)
	}

	identity, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, "weixin", userID, nil, nil, nil)
	if err != nil {
		return err
	}

	// 群聊也 upsert group identity
	if !isPrivate {
		if _, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, ch.ChannelType, platformChatID, nil, nil, nil); err != nil {
			return err
		}
	}

	incoming := InboundMessage{
		ChannelID:        ch.ID,
		ChannelType:      "weixin",
		PlatformChatID:   platformChatID,
		PlatformMsgID:    msg.ContextToken,
		PlatformUserID:   userID,
		ConversationType: chatType,
		Text:             text,
		CommandText:      text,
	}

	// 命令处理：/model /think /heartbeat /new /stop
	cmdText := incoming.CommandText
	if _, replyText, _, cancelRunID, err := DispatchChannelCommand(
		ctx, tx, ch, *persona, identity,
		cmdText, isPrivate, platformChatID,
		cfg.DefaultModel, nil,
		ChannelCommandResolver{
			ResolveThreadID: func(ctx context.Context, tx pgx.Tx, personaID, projectID uuid.UUID, isPrivate bool, chatID string) (uuid.UUID, error) {
				return c.resolveWeixinThreadID(ctx, tx, ch, personaID, projectID, identity, isPrivate, chatID)
			},
			ResolveHeartbeatIdentity: func(ctx context.Context, tx pgx.Tx) (*data.ChannelIdentity, error) {
				gi, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, ch.ChannelType, platformChatID, nil, nil, nil)
				if err != nil {
					return nil, err
				}
				return &gi, nil
			},
		},
		c.channelIdentitiesRepo, c.channelDMThreadsRepo, c.channelGroupThreadsRepo,
		c.personasRepo, c.runEventRepo,
	); err != nil {
		return err
	} else if replyText != "" {
		if err := commitTx(); err != nil {
			return err
		}
		if cancelRunID != uuid.Nil {
			_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, cancelRunID.String())
		}
		c.sendWeixinReply(ctx, msg, replyText)
		return nil
	}

	dispatchResult, accepted, err := DispatchInboundImmediate(ctx, tx, InboundImmediatePipelineRequest{
		TraceID:      traceID,
		Channel:      ch,
		PersonaRef:   personaRef,
		Identity:     identity,
		Incoming:     incoming,
		Source:       "weixin",
		JobPayload:   map[string]any{"context_token": msg.ContextToken},
		ForceActive:  true,
		LedgerRepo:   c.channelLedgerRepo,
		ReceiptsRepo: c.channelReceiptsRepo,
		RunEventRepo: c.runEventRepo,
		JobRepo:      c.jobRepo,
		ReceivedLedgerMetadata: inboundLedgerMetadata(map[string]any{
			inboundLedgerKeySource:           "weixin",
			inboundLedgerKeyConversationType: chatType,
		}, inboundStateReceived),
		ResolveAndPersist: func(ctx context.Context, tx pgx.Tx) (InboundPipelinePersistResult, error) {
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
				return InboundPipelinePersistResult{}, fmt.Errorf("cannot resolve project for persona %s", persona.ID)
			}
			threadID, err := c.resolveWeixinThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, isPrivate, platformChatID)
			if err != nil {
				return InboundPipelinePersistResult{}, err
			}
			if err := ensureInboundThreadDefaultModel(ctx, tx, threadID, cfg.DefaultModel); err != nil {
				return InboundPipelinePersistResult{}, err
			}
			content, err := messagecontent.Normalize(messagecontent.FromText(text).Parts)
			if err != nil {
				return InboundPipelinePersistResult{}, err
			}
			contentJSON, err := content.JSON()
			if err != nil {
				return InboundPipelinePersistResult{}, err
			}
			metadataJSON, _ := json.Marshal(map[string]any{
				"source":              "weixin",
				"channel_identity_id": identity.ID.String(),
				"platform_chat_id":    platformChatID,
				"platform_message_id": msg.ContextToken,
				"platform_user_id":    userID,
				"chat_type":           chatType,
			})
			msgRow, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(ctx, ch.AccountID, threadID, "user", text, contentJSON, metadataJSON, identity.UserID)
			if err != nil {
				return InboundPipelinePersistResult{}, err
			}
			return InboundPipelinePersistResult{
				ThreadID:            threadID,
				MessageID:           msgRow.ID,
				InputContent:        text,
				ThreadTailMessageID: msgRow.ID.String(),
			}, nil
		},
		DeliverToActiveRun: func(ctx context.Context, repo *data.RunEventRepository, run *data.Run, content string, traceID string) (bool, error) {
			return c.deliverToActiveRun(ctx, repo, run, content, traceID)
		},
	})
	if err != nil {
		return err
	}
	if !accepted {
		return commitTx()
	}
	if dispatchResult.Delivered {
		slog.InfoContext(ctx, "weixin_inbound_processed",
			"stage", "delivered_to_existing_run",
			"channel_id", ch.ID, "run_id", dispatchResult.RunID, "thread_id", dispatchResult.ThreadID,
		)
		c.notifyInput(ctx, dispatchResult.RunID)
	}
	return commitTx()
}

func (c *weixinConnector) currentWeixinChannel(ctx context.Context, ch data.Channel) (data.Channel, bool, error) {
	if c == nil || c.channelsRepo == nil || ch.ID == uuid.Nil {
		return ch, true, nil
	}
	latest, err := c.channelsRepo.GetByID(ctx, ch.ID)
	if err != nil {
		return data.Channel{}, false, err
	}
	if latest == nil || !latest.IsActive || latest.ChannelType != "weixin" {
		return data.Channel{}, false, nil
	}
	return *latest, true, nil
}

// --- persona ---

func (c *weixinConnector) resolveWeixinPersona(ctx context.Context, ch data.Channel) (*data.Persona, string, error) {
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return nil, "", fmt.Errorf("weixin channel requires persona_id")
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

// --- thread ---

func (c *weixinConnector) resolveWeixinThreadID(
	ctx context.Context, tx pgx.Tx, ch data.Channel,
	personaID, projectID uuid.UUID, identity data.ChannelIdentity,
	isPrivate bool, platformChatID string,
) (uuid.UUID, error) {
	threadRepoTx := c.threadRepo.WithTx(tx)

	buildTitle := func() *string {
		var t string
		if isPrivate {
			t = platformChatID + " (微信私聊)"
		} else {
			t = "微信群 " + platformChatID
		}
		return &t
	}

	lockTitle := func(threadID uuid.UUID) {
		_, _ = threadRepoTx.UpdateFields(ctx, threadID, data.ThreadUpdateFields{
			SetTitleLocked: true,
			TitleLocked:    true,
		})
	}

	if isPrivate {
		dmRepo := c.channelDMThreadsRepo.WithTx(tx)
		threadMap, err := dmRepo.GetByBinding(ctx, ch.ID, identity.ID, personaID, "")
		if err != nil {
			return uuid.Nil, err
		}
		if threadMap != nil {
			if existing, _ := threadRepoTx.GetByID(ctx, threadMap.ThreadID); existing != nil {
				return threadMap.ThreadID, nil
			}
			slog.InfoContext(ctx, "weixin_stale_dm_binding", "thread_id", threadMap.ThreadID, "channel_id", ch.ID)
			_ = dmRepo.DeleteByBinding(ctx, ch.ID, identity.ID, personaID, "")
		}
		thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, buildTitle(), false)
		if err != nil {
			return uuid.Nil, err
		}
		lockTitle(thread.ID)
		if _, err := dmRepo.Create(ctx, ch.ID, identity.ID, personaID, "", thread.ID); err != nil {
			return uuid.Nil, err
		}
		return thread.ID, nil
	}

	groupRepo := c.channelGroupThreadsRepo.WithTx(tx)
	threadMap, err := groupRepo.GetByBinding(ctx, ch.ID, platformChatID, personaID)
	if err != nil {
		return uuid.Nil, err
	}
	if threadMap != nil {
		if existing, _ := threadRepoTx.GetByID(ctx, threadMap.ThreadID); existing != nil {
			return threadMap.ThreadID, nil
		}
		slog.InfoContext(ctx, "weixin_stale_group_binding", "thread_id", threadMap.ThreadID, "channel_id", ch.ID)
		_ = groupRepo.DeleteByBinding(ctx, ch.ID, platformChatID, personaID)
	}
	thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, buildTitle(), false)
	if err != nil {
		return uuid.Nil, err
	}
	lockTitle(thread.ID)
	if _, err := groupRepo.Create(ctx, ch.ID, platformChatID, personaID, thread.ID); err != nil {
		return uuid.Nil, err
	}
	return thread.ID, nil
}

// --- deliver to active run ---

func (c *weixinConnector) deliverToActiveRun(ctx context.Context, repo *data.RunEventRepository, run *data.Run, content, traceID string) (bool, error) {
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

// --- notify ---

func (c *weixinConnector) notifyInput(ctx context.Context, runID uuid.UUID) {
	if c.inputNotify == nil || runID == uuid.Nil {
		return
	}
	c.inputNotify(ctx, runID)
}

// sendWeixinReply 通过 iLink API 发送文本回复（best-effort，commit 后调用）。
func (c *weixinConnector) sendWeixinReply(ctx context.Context, msg weixinclient.WeixinMessage, replyText string) {
	if c.weixinClient == nil || strings.TrimSpace(replyText) == "" {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.weixinClient.SendMessage(sendCtx, &weixinclient.SendMessageRequest{
		ToUserID:     msg.FromUserID,
		FromUserID:   msg.ToUserID,
		MessageType:  1,
		MessageState: 2,
		ClientID:     uuid.NewString(),
		ContextToken: msg.ContextToken,
		ItemList: []weixinclient.MessageItem{
			{Type: 1, TextItem: &weixinclient.TextItem{Text: replyText}},
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "weixin_send_reply_failed", "error", err, "reply_len", len(replyText))
	}
}
