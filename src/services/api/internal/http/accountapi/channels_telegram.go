package accountapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var telegramUserIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

type telegramChannelConfig struct {
	AllowedUserIDs []string `json:"allowed_user_ids"`
}

type telegramUpdate struct {
	Message *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64         `json:"message_id"`
	Date      int64         `json:"date"`
	Text      string        `json:"text"`
	Chat      telegramChat  `json:"chat"`
	From      *telegramUser `json:"from"`
}

type telegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type telegramUser struct {
	ID        int64   `json:"id"`
	IsBot     bool    `json:"is_bot"`
	Username  *string `json:"username"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
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
	if len(cfg.AllowedUserIDs) == 0 {
		return nil, "", telegramChannelConfig{}, fmt.Errorf("telegram channel requires allowed_user_ids before activation")
	}
	return persona, buildPersonaRef(*persona), cfg, nil
}

func buildPersonaRef(persona data.Persona) string {
	if strings.TrimSpace(persona.Version) == "" {
		return strings.TrimSpace(persona.PersonaKey)
	}
	return fmt.Sprintf("%s@%s", strings.TrimSpace(persona.PersonaKey), strings.TrimSpace(persona.Version))
}

func configureTelegramRemote(
	ctx context.Context,
	client *telegrambot.Client,
	token string,
	channel data.Channel,
) error {
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
	if err := client.SetWebhook(ctx, token, telegrambot.SetWebhookRequest{
		URL:         strings.TrimSpace(*channel.WebhookURL),
		SecretToken: secret,
		Updates:     []string{"message"},
	}); err != nil {
		return err
	}
	return client.SetMyCommands(ctx, token, []telegrambot.BotCommand{
		{Command: "start", Description: "开始使用"},
		{Command: "help", Description: "查看帮助"},
		{Command: "bind", Description: "绑定账号"},
	})
}

func disableTelegramRemote(ctx context.Context, client *telegrambot.Client, token string) error {
	if client == nil {
		return fmt.Errorf("telegram client not configured")
	}
	return client.DeleteWebhook(ctx, token)
}

func telegramWebhookEntry(
	channelsRepo *data.ChannelsRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
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
) func(nethttp.ResponseWriter, *nethttp.Request) {
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
		if update.Message == nil || update.Message.Chat.Type != "private" || update.Message.From == nil {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		cfg, err := resolveTelegramConfig(ch.ChannelType, ch.ConfigJSON)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "invalid channel config", traceID, nil)
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context()) //nolint:errcheck

		txReceiptsRepo := channelReceiptsRepo.WithTx(tx)
		accepted, err := txReceiptsRepo.Record(
			r.Context(),
			ch.ID,
			strconv.FormatInt(update.Message.Chat.ID, 10),
			strconv.FormatInt(update.Message.MessageID, 10),
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if !accepted {
			if err := tx.Commit(r.Context()); err == nil {
				httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		token, err := secretsRepo.DecryptByID(r.Context(), derefUUID(ch.CredentialsID))
		if err != nil || token == nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "telegram token unavailable", traceID, nil)
			return
		}

		senderUserID := strconv.FormatInt(update.Message.From.ID, 10)
		if !telegramUserAllowed(cfg.AllowedUserIDs, senderUserID) {
			if err := tx.Commit(r.Context()); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			_ = telegramClient.SendMessage(r.Context(), *token, telegrambot.SendMessageRequest{
				ChatID: senderUserID,
				Text:   "当前账号未被授权使用这个机器人。",
			})
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		persona, personaRef, _, err := mustValidateTelegramActivation(r.Context(), ch.AccountID, personasRepo, ch.PersonaID, ch.ConfigJSON)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}

		identity, err := upsertTelegramIdentity(r.Context(), channelIdentitiesRepo.WithTx(tx), update.Message.From)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		trimmedText := strings.TrimSpace(update.Message.Text)
		if handled, replyText, err := handleTelegramCommand(
			r.Context(),
			tx,
			ch,
			identity,
			trimmedText,
			channelBindCodesRepo,
			channelIdentitiesRepo,
			channelDMThreadsRepo,
			threadRepo,
			usersRepo,
		); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", err.Error(), traceID, nil)
			return
		} else if handled {
			if err := tx.Commit(r.Context()); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			_ = telegramClient.SendMessage(r.Context(), *token, telegrambot.SendMessageRequest{
				ChatID: senderUserID,
				Text:   replyText,
			})
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}
		if trimmedText == "" {
			if err := tx.Commit(r.Context()); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
			return
		}

		if identity.UserID == nil {
			shadowUser, bootstrapErr := bootstrapTelegramShadowUser(
				r.Context(),
				tx,
				usersRepo,
				accountRepo,
				membershipRepo,
				projectRepo,
				creditsRepo,
				entitlementSvc,
				update.Message.From.ID,
			)
			if bootstrapErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := channelIdentitiesRepo.WithTx(tx).UpdateUserID(r.Context(), identity.ID, &shadowUser.ID); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			identity.UserID = &shadowUser.ID
		}

		threadProjectID := derefUUID(persona.ProjectID)
		txDMThreadsRepo := channelDMThreadsRepo.WithTx(tx)
		threadMap, err := txDMThreadsRepo.GetByBinding(r.Context(), ch.ID, identity.ID, persona.ID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		var threadID uuid.UUID
		if threadMap == nil {
			thread, err := threadRepo.WithTx(tx).Create(r.Context(), ch.AccountID, identity.UserID, threadProjectID, nil, false)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			threadID = thread.ID
			if _, err := txDMThreadsRepo.Create(r.Context(), ch.ID, identity.ID, persona.ID, thread.ID); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		} else {
			threadID = threadMap.ThreadID
		}

		content := renderTelegramInboundMessage(identity, trimmedText, update.Message.Date)
		if _, err := messageRepo.WithTx(tx).Create(r.Context(), ch.AccountID, threadID, "user", content, identity.UserID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		run, _, err := runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
			r.Context(),
			ch.AccountID,
			threadID,
			identity.UserID,
			"run.started",
			map[string]any{
				"persona_id": personaRef,
			},
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		_, err = jobRepo.WithTx(tx).EnqueueRun(
			r.Context(),
			ch.AccountID,
			run.ID,
			traceID,
			data.RunExecuteJobType,
			map[string]any{
				"source": "telegram",
				"channel_delivery": map[string]any{
					"channel_id":                 ch.ID.String(),
					"channel_type":               "telegram",
					"platform_chat_id":           strconv.FormatInt(update.Message.Chat.ID, 10),
					"sender_channel_identity_id": identity.ID.String(),
				},
			},
			nil,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	}
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

func bootstrapTelegramShadowUser(
	ctx context.Context,
	tx pgx.Tx,
	usersRepo *data.UserRepository,
	accountRepo *data.AccountRepository,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	creditsRepo *data.CreditsRepository,
	entitlementSvc *entitlement.Service,
	telegramUserID int64,
) (data.User, error) {
	user, err := usersRepo.WithTx(tx).CreateShadow(ctx, fmt.Sprintf("tg_shadow_%d", telegramUserID), "channel_shadow")
	if err != nil {
		return data.User{}, err
	}
	account, err := accountRepo.WithTx(tx).Create(
		ctx,
		fmt.Sprintf("personal-shadow-%s", strings.ReplaceAll(user.ID.String(), "-", "")[:12]),
		fmt.Sprintf("Telegram %d", telegramUserID),
		"personal",
	)
	if err != nil {
		return data.User{}, err
	}
	if _, err := membershipRepo.WithTx(tx).Create(ctx, account.ID, user.ID, auth.RoleAccountAdmin); err != nil {
		return data.User{}, err
	}
	if _, err := projectRepo.WithTx(tx).CreateDefaultForOwner(ctx, account.ID, user.ID); err != nil {
		return data.User{}, err
	}
	initialGrant := int64(1000)
	if entitlementSvc != nil {
		if value, err := entitlementSvc.Resolve(ctx, account.ID, "credit.initial_grant"); err == nil {
			if v := value.Int(); v > 0 {
				initialGrant = v
			}
		}
	}
	if _, err := creditsRepo.WithTx(tx).InitBalance(ctx, account.ID, initialGrant); err != nil {
		return data.User{}, err
	}
	return user, nil
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
	usersRepo *data.UserRepository,
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
		return true, "可用命令：/start /help /bind <code>", nil
	case command == "/start":
		if len(parts) > 1 && strings.HasPrefix(parts[1], "bind_") {
			replyText, err := bindTelegramIdentity(ctx, tx, channel, identity, strings.TrimPrefix(parts[1], "bind_"), channelBindCodesRepo, channelIdentitiesRepo, channelDMThreadsRepo, threadRepo, usersRepo)
			return true, replyText, err
		}
		return true, "已连接 Arkloop。使用 /bind <code> 绑定账号。", nil
	case command == "/bind":
		if len(parts) < 2 {
			return true, "用法：/bind <code>", nil
		}
		replyText, err := bindTelegramIdentity(ctx, tx, channel, identity, parts[1], channelBindCodesRepo, channelIdentitiesRepo, channelDMThreadsRepo, threadRepo, usersRepo)
		return true, replyText, err
	default:
		return false, "", nil
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
	usersRepo *data.UserRepository,
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
	if identity.UserID != nil {
		shadow := false
		if usersRepo != nil {
			currentUser, err := usersRepo.WithTx(tx).GetByID(ctx, *identity.UserID)
			if err != nil {
				return "", err
			}
			if currentUser != nil && strings.HasPrefix(strings.TrimSpace(currentUser.Source), "channel_shadow") {
				shadow = true
			}
		}
		if !shadow && *identity.UserID != activeCode.IssuedByUserID {
			return "当前 Telegram 身份已绑定到其他账号。", nil
		}
		if !shadow {
			if _, err := channelBindCodesRepo.WithTx(tx).ConsumeForChannel(ctx, code, identity.ID, channel.ChannelType); err != nil {
				return "", err
			}
			return "账号已绑定。", nil
		}
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
