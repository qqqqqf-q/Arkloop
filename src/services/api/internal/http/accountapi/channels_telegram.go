package accountapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	nethttp "net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var telegramUserIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

const telegramRemoteRequestTimeout = 5 * time.Second

// telegramPassiveIngestSyncForTest 保留给旧测试入口；当前被动群消息默认同步落库。
var telegramPassiveIngestSyncForTest bool

// SetTelegramPassiveIngestSyncForTest 仅测试使用。
func SetTelegramPassiveIngestSyncForTest(sync bool) {
	telegramPassiveIngestSyncForTest = sync
}

type telegramChannelConfig struct {
	AllowedUserIDs        []string `json:"allowed_user_ids"`
	DefaultModel          string   `json:"default_model,omitempty"`
	BotUsername           string   `json:"bot_username,omitempty"`
	TelegramBotUserID     int64    `json:"telegram_bot_user_id,omitempty"`
	TelegramTypingSignal  *bool    `json:"telegram_typing_indicator,omitempty"`
	TelegramReactionEmoji string   `json:"telegram_reaction_emoji,omitempty"`
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID       int64                   `json:"message_id"`
	MessageThreadID *int64                  `json:"message_thread_id,omitempty"`
	Date            int64                   `json:"date"`
	Text            string                  `json:"text"`
	Caption         string                  `json:"caption"`
	Entities        []telegramMessageEntity `json:"entities,omitempty"`
	CaptionEntities []telegramMessageEntity `json:"caption_entities,omitempty"`
	Chat            telegramChat            `json:"chat"`
	From            *telegramUser           `json:"from"`
	ReplyToMessage  *telegramMessage        `json:"reply_to_message,omitempty"`
	Photo           []telegramPhotoSize     `json:"photo,omitempty"`
	Document        *telegramDocument       `json:"document,omitempty"`
	Audio           *telegramAudio          `json:"audio,omitempty"`
	Voice           *telegramVoice          `json:"voice,omitempty"`
	Video           *telegramVideo          `json:"video,omitempty"`
	Animation       *telegramAnimation      `json:"animation,omitempty"`
	Sticker         *telegramSticker        `json:"sticker,omitempty"`
	MediaGroupID    string                  `json:"media_group_id,omitempty"`
}

type telegramChat struct {
	ID       int64   `json:"id"`
	Type     string  `json:"type"`
	Title    *string `json:"title,omitempty"`
	Username *string `json:"username,omitempty"`
}

