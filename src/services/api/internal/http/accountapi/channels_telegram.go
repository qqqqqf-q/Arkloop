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

const telegramRemoteRequestTimeout = 5 * time.Second

type telegramChannelConfig struct {
	AllowedUserIDs []string `json:"allowed_user_ids"`
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
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
	channelIdentitiesRepo *data.ChannelIdentitiesRepository
	channelBindCodesRepo  *data.ChannelBindCodesRepository
	channelDMThreadsRepo  *data.ChannelDMThreadsRepository
	channelReceiptsRepo   *data.ChannelMessageReceiptsRepository
	personasRepo          *data.PersonasRepository
	usersRepo             *data.UserRepository
	accountRepo           *data.AccountRepository
	membershipRepo        *data.AccountMembershipRepository
	projectRepo           *data.ProjectRepository
	threadRepo            *data.ThreadRepository
	messageRepo           *data.MessageRepository
	runEventRepo          *data.RunEventRepository
	jobRepo               *data.JobRepository
	creditsRepo           *data.CreditsRepository
	pool                  data.DB
	entitlementSvc        *entitlement.Service
	telegramClient        *telegrambot.Client
}

func (c telegramConnector) HandleUpdate(
	ctx context.Context,
	traceID string,
	ch data.Channel,
	token string,
	update telegramUpdate,
) error {
	if update.Message == nil || update.Message.Chat.Type != "private" || update.Message.From == nil {
		return nil
	}
	cfg, err := resolveTelegramConfig(ch.ChannelType, ch.ConfigJSON)
	if err != nil {
		return fmt.Errorf("invalid channel config: %w", err)
	}

	senderUserID := strconv.FormatInt(update.Message.From.ID, 10)
	if !telegramUserAllowed(cfg.AllowedUserIDs, senderUserID) {
		if c.telegramClient != nil && strings.TrimSpace(token) != "" {
			sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
			_ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
				ChatID: senderUserID,
				Text:   "当前账号未被授权使用这个机器人。",
			})
			sendCancel()
		}
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
		strconv.FormatInt(update.Message.Chat.ID, 10),
		strconv.FormatInt(update.Message.MessageID, 10),
	)
	if err != nil {
		return err
	}
	if !accepted {
		return tx.Commit(ctx)
	}

	persona, personaRef, _, err := mustValidateTelegramActivation(ctx, ch.AccountID, c.personasRepo, ch.PersonaID, ch.ConfigJSON)
	if err != nil {
		return err
	}

	identity, err := upsertTelegramIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), update.Message.From)
	if err != nil {
		return err
	}

	trimmedText := strings.TrimSpace(update.Message.Text)
	if handled, replyText, err := handleTelegramCommand(
		ctx,
		tx,
		&ch,
		identity,
		trimmedText,
		c.channelBindCodesRepo,
		c.channelIdentitiesRepo,
		c.channelDMThreadsRepo,
		c.threadRepo,
		c.usersRepo,
	); err != nil {
		return err
	} else if handled {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		if c.telegramClient != nil && strings.TrimSpace(token) != "" {
			sendCtx, sendCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
			_ = c.telegramClient.SendMessage(sendCtx, token, telegrambot.SendMessageRequest{
				ChatID: senderUserID,
				Text:   replyText,
			})
			sendCancel()
		}
		return nil
	}
	if trimmedText == "" {
		return tx.Commit(ctx)
	}

	if identity.UserID == nil {
		shadowUser, bootstrapErr := bootstrapTelegramShadowUser(
			ctx,
			tx,
			c.usersRepo,
			c.accountRepo,
			c.membershipRepo,
			c.projectRepo,
			c.creditsRepo,
			c.entitlementSvc,
			update.Message.From.ID,
		)
		if bootstrapErr != nil {
			return bootstrapErr
		}
		if err := c.channelIdentitiesRepo.WithTx(tx).UpdateUserID(ctx, identity.ID, &shadowUser.ID); err != nil {
			return err
		}
		identity.UserID = &shadowUser.ID
	}

	threadProjectID := derefUUID(persona.ProjectID)
	threadMap, err := c.channelDMThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, identity.ID, persona.ID)
	if err != nil {
		return err
	}
	var threadID uuid.UUID
	if threadMap == nil {
		thread, err := c.threadRepo.WithTx(tx).Create(ctx, ch.AccountID, identity.UserID, threadProjectID, nil, false)
		if err != nil {
			return err
		}
		threadID = thread.ID
		if _, err := c.channelDMThreadsRepo.WithTx(tx).Create(ctx, ch.ID, identity.ID, persona.ID, thread.ID); err != nil {
			return err
		}
	} else {
		threadID = threadMap.ThreadID
	}

	content := renderTelegramInboundMessage(identity, trimmedText, update.Message.Date)
	if _, err := c.messageRepo.WithTx(tx).Create(ctx, ch.AccountID, threadID, "user", content, identity.UserID); err != nil {
		return err
	}

	run, _, err := c.runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		threadID,
		identity.UserID,
		"run.started",
		map[string]any{"persona_id": personaRef},
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
			"source": "telegram",
			"channel_delivery": map[string]any{
				"channel_id":                 ch.ID.String(),
				"channel_type":               "telegram",
				"platform_chat_id":           strconv.FormatInt(update.Message.Chat.ID, 10),
				"sender_channel_identity_id": identity.ID.String(),
			},
		},
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
	connector := telegramConnector{
		channelIdentitiesRepo: channelIdentitiesRepo,
		channelBindCodesRepo:  channelBindCodesRepo,
		channelDMThreadsRepo:  channelDMThreadsRepo,
		channelReceiptsRepo:   channelReceiptsRepo,
		personasRepo:          personasRepo,
		usersRepo:             usersRepo,
		accountRepo:           accountRepo,
		membershipRepo:        membershipRepo,
		projectRepo:           projectRepo,
		threadRepo:            threadRepo,
		messageRepo:           messageRepo,
		runEventRepo:          runEventRepo,
		jobRepo:               jobRepo,
		creditsRepo:           creditsRepo,
		pool:                  pool,
		entitlementSvc:        entitlementSvc,
		telegramClient:        telegramClient,
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

type TelegramDesktopPollerDeps struct {
	ChannelsRepo          *data.ChannelsRepository
	ChannelIdentitiesRepo *data.ChannelIdentitiesRepository
	ChannelBindCodesRepo  *data.ChannelBindCodesRepository
	ChannelDMThreadsRepo  *data.ChannelDMThreadsRepository
	ChannelReceiptsRepo   *data.ChannelMessageReceiptsRepository
	SecretsRepo           *data.SecretsRepository
	PersonasRepo          *data.PersonasRepository
	UsersRepo             *data.UserRepository
	AccountRepo           *data.AccountRepository
	AccountMembershipRepo *data.AccountMembershipRepository
	ProjectRepo           *data.ProjectRepository
	ThreadRepo            *data.ThreadRepository
	MessageRepo           *data.MessageRepository
	RunEventRepo          *data.RunEventRepository
	JobRepo               *data.JobRepository
	CreditsRepo           *data.CreditsRepository
	Pool                  data.DB
	EntitlementService    *entitlement.Service
	TelegramBotClient     *telegrambot.Client
	PollInterval          time.Duration
	PollLimit             int
}

func StartTelegramDesktopPoller(ctx context.Context, deps TelegramDesktopPollerDeps) {
	if ctx == nil ||
		deps.ChannelsRepo == nil ||
		deps.ChannelIdentitiesRepo == nil ||
		deps.ChannelBindCodesRepo == nil ||
		deps.ChannelDMThreadsRepo == nil ||
		deps.ChannelReceiptsRepo == nil ||
		deps.SecretsRepo == nil ||
		deps.PersonasRepo == nil ||
		deps.UsersRepo == nil ||
		deps.AccountRepo == nil ||
		deps.AccountMembershipRepo == nil ||
		deps.ProjectRepo == nil ||
		deps.ThreadRepo == nil ||
		deps.MessageRepo == nil ||
		deps.RunEventRepo == nil ||
		deps.JobRepo == nil ||
		deps.CreditsRepo == nil ||
		deps.Pool == nil {
		return
	}

	client := deps.TelegramBotClient
	if client == nil {
		client = telegrambot.NewClient("", nil)
	}
	interval := deps.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	limit := deps.PollLimit
	if limit <= 0 {
		limit = 20
	}

	connector := telegramConnector{
		channelIdentitiesRepo: deps.ChannelIdentitiesRepo,
		channelBindCodesRepo:  deps.ChannelBindCodesRepo,
		channelDMThreadsRepo:  deps.ChannelDMThreadsRepo,
		channelReceiptsRepo:   deps.ChannelReceiptsRepo,
		personasRepo:          deps.PersonasRepo,
		usersRepo:             deps.UsersRepo,
		accountRepo:           deps.AccountRepo,
		membershipRepo:        deps.AccountMembershipRepo,
		projectRepo:           deps.ProjectRepo,
		threadRepo:            deps.ThreadRepo,
		messageRepo:           deps.MessageRepo,
		runEventRepo:          deps.RunEventRepo,
		jobRepo:               deps.JobRepo,
		creditsRepo:           deps.CreditsRepo,
		pool:                  deps.Pool,
		entitlementSvc:        deps.EntitlementService,
		telegramClient:        client,
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		offsets := make(map[uuid.UUID]int64)
		for {
			_ = pollTelegramDesktopOnce(ctx, client, connector, deps.ChannelsRepo, deps.SecretsRepo, offsets, limit)

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func pollTelegramDesktopOnce(
	ctx context.Context,
	client *telegrambot.Client,
	connector telegramConnector,
	channelsRepo *data.ChannelsRepository,
	secretsRepo *data.SecretsRepository,
	offsets map[uuid.UUID]int64,
	limit int,
) error {
	channels, err := channelsRepo.ListActiveByType(ctx, "telegram")
	if err != nil {
		return err
	}
	for _, ch := range channels {
		token, err := secretsRepo.DecryptByID(ctx, derefUUID(ch.CredentialsID))
		if err != nil || token == nil || strings.TrimSpace(*token) == "" {
			continue
		}

		req := telegrambot.GetUpdatesRequest{
			Limit:   limit,
			Updates: []string{"message"},
		}
		if offset, ok := offsets[ch.ID]; ok && offset > 0 {
			req.Offset = &offset
		}

		var updates []telegramUpdate
		pollCtx, pollCancel := context.WithTimeout(ctx, telegramRemoteRequestTimeout)
		err = client.GetUpdates(pollCtx, strings.TrimSpace(*token), req, &updates)
		pollCancel()
		if err != nil {
			continue
		}

		nextOffset := offsets[ch.ID]
		for _, update := range updates {
			if err := connector.HandleUpdate(ctx, observability.NewTraceID(), ch, strings.TrimSpace(*token), update); err != nil {
				break
			}
			if candidate := update.UpdateID + 1; candidate > nextOffset {
				nextOffset = candidate
			}
		}
		if nextOffset > 0 {
			offsets[ch.ID] = nextOffset
		}
	}
	return nil
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