type telegramUser struct {
	ID        int64   `json:"id"`
	IsBot     bool    `json:"is_bot"`
	Username  *string `json:"username"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}

type telegramMessageEntity struct {
	Type   string        `json:"type"`
	Offset int           `json:"offset"`
	Length int           `json:"length"`
	User   *telegramUser `json:"user,omitempty"`
}

type telegramPhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type telegramDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type telegramAudio struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
}

type telegramVoice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
}

type telegramVideo struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type telegramAnimation struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type telegramSticker struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func normalizeChannelConfigJSON(channelType string, raw json.RawMessage) (json.RawMessage, *telegramChannelConfig, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, nil, fmt.Errorf("config_json must be a valid JSON object")
	}

	if channelType != "telegram" {
		normalized, err := json.Marshal(generic)
		if err != nil {
			return nil, nil, err
		}
		return normalized, nil, nil
	}

	var cfg telegramChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	normalizedIDs, err := normalizeTelegramAllowedUserIDs(cfg.AllowedUserIDs)
	if err != nil {
		return nil, nil, err
	}
	cfg.AllowedUserIDs = normalizedIDs
	cfg.DefaultModel = strings.TrimSpace(cfg.DefaultModel)
	cfg.BotUsername = strings.TrimSpace(strings.TrimPrefix(cfg.BotUsername, "@"))
	cfg.TelegramReactionEmoji = strings.TrimSpace(cfg.TelegramReactionEmoji)
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	return normalized, &cfg, nil
}

func normalizeTelegramAllowedUserIDs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				continue
			}
			if !telegramUserIDPattern.MatchString(cleaned) {
				return nil, fmt.Errorf("telegram allowed_user_ids must contain numeric user ids")
			}
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			out = append(out, cleaned)
		}
	}
	return out, nil
}

func resolveTelegramConfig(channelType string, raw json.RawMessage) (telegramChannelConfig, error) {
	if channelType != "telegram" {
		return telegramChannelConfig{}, fmt.Errorf("unsupported channel type")
	}
	_, cfg, err := normalizeChannelConfigJSON(channelType, raw)
	if err != nil {
		return telegramChannelConfig{}, err
	}
	if cfg == nil {
		return telegramChannelConfig{}, nil
	}
	return *cfg, nil
}

func telegramTypingEnabled(cfg telegramChannelConfig) bool {
	if cfg.TelegramTypingSignal == nil {
		return true
	}
	return *cfg.TelegramTypingSignal
}

func shouldSendTelegramImmediateTyping(incoming *telegramIncomingMessage) bool {
	if incoming == nil || !incoming.HasContent() {
		return false
	}
	cmd, ok := telegramCommandBase(strings.TrimSpace(incoming.CommandText), "")
	if ok && strings.HasPrefix(cmd, "/heartbeat") {
		return false
	}
	return incoming.ShouldCreateRun()
}

func maybeSendTelegramImmediateTyping(
	ctx context.Context,
	client *telegrambot.Client,
	token string,
	chatID string,
	cfg telegramChannelConfig,
	incoming *telegramIncomingMessage,
) {
	if client == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(chatID) == "" {
		return
	}
	if !telegramTypingEnabled(cfg) || !shouldSendTelegramImmediateTyping(incoming) {
		return
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
	defer sendCancel()
	if err := client.SendChatAction(sendCtx, token, telegrambot.SendChatActionRequest{
		ChatID: strings.TrimSpace(chatID),
		Action: "typing",
	}); err != nil {
		slog.DebugContext(ctx, "telegram_immediate_typing_failed", "chat_id", strings.TrimSpace(chatID), "err", err)
	}
}

func mustValidateTelegramActivation(
	ctx context.Context,
	accountID uuid.UUID,
	personasRepo *data.PersonasRepository,
	personaID *uuid.UUID,
	configJSON json.RawMessage,
) (*data.Persona, string, telegramChannelConfig, error) {
	if personaID == nil || *personaID == uuid.Nil {
		return nil, "", telegramChannelConfig{}, fmt.Errorf("telegram channel requires persona_id before activation")
	}
	persona, err := personasRepo.GetByIDForAccount(ctx, accountID, *personaID)
	if err != nil {
		return nil, "", telegramChannelConfig{}, err
	}
	if persona == nil || !persona.IsActive {
		return nil, "", telegramChannelConfig{}, fmt.Errorf("persona not found or inactive")
	}
	if persona.ProjectID == nil || *persona.ProjectID == uuid.Nil {
		return nil, "", telegramChannelConfig{}, fmt.Errorf("telegram channel persona must belong to a project")
	}
	cfg, err := resolveTelegramConfig("telegram", configJSON)
	if err != nil {
		return nil, "", telegramChannelConfig{}, err
	}
	// allowed_user_ids 为空：不限制 Telegram user_id（非空时仅允许列表内 ID）。
	return persona, buildPersonaRef(*persona), cfg, nil
}

func buildPersonaRef(persona data.Persona) string {
	if strings.TrimSpace(persona.Version) == "" {
		return strings.TrimSpace(persona.PersonaKey)
	}
	return fmt.Sprintf("%s@%s", strings.TrimSpace(persona.PersonaKey), strings.TrimSpace(persona.Version))
}

func telegramModeUsesWebhook(mode string) bool {
	return strings.TrimSpace(strings.ToLower(mode)) != "polling"
}

func configureTelegramRemote(
	ctx context.Context,
	client *telegrambot.Client,
	token string,
	channel data.Channel,
) error {
	remoteCtx, cancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
	defer cancel()
	if client == nil {
		return fmt.Errorf("telegram client not configured")
	}
	if channel.WebhookURL == nil || strings.TrimSpace(*channel.WebhookURL) == "" {
		return fmt.Errorf("webhook_url must not be empty")
	}
	secret := ""
	if channel.WebhookSecret != nil {
		secret = strings.TrimSpace(*channel.WebhookSecret)
	}
	if err := client.SetWebhook(remoteCtx, token, telegrambot.SetWebhookRequest{
		URL:         strings.TrimSpace(*channel.WebhookURL),
		SecretToken: secret,
		Updates:     []string{"message"},
	}); err != nil {
		return err
	}
	return client.SetMyCommands(remoteCtx, token, []telegrambot.BotCommand{
		{Command: "start", Description: "开始使用"},
		{Command: "help", Description: "查看帮助"},
		{Command: "bind", Description: "绑定账号"},
		{Command: "new", Description: "新建会话"},
	})
}

func disableTelegramRemote(ctx context.Context, client *telegrambot.Client, token string) error {
	remoteCtx, cancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
	defer cancel()
	if client == nil {
		return fmt.Errorf("telegram client not configured")
	}
	return client.DeleteWebhook(remoteCtx, token)
}

func configureTelegramPollingRemote(
	ctx context.Context,
	client *telegrambot.Client,
	token string,
) error {
	// Polling mode connects via getUpdates; no webhook or command registration needed.
	_ = ctx
	_ = client
	_ = token
	return nil
}

func configureTelegramActivationRemote(
	ctx context.Context,
	client *telegrambot.Client,
	token string,
	channel data.Channel,
	mode string,
) error {
	if telegramModeUsesWebhook(mode) {
		return configureTelegramRemote(ctx, client, token, channel)
	}
	return configureTelegramPollingRemote(ctx, client, token)
}

// mergeTelegramChannelConfigJSONPatch 将 patch 覆盖到 existing 的键上；patch 未出现的键保留（避免 Desktop 只发 allowlist/model 时抹掉 bot 元数据）。
func mergeTelegramChannelConfigJSONPatch(existing, patch json.RawMessage) (json.RawMessage, error) {
	if len(patch) == 0 {
		return normalizeChannelConfigJSONFirst(existing)
	}
	ex := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &ex); err != nil {
			return nil, fmt.Errorf("config_json must be a valid JSON object")
		}
	}
	if ex == nil {
		ex = map[string]any{}
	}
	patchMap := map[string]any{}
	if err := json.Unmarshal(patch, &patchMap); err != nil {
		return nil, fmt.Errorf("config_json must be a valid JSON object")
	}
	for k, v := range patchMap {
		ex[k] = v
	}
	merged, err := json.Marshal(ex)
	if err != nil {
		return nil, err
	}
	return normalizeChannelConfigJSONFirst(merged)
}

func normalizeChannelConfigJSONFirst(raw json.RawMessage) (json.RawMessage, error) {
	normalized, _, err := normalizeChannelConfigJSON("telegram", raw)
	return normalized, err
}

// mergeTelegramBotProfileFromGetMe 仅在缺省时写入 telegram_bot_user_id / bot_username（与 GetMe 一致）。
func mergeTelegramBotProfileFromGetMe(raw json.RawMessage, info *telegrambot.BotInfo) (json.RawMessage, bool, error) {
	if info == nil {
		return nil, false, fmt.Errorf("telegram getMe result required")
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	cfg, err := resolveTelegramConfig("telegram", raw)
	if err != nil {
		return nil, false, err
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, false, err
	}
	if generic == nil {
		generic = map[string]any{}
	}
	changed := false
	if cfg.TelegramBotUserID == 0 && info.ID != 0 {
		generic["telegram_bot_user_id"] = info.ID
		changed = true
	}
	uname := ""
	if info.Username != nil {
		uname = strings.TrimSpace(*info.Username)
	}
	uname = strings.TrimPrefix(uname, "@")
	if strings.TrimSpace(cfg.BotUsername) == "" && uname != "" {
		generic["bot_username"] = uname
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(generic)
	if err != nil {
		return nil, false, err
	}
	normalized, _, err := normalizeChannelConfigJSON("telegram", out)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

// syncTelegramBotUserIDToConfig 在启用频道后写入 getMe 得到的 Bot ID / username（仅缺省时），供群聊 @ 与回复判定。
func syncTelegramBotUserIDToConfig(
	ctx context.Context,
	channelsRepo *data.ChannelsRepository,
	accountID, channelID uuid.UUID,
	client *telegrambot.Client,
	token string,
	current json.RawMessage,
) error {
	if channelsRepo == nil || client == nil || strings.TrimSpace(token) == "" {
		return nil
	}
	cfg, err := resolveTelegramConfig("telegram", current)
	if err != nil {
		return nil
	}
	if cfg.TelegramBotUserID != 0 && strings.TrimSpace(cfg.BotUsername) != "" {
		return nil
	}
	remoteCtx, cancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
	defer cancel()
	info, err := client.GetMe(remoteCtx, strings.TrimSpace(token))
	if err != nil || info == nil {
		return nil
	}
	merged, changed, err := mergeTelegramBotProfileFromGetMe(current, info)
	if err != nil || !changed {
		return err
	}
	_, err = channelsRepo.Update(ctx, channelID, accountID, data.ChannelUpdate{ConfigJSON: &merged})
	return err
}

func disableTelegramActivationRemote(
	ctx context.Context,
	client *telegrambot.Client,
	token string,
	mode string,
) error {
	if telegramModeUsesWebhook(mode) {
		return disableTelegramRemote(ctx, client, token)
	}
	// Polling mode: no webhook to remove.
	_ = ctx
	_ = client
	_ = token
	return nil
}

type telegramConnector struct {
	channelsRepo            *data.ChannelsRepository
	channelIdentitiesRepo   *data.ChannelIdentitiesRepository
	channelBindCodesRepo    *data.ChannelBindCodesRepository
	channelDMThreadsRepo    *data.ChannelDMThreadsRepository
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository
	channelReceiptsRepo     *data.ChannelMessageReceiptsRepository
	channelLedgerRepo       *data.ChannelMessageLedgerRepository
	personasRepo            *data.PersonasRepository
	usersRepo               *data.UserRepository
	accountRepo             *data.AccountRepository
	membershipRepo          *data.AccountMembershipRepository
	projectRepo             *data.ProjectRepository
	threadRepo              *data.ThreadRepository
	messageRepo             *data.MessageRepository
	runEventRepo            *data.RunEventRepository
	jobRepo                 *data.JobRepository
	creditsRepo             *data.CreditsRepository
	pool                    data.DB
	entitlementSvc          *entitlement.Service
	telegramClient          *telegrambot.Client
	attachmentStore         MessageAttachmentPutStore
	inputNotify             func(ctx context.Context, runID uuid.UUID)
}

func (c telegramConnector) refreshTelegramBotProfile(ctx context.Context, token string, ch *data.Channel) {
	if c.channelsRepo == nil || c.telegramClient == nil || ch == nil || strings.TrimSpace(token) == "" {
		return
	}
	cfg, err := resolveTelegramConfig("telegram", ch.ConfigJSON)
	if err != nil {
		return
	}
	if cfg.TelegramBotUserID != 0 && strings.TrimSpace(cfg.BotUsername) != "" {
		return
	}
	remoteCtx, cancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
	defer cancel()
	info, err := c.telegramClient.GetMe(remoteCtx, strings.TrimSpace(token))
	if err != nil || info == nil {
		return
	}
	merged, changed, err := mergeTelegramBotProfileFromGetMe(ch.ConfigJSON, info)
	if err != nil || !changed {
		return
	}
	upd, err := c.channelsRepo.Update(ctx, ch.ID, ch.AccountID, data.ChannelUpdate{ConfigJSON: &merged})
	if err != nil || upd == nil {
		return
	}
	ch.ConfigJSON = upd.ConfigJSON
}

func isTelegramGroupLikeChatType(chatType string) bool {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

// telegramCommandBase 返回命令名（不含 @bot），如 "/new"。
// 若命令带有 @target 且与 botUsername 不匹配，返回 ok=false（命令非发给本 bot）。
func telegramCommandBase(text, botUsername string) (cmd string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false
	}
	parts := strings.SplitN(fields[0], "@", 2)
	if len(parts) == 2 && parts[1] != "" {
		cleanTarget := strings.ToLower(strings.TrimSpace(parts[1]))
		cleanBot := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(botUsername, "@")))
		if cleanBot == "" || cleanTarget != cleanBot {
			return "", false
		}
	}
	return parts[0], true
}

func (c telegramConnector) persistTelegramGroupPassiveMessageTx(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	token string,
	incoming telegramIncomingMessage,
	identity data.ChannelIdentity,
	persona *data.Persona,
) (uuid.UUID, error) {
	if persona == nil {
		return uuid.Nil, fmt.Errorf("telegram passive ingest: persona required")
	}
	if tx == nil {
		return uuid.Nil, fmt.Errorf("telegram passive ingest: tx required")
	}

	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, incoming)
	if err != nil {
		return uuid.Nil, err
	}
	if c.channelLedgerRepo != nil {
		ledgerMetadata, metaErr := json.Marshal(map[string]any{
			"source":            "telegram",
			"conversation_type": incoming.ChatType,
			"mentions_bot":      incoming.MentionsBot,
			"is_reply_to_bot":   incoming.IsReplyToBot,
		})
		if metaErr != nil {
			return uuid.Nil, metaErr
		}
		if _, ledgerErr := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			ThreadID:                &threadID,
			PlatformConversationID:  incoming.PlatformChatID,
			PlatformMessageID:       incoming.PlatformMsgID,
			PlatformParentMessageID: incoming.ReplyToMsgID,
			PlatformThreadID:        incoming.MessageThreadID,
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMetadata,
		}); ledgerErr != nil {
			return uuid.Nil, ledgerErr
		}
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
		return uuid.Nil, err
	}
	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(
		ctx,
		ch.AccountID,
		threadID,
		"user",
		content,
		contentJSON,
		metadataJSON,
		identity.UserID,
	); err != nil {
		return uuid.Nil, err
	}
	return threadID, nil
}

func (c telegramConnector) HandleUpdate(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	token string,
	update telegramUpdate,
) error {
	if update.Message == nil || update.Message.From == nil {
		return nil
	}
	c.refreshTelegramBotProfile(ctx, token, &ch)
	cfg, err := resolveTelegramConfig(ch.ChannelType, ch.ConfigJSON)
	if err != nil {
		return fmt.Errorf("invalid channel config: %w", err)
	}
	rawPayload, err := json.Marshal(update)
	if err != nil {
		return err
	}
	incoming, err := normalizeTelegramIncomingMessage(ch.ID, ch.ChannelType, rawPayload, update, cfg.BotUsername, cfg.TelegramBotUserID)
	if err != nil {
		return err
	}
	if incoming == nil {
		return nil
	}

	if !telegramUserAllowed(cfg.AllowedUserIDs, incoming.PlatformUserID) {
		if incoming.IsPrivate() && c.telegramClient != nil && strings.TrimSpace(token) != "" {
			sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
			_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
				ChatID: incoming.PlatformChatID,
				Text:   "当前账号未被授权使用这个机器人。",
			})
			sendCancel()
		}
		return nil
	}

	// Both mustValidateTelegramActivation and entitlementSvc.Resolve use non-tx
	// connections. On SQLite (single-connection pool) calling them inside a
	// transaction deadlocks. Resolve everything before BeginTx.
	persona, personaRef, _, err := mustValidateTelegramActivation(ctx, ch.AccountID, c.personasRepo, ch.PersonaID, ch.ConfigJSON)
	if err != nil {
		return err
	}

	if c.tryScheduleTelegramMediaGroup(ctx, traceID, ch, token, update, *incoming, persona) {
		return nil
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	accepted, err := c.channelReceiptsRepo.WithTx(tx).Record(
		ctx,
		ch.ID,
		incoming.PlatformChatID,
		incoming.PlatformMsgID,
	)
	if err != nil {
		return err
	}
	if !accepted {
		return tx.Commit(ctx)
	}

	maybeSendTelegramImmediateTyping(ctx, c.telegramClient, token, incoming.PlatformChatID, cfg, incoming)

	identity, err := upsertTelegramIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), update.Message.From)
	if err != nil {
		return err
	}

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
			return err
		}
		groupIdentity = &gi
	}

	if incoming.IsPrivate() {
		trimmedCommandText := strings.TrimSpace(incoming.CommandText)
		if handled, replyText, err := handleTelegramCommand(
			ctx,
			tx,
			&ch,
			identity,
			trimmedCommandText,
			c.channelBindCodesRepo,
			c.channelIdentitiesRepo,
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
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
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
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
		}
		if ok && strings.HasPrefix(cmd, "/heartbeat") {
			heartbeatIdentity := identity
			if groupIdentity != nil {
				heartbeatIdentity = *groupIdentity
			}
			replyText, err := handleTelegramHeartbeatCommand(ctx, tx, heartbeatIdentity, incoming.CommandText, c.channelIdentitiesRepo)
			if err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
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
						return err
					}
					if threadMap == nil {
						replyText = "当前没有运行中的任务。"
					} else {
						activeRun, err := c.runEventRepo.GetActiveRootRunForThread(ctx, threadMap.ThreadID)
						if err != nil {
							return err
						}
						if activeRun == nil {
							replyText = "当前没有运���中的任务。"
						} else {
							if _, err := c.runEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, identity.UserID, traceID, 0, nil); err != nil {
								return err
							}
							cancelRunID = activeRun.ID
							replyText = "已请求停止当前任务。"
						}
					}
				}
			} else {
				replyText = "当前没有运行中的任务。"
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if cancelRunID != uuid.Nil {
				_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, cancelRunID.String())
			}
			if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
		}
	}

	if !incoming.HasContent() {
		return tx.Commit(ctx)
	}

	if !incoming.IsPrivate() && !incoming.ShouldCreateRun() {
		if _, err := c.persistTelegramGroupPassiveMessageTx(ctx, tx, ch, token, *incoming, identity, persona); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, *incoming)
	if err != nil {
		return err
	}
	if c.channelLedgerRepo != nil {
		ledgerMetadata, metaErr := json.Marshal(map[string]any{
			"source":            "telegram",
			"conversation_type": incoming.ChatType,
			"mentions_bot":      incoming.MentionsBot,
			"is_reply_to_bot":   incoming.IsReplyToBot,
		})
		if metaErr != nil {
			return metaErr
		}
		if _, ledgerErr := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			ThreadID:                &threadID,
			PlatformConversationID:  incoming.PlatformChatID,
			PlatformMessageID:       incoming.PlatformMsgID,
			PlatformParentMessageID: incoming.ReplyToMsgID,
			PlatformThreadID:        incoming.MessageThreadID,
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMetadata,
		}); ledgerErr != nil {
			return ledgerErr
		}
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
		*incoming,
	)
	if err != nil {
		return err
	}
	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(
		ctx,
		ch.AccountID,
		threadID,
		"user",
		content,
		contentJSON,
		metadataJSON,
		identity.UserID,
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
		delivered, err := c.deliverTelegramMessageToActiveRun(ctx, runRepoTx, activeRun, content, traceID)
		if err != nil {
			return err
		}
		if delivered {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			c.notifyActiveRunInput(ctx, activeRun.ID)
			return nil
		}
	}

	if !channelAgentTriggerConsume(ch.ID) {
		return tx.Commit(ctx)
	}

	runStartedData := buildTelegramRunStartedData(personaRef, cfg.DefaultModel)
	run, _, err := c.runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		threadID,
		identity.UserID,
		"run.started",
		runStartedData,
	)
	if err != nil {
		return err
	}
	jobPayload := map[string]any{
		"source":           "telegram",
		"channel_delivery": buildTelegramChannelDeliveryPayload(ch.ID, identity.ID, *incoming),
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(
		ctx,
		ch.AccountID,
		run.ID,
		traceID,
		data.RunExecuteJobType,
		jobPayload,
		nil,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func telegramWebhookEntry(
	channelsRepo *data.ChannelsRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	channelReceiptsRepo *data.ChannelMessageReceiptsRepository,
	secretsRepo *data.SecretsRepository,
	personasRepo *data.PersonasRepository,
	usersRepo *data.UserRepository,
	accountRepo *data.AccountRepository,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	runEventRepo *data.RunEventRepository,
	jobRepo *data.JobRepository,
	creditsRepo *data.CreditsRepository,
	pool data.DB,
	entitlementSvc *entitlement.Service,
	telegramClient *telegrambot.Client,
	messageAttachmentStore MessageAttachmentPutStore,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	var channelLedgerRepo *data.ChannelMessageLedgerRepository
	if pool != nil {
		repo, err := data.NewChannelMessageLedgerRepository(pool)
		if err != nil {
			panic(err)
		}
		channelLedgerRepo = repo
	}
	connector := telegramConnector{
		channelsRepo:            channelsRepo,
		channelIdentitiesRepo:   channelIdentitiesRepo,
		channelBindCodesRepo:    channelBindCodesRepo,
		channelDMThreadsRepo:    channelDMThreadsRepo,
		channelGroupThreadsRepo: channelGroupThreadsRepo,
		channelReceiptsRepo:     channelReceiptsRepo,
		channelLedgerRepo:       channelLedgerRepo,
		personasRepo:            personasRepo,
		usersRepo:               usersRepo,
		accountRepo:             accountRepo,
		membershipRepo:          membershipRepo,
		projectRepo:             projectRepo,
		threadRepo:              threadRepo,
		messageRepo:             messageRepo,
		runEventRepo:            runEventRepo,
		jobRepo:                 jobRepo,
		creditsRepo:             creditsRepo,
		pool:                    pool,
		entitlementSvc:          entitlementSvc,
		telegramClient:          telegramClient,
		attachmentStore:         messageAttachmentStore,
		inputNotify: func(ctx context.Context, runID uuid.UUID) {
			if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String()); err != nil {
				slog.Warn("telegram_active_run_notify_failed", "run_id", runID.String(), "error", err)
			}
		},
	}

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if channelsRepo == nil || channelIdentitiesRepo == nil || channelBindCodesRepo == nil || channelDMThreadsRepo == nil || channelReceiptsRepo == nil ||
			secretsRepo == nil || personasRepo == nil || usersRepo == nil || accountRepo == nil || membershipRepo == nil ||
			projectRepo == nil || threadRepo == nil || messageRepo == nil || runEventRepo == nil || jobRepo == nil || creditsRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		channelID, ok := parseTelegramWebhookChannelID(r.URL.Path)
		if !ok {
			httpkit.WriteNotFound(w, r)
			return
		}

		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid telegram payload", traceID, nil)
			return
		}

		ch, err := channelsRepo.GetByID(r.Context(), channelID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if ch == nil || ch.ChannelType != "telegram" {
			httpkit.WriteNotFound(w, r)
			return
		}
		if !ch.IsActive {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		secret := ""
		if ch.WebhookSecret != nil {
			secret = *ch.WebhookSecret
		}
		if subtle.ConstantTimeCompare(
			[]byte(strings.TrimSpace(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"))),
			[]byte(strings.TrimSpace(secret)),
		) != 1 {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "channels.invalid_signature", "invalid telegram signature", traceID, nil)
			return
		}

		var update telegramUpdate
		if err := json.Unmarshal(rawBody, &update); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid telegram payload", traceID, nil)
			return
		}
		token, err := secretsRepo.DecryptByID(r.Context(), derefUUID(ch.CredentialsID))
		if err != nil || token == nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "telegram token unavailable", traceID, nil)
			return
		}
		if err := connector.HandleUpdate(r.Context(), traceID, *ch, strings.TrimSpace(*token), update); err != nil {
			status := nethttp.StatusInternalServerError
			code := "internal.error"
			message := "internal error"
			if strings.Contains(err.Error(), "persona") || strings.Contains(err.Error(), "allowed_user_ids") {
				status = nethttp.StatusUnprocessableEntity
				code = "validation.error"
				message = err.Error()
			}
			httpkit.WriteError(w, status, code, message, traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	}
}

func (c telegramConnector) resolveTelegramThreadID(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	personaID uuid.UUID,
	projectID uuid.UUID,
	identity data.ChannelIdentity,
	incoming telegramIncomingMessage,
) (uuid.UUID, error) {
	if incoming.IsPrivate() {
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

	threadMap, err := c.channelGroupThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, incoming.PlatformChatID, personaID)
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
	if _, err := c.channelGroupThreadsRepo.WithTx(tx).Create(ctx, ch.ID, incoming.PlatformChatID, personaID, thread.ID); err != nil {
		return uuid.Nil, err
	}
	return thread.ID, nil
}

func (c telegramConnector) deliverTelegramMessageToActiveRun(
	ctx context.Context,
	repo *data.RunEventRepository,
	run *data.Run,
	content, traceID string,
) (bool, error) {
	if run == nil {
		return false, nil
	}
	if strings.TrimSpace(content) == "" {
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

func (c telegramConnector) notifyActiveRunInput(ctx context.Context, runID uuid.UUID) {
	if c.inputNotify == nil || runID == uuid.Nil {
		return
	}
	c.inputNotify(ctx, runID)
}

func buildTelegramRunStartedData(personaRef string, defaultModel string) map[string]any {
	dataJSON := map[string]any{"persona_id": personaRef}
	if model := strings.TrimSpace(defaultModel); model != "" {
		dataJSON["model"] = model
	}
	return dataJSON
}

func buildTelegramChannelDeliveryPayload(
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	incoming telegramIncomingMessage,
) map[string]any {
	payload := map[string]any{
		"channel_id":   channelID.String(),
		"channel_type": "telegram",
		"conversation_ref": map[string]any{
			"target": incoming.PlatformChatID,
		},
		"inbound_message_ref": map[string]any{
			"message_id": incoming.PlatformMsgID,
		},
		"trigger_message_ref": map[string]any{
			"message_id": incoming.PlatformMsgID,
		},
		"platform_chat_id":           incoming.PlatformChatID,
		"platform_message_id":        incoming.PlatformMsgID,
		"reply_to_message_id":        incoming.PlatformMsgID,
		"sender_channel_identity_id": channelIdentityID.String(),
		"conversation_type":          incoming.ChatType,
		"mentions_bot":               incoming.MentionsBot,
		"is_reply_to_bot":            incoming.IsReplyToBot,
	}
	if incoming.ReplyToMsgID != nil && strings.TrimSpace(*incoming.ReplyToMsgID) != "" {
		payload["inbound_reply_to_message_id"] = strings.TrimSpace(*incoming.ReplyToMsgID)
	}
	if incoming.MessageThreadID != nil && strings.TrimSpace(*incoming.MessageThreadID) != "" {
		payload["conversation_ref"].(map[string]any)["thread_id"] = strings.TrimSpace(*incoming.MessageThreadID)
		payload["message_thread_id"] = strings.TrimSpace(*incoming.MessageThreadID)
	}
	return payload
}

func parseTelegramWebhookChannelID(path string) (uuid.UUID, bool) {
	tail := strings.TrimPrefix(path, "/v1/channels/telegram/")
	tail = strings.TrimSuffix(tail, "/webhook")
	tail = strings.Trim(tail, "/")
	if tail == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(tail)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func telegramUserAllowed(allowed []string, userID string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if item == userID {
			return true
		}
	}
	return false
}

func upsertTelegramIdentity(ctx context.Context, repo *data.ChannelIdentitiesRepository, from *telegramUser) (data.ChannelIdentity, error) {
	displayName := formatTelegramDisplayName(from)
	metadata, err := json.Marshal(map[string]any{
		"username":   trimOptional(from.Username),
		"first_name": trimOptional(from.FirstName),
		"last_name":  trimOptional(from.LastName),
		"is_bot":     from.IsBot,
	})
	if err != nil {
		return data.ChannelIdentity{}, err
	}
	return repo.Upsert(
		ctx,
		"telegram",
		strconv.FormatInt(from.ID, 10),
		displayName,
		nil,
		metadata,
	)
}

func formatTelegramDisplayName(from *telegramUser) *string {
	if from == nil {
		return nil
	}
	parts := []string{
		trimOptional(from.FirstName),
		trimOptional(from.LastName),
	}
	text := strings.TrimSpace(strings.Join(parts, " "))
	if text != "" {
		return &text
	}
	if from.Username != nil && strings.TrimSpace(*from.Username) != "" {
		value := strings.TrimSpace(*from.Username)
		return &value
	}
	return nil
}

func trimOptional(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func handleTelegramCommand(
	ctx context.Context,
	tx pgx.Tx,
	channel *data.Channel,
	identity data.ChannelIdentity,
	text string,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	threadRepo *data.ThreadRepository,
	runEventRepo *data.RunEventRepository,
	pool data.DB,
) (bool, string, error) {
	if !strings.HasPrefix(text, "/") {
		return false, "", nil
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false, "", nil
	}
	command := strings.TrimSpace(parts[0])
	switch {
	case command == "/help":
		return true, "/start — 查看连接状态\n/bind <code> — 绑定你的账号\n/new — 开启新会话\n/stop — 停止当前任务\n/help — 显示此帮助", nil
	case command == "/start":
		if len(parts) > 1 && strings.HasPrefix(parts[1], "bind_") {
			replyText, err := bindTelegramIdentity(ctx, tx, channel, identity, strings.TrimPrefix(parts[1], "bind_"), channelBindCodesRepo, channelIdentitiesRepo, channelDMThreadsRepo, threadRepo)
			return true, replyText, err
		}
		return true, "已连接 Arkloop\n\n使用 /bind <code> 绑定账号\n私聊直接发消息开始对话，/new 开启新会话\n群内 @bot 触发对话，管理员可用 /new 重置会话", nil
	case command == "/bind":
		if len(parts) < 2 {
			return true, "用法：/bind <code>", nil
		}
		replyText, err := bindTelegramIdentity(ctx, tx, channel, identity, parts[1], channelBindCodesRepo, channelIdentitiesRepo, channelDMThreadsRepo, threadRepo)
		return true, replyText, err
	case command == "/new":
		if channel == nil || channel.PersonaID == nil || *channel.PersonaID == uuid.Nil {
			return true, "当前会话未配置 persona。", nil
		}
		if err := channelDMThreadsRepo.WithTx(tx).DeleteByBinding(ctx, channel.ID, identity.ID, *channel.PersonaID); err != nil {
			return true, "", err
		}
		return true, "已开启新会话。", nil
	case command == "/stop":
		if channel == nil || channel.PersonaID == nil || *channel.PersonaID == uuid.Nil {
			return true, "当前没有运行中的任务。", nil
		}
		dmThread, err := channelDMThreadsRepo.GetByBinding(ctx, channel.ID, identity.ID, *channel.PersonaID)
		if err != nil {
			return true, "", err
		}
		if dmThread == nil {
			return true, "当前没有运行中的任务。", nil
		}
		activeRun, err := runEventRepo.GetActiveRootRunForThread(ctx, dmThread.ThreadID)
		if err != nil {
			return true, "", err
		}
		if activeRun == nil {
			return true, "当前没有运行中的任务。", nil
		}
		if _, err := runEventRepo.RequestCancel(ctx, activeRun.ID, identity.UserID, "", 0, nil); err != nil {
			return true, "", err
		}
		_, _ = pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, activeRun.ID.String())
		return true, "已请求停止当前任务。", nil
	default:
		return false, "", nil
	}
}

// handleTelegramHeartbeatCommand 处理群内 /heartbeat 命令。
// 支持：/heartbeat、/heartbeat on、/heartbeat off、/heartbeat interval N、/heartbeat model NAME
func handleTelegramHeartbeatCommand(
	ctx context.Context,
	tx pgx.Tx,
	identity data.ChannelIdentity,
	rawText string,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
) (string, error) {
	parts := strings.Fields(rawText)

	enabled, intervalMin, model, err := channelIdentitiesRepo.WithTx(tx).GetHeartbeatConfig(ctx, identity.ID)
	if err != nil {
		return "", err
	}

	if len(parts) == 1 {
		status := "关闭"
		if enabled {
			status = "开启"
		}
		modelDisplay := "跟随对话"
		if strings.TrimSpace(model) != "" {
			modelDisplay = model
		}
		return fmt.Sprintf("心跳：%s\n间隔：%d 分钟\n模型：%s", status, intervalMin, modelDisplay), nil
	}

	sub := strings.TrimSpace(parts[1])
	switch sub {
	case "on":
		if intervalMin <= 0 {
			intervalMin = runkind.DefaultHeartbeatIntervalMinutes
		}
		if err := channelIdentitiesRepo.WithTx(tx).UpdateHeartbeatConfig(ctx, identity.ID, true, intervalMin, model); err != nil {
			return "", err
		}
		return fmt.Sprintf("心跳已开启（间隔 %d 分钟）。", intervalMin), nil
	case "off":
		if err := channelIdentitiesRepo.WithTx(tx).UpdateHeartbeatConfig(ctx, identity.ID, false, intervalMin, model); err != nil {
			return "", err
		}
		return "心跳已关闭。", nil
	case "interval":
		if len(parts) < 3 {
			return "用法：/heartbeat interval <分钟数>", nil
		}
		n, parseErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		if parseErr != nil || n <= 0 {
			return "间隔必须是正整数（分钟）。", nil
		}
		if err := channelIdentitiesRepo.WithTx(tx).UpdateHeartbeatConfig(ctx, identity.ID, enabled, n, model); err != nil {
			return "", err
		}
		return fmt.Sprintf("心跳间隔已设为 %d 分钟。", n), nil
	case "model":
		newModel := ""
		if len(parts) >= 3 {
			newModel = strings.TrimSpace(parts[2])
		}
		if err := channelIdentitiesRepo.WithTx(tx).UpdateHeartbeatConfig(ctx, identity.ID, enabled, intervalMin, newModel); err != nil {
			return "", err
		}
		if newModel == "" {
			return "心跳模型已设为跟随对话。", nil
		}
		return fmt.Sprintf("心跳模型已设为 %s。", newModel), nil
	default:
		return "可用子命令：on、off、interval <分钟>、model <模型名>", nil
	}
}

func bindTelegramIdentity(
	ctx context.Context,
	tx pgx.Tx,
	channel *data.Channel,
	identity data.ChannelIdentity,
	code string,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	threadRepo *data.ThreadRepository,
) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "绑定码不能为空。", nil
	}
	activeCode, err := channelBindCodesRepo.WithTx(tx).GetActiveByToken(ctx, code)
	if err != nil {
		return "", err
	}
	if activeCode == nil || (activeCode.ChannelType != nil && *activeCode.ChannelType != channel.ChannelType) {
		return "绑定码无效或已过期。", nil
	}
	if identity.UserID != nil && *identity.UserID != activeCode.IssuedByUserID {
		return "当前 Telegram 身份已绑定到其他账号。", nil
	}
	if identity.UserID != nil {
		if _, err := channelBindCodesRepo.WithTx(tx).ConsumeForChannel(ctx, code, identity.ID, channel.ChannelType); err != nil {
			return "", err
		}
		return "账号已绑定。", nil
	}

	consumed, err := channelBindCodesRepo.WithTx(tx).ConsumeForChannel(ctx, code, identity.ID, channel.ChannelType)
	if err != nil {
		return "", err
	}
	if consumed == nil {
		return "绑定码无效或已过期。", nil
	}
	if err := channelIdentitiesRepo.WithTx(tx).UpdateUserID(ctx, identity.ID, &consumed.IssuedByUserID); err != nil {
		return "", err
	}
	threadMappings, err := channelDMThreadsRepo.WithTx(tx).ListByChannelIdentity(ctx, channel.ID, identity.ID)
	if err != nil {
		return "", err
	}
	for _, threadMap := range threadMappings {
		if _, err := threadRepo.WithTx(tx).UpdateOwner(ctx, threadMap.ThreadID, &consumed.IssuedByUserID); err != nil {
			return "", err
		}
	}
	return "绑定成功。", nil
}

func renderTelegramInboundMessage(identity data.ChannelIdentity, text string, unixTS int64) string {
	displayName := identity.PlatformSubjectID
	if identity.DisplayName != nil && strings.TrimSpace(*identity.DisplayName) != "" {
		displayName = strings.TrimSpace(*identity.DisplayName)
	}
	return fmt.Sprintf(`---
channel-identity-id: "%s"
display-name: "%s"
channel: "telegram"
conversation-type: "private"
time: "%s"
---
%s`,
		identity.ID.String(),
		displayName,
		formatTelegramTimestamp(unixTS),
		strings.TrimSpace(text),
	)
}

func formatTelegramTimestamp(unixTS int64) string {
	if unixTS <= 0 {
		return ""
	}
	return time.Unix(unixTS, 0).UTC().Format(time.RFC3339)
}

func derefUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

// HandleUpdateForPoll 是 HandleUpdate 的轮询路径变体。
func (c telegramConnector) HandleUpdateForPoll(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	token string,
	update telegramUpdate,
) (err error) {
	handleStart := time.Now()
	txStarted := false
	logPhase := func(phase string, extra ...any) {
		fields := []any{
			"phase",
			phase,
			"channel_id",
			ch.ID.String(),
			"trace_id",
			traceID,
			"update_id",
			update.UpdateID,
			"elapsed_ms",
			int(time.Since(handleStart).Milliseconds()),
		}
		fields = append(fields, extra...)
		slog.DebugContext(ctx, "telegram_poll_phase", fields...)
	}
	if update.Message == nil || update.Message.From == nil {
		return nil
	}
	c.refreshTelegramBotProfile(ctx, token, &ch)
	cfg, err := resolveTelegramConfig(ch.ChannelType, ch.ConfigJSON)
	if err != nil {
		return fmt.Errorf("invalid channel config: %w", err)
	}
	rawPayload, err := json.Marshal(update)
	if err != nil {
		return err
	}
	incoming, err := normalizeTelegramIncomingMessage(ch.ID, ch.ChannelType, rawPayload, update, cfg.BotUsername, cfg.TelegramBotUserID)
	if err != nil {
		return err
	}
	if incoming == nil {
		return nil
	}

	if !telegramUserAllowed(cfg.AllowedUserIDs, incoming.PlatformUserID) {
		if incoming.IsPrivate() && c.telegramClient != nil && strings.TrimSpace(token) != "" {
			sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
			_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
				ChatID: incoming.PlatformChatID,
				Text:   "当前账号未被授权使用这个机器人。",
			})
			sendCancel()
		}
		return nil
	}

	persona, personaRef, _, err := mustValidateTelegramActivation(ctx, ch.AccountID, c.personasRepo, ch.PersonaID, ch.ConfigJSON)
	if err != nil {
		return err
	}

	if c.tryScheduleTelegramMediaGroup(ctx, traceID, ch, token, update, *incoming, persona) {
		return nil
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	txStarted = true
	defer tx.Rollback(ctx) //nolint:errcheck
	logPhase("tx_begin")
	defer func() {
		if !txStarted {
			return
		}
		if err != nil {
			logPhase("tx_rollback", "rollback_error", err.Error())
		} else {
			logPhase("tx_success")
		}
	}()

	accepted, err := c.channelReceiptsRepo.WithTx(tx).Record(
		ctx,
		ch.ID,
		incoming.PlatformChatID,
		incoming.PlatformMsgID,
	)
	if err != nil {
		return err
	}
	if !accepted {
		return tx.Commit(ctx)
	}

	maybeSendTelegramImmediateTyping(ctx, c.telegramClient, token, incoming.PlatformChatID, cfg, incoming)

	identity, err := upsertTelegramIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), update.Message.From)
	if err != nil {
		return err
	}

	// 群消息额外 upsert 群自身的 identity（heartbeat 配置挂在群上）
	var groupIdentity *data.ChannelIdentity
	if isTelegramGroupLikeChatType(incoming.ChatType) {
		gi, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(
			ctx,
			incoming.ChannelType,
			incoming.PlatformChatID,
			nil,
			nil,
			nil,
		)
		if err != nil {
			return err
		}
		groupIdentity = &gi
	}

	if incoming.IsPrivate() {
		trimmedCommandText := strings.TrimSpace(incoming.CommandText)
		if handled, replyText, err := handleTelegramCommand(
			ctx,
			tx,
			&ch,
			identity,
			trimmedCommandText,
			c.channelBindCodesRepo,
			c.channelIdentitiesRepo,
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
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
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
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
		}
		if ok && strings.HasPrefix(cmd, "/heartbeat") {
			heartbeatIdentity := identity
			if groupIdentity != nil {
				heartbeatIdentity = *groupIdentity
			}
			slog.DebugContext(ctx, "heartbeat_cmd: dispatching",
				"cmd", incoming.CommandText,
				"group_identity_nil", groupIdentity == nil,
				"heartbeat_identity_id", heartbeatIdentity.ID,
				"chat_id", incoming.PlatformChatID,
			)
			replyText, err := handleTelegramHeartbeatCommand(ctx, tx, heartbeatIdentity, incoming.CommandText, c.channelIdentitiesRepo)
			if err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
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
						return err
					}
					if threadMap == nil {
						replyText = "当前没有运行中的任务。"
					} else {
						activeRun, err := c.runEventRepo.GetActiveRootRunForThread(ctx, threadMap.ThreadID)
						if err != nil {
							return err
						}
						if activeRun == nil {
							replyText = "当前没有运行中的任务。"
						} else {
							if _, err := c.runEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, identity.UserID, traceID, 0, nil); err != nil {
								return err
							}
							cancelRunID = activeRun.ID
							replyText = "已请求停止当前任务。"
						}
					}
				}
			} else {
				replyText = "当前没有运行中的任务。"
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			if cancelRunID != uuid.Nil {
				_, _ = c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, cancelRunID.String())
			}
			if c.telegramClient != nil && strings.TrimSpace(token) != "" {
				sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
				_, _ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
					ChatID: incoming.PlatformChatID,
					Text:   replyText,
				})
				sendCancel()
			}
			return nil
		}
	}

	if !incoming.HasContent() {
		return tx.Commit(ctx)
	}

	if !incoming.IsPrivate() && !incoming.ShouldCreateRun() {
		if _, err := c.persistTelegramGroupPassiveMessageTx(ctx, tx, ch, token, *incoming, identity, persona); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// active 路径：与 HandleUpdate 完全一致
	threadProjectID := derefUUID(persona.ProjectID)
	threadID, err := c.resolveTelegramThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, *incoming)
	if err != nil {
		return err
	}
	if c.channelLedgerRepo != nil {
		ledgerMetadata, metaErr := json.Marshal(map[string]any{
			"source":            "telegram",
			"conversation_type": incoming.ChatType,
			"mentions_bot":      incoming.MentionsBot,
			"is_reply_to_bot":   incoming.IsReplyToBot,
		})
		if metaErr != nil {
			return metaErr
		}
		if _, ledgerErr := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			ThreadID:                &threadID,
			PlatformConversationID:  incoming.PlatformChatID,
			PlatformMessageID:       incoming.PlatformMsgID,
			PlatformParentMessageID: incoming.ReplyToMsgID,
			PlatformThreadID:        incoming.MessageThreadID,
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMetadata,
		}); ledgerErr != nil {
			return ledgerErr
		}
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
		*incoming,
	)
	if err != nil {
		return err
	}
	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(
		ctx,
		ch.AccountID,
		threadID,
		"user",
		content,
		contentJSON,
		metadataJSON,
		identity.UserID,
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
		delivered, err := c.deliverTelegramMessageToActiveRun(ctx, runRepoTx, activeRun, content, traceID)
		if err != nil {
			return err
		}
		if delivered {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			c.notifyActiveRunInput(ctx, activeRun.ID)
			return nil
		}
	}

	if !channelAgentTriggerConsume(ch.ID) {
		return tx.Commit(ctx)
	}

	runStartedData := buildTelegramRunStartedData(personaRef, cfg.DefaultModel)
	run, _, err := c.runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		threadID,
		identity.UserID,
		"run.started",
		runStartedData,
	)
	if err != nil {
		return err
	}
	jobPayload := map[string]any{
		"source":           "telegram",
		"channel_delivery": buildTelegramChannelDeliveryPayload(ch.ID, identity.ID, *incoming),
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(
		ctx,
		ch.AccountID,
		run.ID,
		traceID,
		data.RunExecuteJobType,
		jobPayload,
		nil,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
