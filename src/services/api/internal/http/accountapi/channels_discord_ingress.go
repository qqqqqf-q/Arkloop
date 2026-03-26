package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/discordbot"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DiscordIngressRunnerDeps struct {
	ChannelsRepo          *data.ChannelsRepository
	ChannelIdentitiesRepo *data.ChannelIdentitiesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	ChannelBindCodesRepo  *data.ChannelBindCodesRepository
	ChannelDMThreadsRepo  *data.ChannelDMThreadsRepository
	ChannelReceiptsRepo   *data.ChannelMessageReceiptsRepository
	ChannelLedgerRepo     *data.ChannelMessageLedgerRepository
	SecretsRepo           *data.SecretsRepository
	PersonasRepo          *data.PersonasRepository
	ThreadRepo            *data.ThreadRepository
	MessageRepo           *data.MessageRepository
	RunEventRepo          *data.RunEventRepository
	JobRepo               *data.JobRepository
	CreditsRepo           *data.CreditsRepository
	Pool                  data.DB
	EntitlementService    *entitlement.Service
	DiscordClient         *discordbot.Client
	ScanInterval          time.Duration
	Bus                   eventbus.EventBus
}

type discordSessionState struct {
	token  string
	cancel context.CancelFunc
	done   chan struct{}
}

type discordIngressManager struct {
	deps     DiscordIngressRunnerDeps
	mu       sync.Mutex
	sessions map[uuid.UUID]discordSessionState
}

type discordConnector struct {
	channelsRepo          *data.ChannelsRepository
	channelIdentitiesRepo *data.ChannelIdentitiesRepository
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	channelBindCodesRepo  *data.ChannelBindCodesRepository
	channelDMThreadsRepo  *data.ChannelDMThreadsRepository
	channelReceiptsRepo   *data.ChannelMessageReceiptsRepository
	channelLedgerRepo     *data.ChannelMessageLedgerRepository
	personasRepo          *data.PersonasRepository
	threadRepo            *data.ThreadRepository
	messageRepo           *data.MessageRepository
	runEventRepo          *data.RunEventRepository
	jobRepo               *data.JobRepository
	creditsRepo           *data.CreditsRepository
	pool                  data.DB
	discordClient         *discordbot.Client
	inputNotify           func(ctx context.Context, runID uuid.UUID)
}

type discordInteractionReply struct {
	Content   string
	Ephemeral bool
}

type discordMessageContext struct {
	ChannelID  string
	MessageID  string
	AuthorID   string
	AuthorName string
	Content    string
	ReplyToID  *string
	Timestamp  time.Time
}

func StartDiscordIngressRunner(ctx context.Context, deps DiscordIngressRunnerDeps) {
	if ctx == nil || deps.ChannelsRepo == nil || deps.ChannelIdentitiesRepo == nil || deps.ChannelIdentityLinksRepo == nil ||
		deps.ChannelBindCodesRepo == nil || deps.ChannelDMThreadsRepo == nil ||
		deps.ChannelReceiptsRepo == nil || deps.SecretsRepo == nil || deps.PersonasRepo == nil ||
		deps.ThreadRepo == nil || deps.MessageRepo == nil || deps.RunEventRepo == nil ||
		deps.JobRepo == nil || deps.CreditsRepo == nil || deps.Pool == nil {
		slog.Warn("discord_ingress_runner_skip", "reason", "deps")
		return
	}
	if deps.ScanInterval <= 0 {
		deps.ScanInterval = 15 * time.Second
	}
	if deps.DiscordClient == nil {
		deps.DiscordClient = discordbot.NewClient("", nil)
	}

	manager := &discordIngressManager{
		deps:     deps,
		sessions: make(map[uuid.UUID]discordSessionState),
	}
	go manager.run(ctx)
}

func (m *discordIngressManager) run(ctx context.Context) {
	ticker := time.NewTicker(m.deps.ScanInterval)
	defer ticker.Stop()
	for {
		if err := m.sync(ctx); err != nil {
			slog.Warn("discord_ingress_sync_failed", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-ticker.C:
		}
	}
}

func (m *discordIngressManager) sync(ctx context.Context) error {
	items, err := m.deps.ChannelsRepo.ListActiveByType(ctx, "discord")
	if err != nil {
		return err
	}
	active := make(map[uuid.UUID]struct{}, len(items))
	for _, ch := range items {
		active[ch.ID] = struct{}{}
		token, tokenErr := m.loadToken(ctx, ch)
		if tokenErr != nil {
			slog.Warn("discord_ingress_token_failed", "channel_id", ch.ID.String(), "err", tokenErr.Error())
			continue
		}
		m.ensureSession(ctx, ch, token)
	}
	m.stopInactive(active)
	return nil
}

func (m *discordIngressManager) loadToken(ctx context.Context, ch data.Channel) (string, error) {
	if ch.CredentialsID == nil || *ch.CredentialsID == uuid.Nil {
		return "", fmt.Errorf("missing credentials")
	}
	token, err := m.deps.SecretsRepo.DecryptByID(ctx, *ch.CredentialsID)
	if err != nil {
		return "", err
	}
	if token == nil || strings.TrimSpace(*token) == "" {
		return "", fmt.Errorf("empty credentials")
	}
	return strings.TrimSpace(*token), nil
}

func (m *discordIngressManager) ensureSession(parent context.Context, ch data.Channel, token string) {
	m.mu.Lock()
	state, ok := m.sessions[ch.ID]
	if ok && state.token == token {
		m.mu.Unlock()
		return
	}
	if ok {
		state.cancel()
		delete(m.sessions, ch.ID)
	}
	childCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.sessions[ch.ID] = discordSessionState{token: token, cancel: cancel, done: done}
	m.mu.Unlock()

	go func() {
		defer close(done)
		if err := m.runSession(childCtx, ch.ID, token); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("discord_ingress_session_failed", "channel_id", ch.ID.String(), "err", err.Error())
		}
		m.mu.Lock()
		current, exists := m.sessions[ch.ID]
		if exists && current.done == done {
			delete(m.sessions, ch.ID)
		}
		m.mu.Unlock()
	}()
}

func (m *discordIngressManager) stopInactive(active map[uuid.UUID]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for channelID, state := range m.sessions {
		if _, ok := active[channelID]; ok {
			continue
		}
		state.cancel()
		delete(m.sessions, channelID)
	}
}

func (m *discordIngressManager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for channelID, state := range m.sessions {
		state.cancel()
		delete(m.sessions, channelID)
	}
}

func (m *discordIngressManager) runSession(ctx context.Context, channelID uuid.UUID, token string) error {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return err
	}
	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	connector := discordConnector{
		channelsRepo:          m.deps.ChannelsRepo,
		channelIdentitiesRepo: m.deps.ChannelIdentitiesRepo,
		channelIdentityLinksRepo: m.deps.ChannelIdentityLinksRepo,
		channelBindCodesRepo:  m.deps.ChannelBindCodesRepo,
		channelDMThreadsRepo:  m.deps.ChannelDMThreadsRepo,
		channelReceiptsRepo:   m.deps.ChannelReceiptsRepo,
		channelLedgerRepo:     m.deps.ChannelLedgerRepo,
		personasRepo:          m.deps.PersonasRepo,
		threadRepo:            m.deps.ThreadRepo,
		messageRepo:           m.deps.MessageRepo,
		runEventRepo:          m.deps.RunEventRepo,
		jobRepo:               m.deps.JobRepo,
		creditsRepo:           m.deps.CreditsRepo,
		pool:                  m.deps.Pool,
		discordClient:         m.deps.DiscordClient,
		inputNotify:           buildDiscordInputNotifier(m.deps.Pool, m.deps.Bus),
	}

	session.AddHandler(func(s *discordgo.Session, evt *discordgo.MessageCreate) {
		if evt == nil || evt.Author == nil || evt.Author.Bot {
			return
		}
		go func() {
			if err := connector.HandleMessageCreate(context.Background(), observability.NewTraceID(), channelID, token, evt); err != nil {
				slog.Warn("discord_ingress_message_failed", "channel_id", channelID.String(), "err", err.Error())
			}
		}()
	})
	session.AddHandler(func(s *discordgo.Session, evt *discordgo.InteractionCreate) {
		if evt == nil || evt.Type != discordgo.InteractionApplicationCommand {
			return
		}
		go func() {
			reply, err := connector.HandleInteraction(context.Background(), observability.NewTraceID(), channelID, token, evt)
			if err != nil {
				slog.Warn("discord_ingress_interaction_failed", "channel_id", channelID.String(), "err", err.Error())
				reply = &discordInteractionReply{Content: "处理失败。", Ephemeral: true}
			}
			if reply == nil {
				return
			}
			resp := &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: reply.Content,
				},
			}
			if reply.Ephemeral {
				resp.Data.Flags = discordgo.MessageFlagsEphemeral
			}
			if err := s.InteractionRespond(evt.Interaction, resp); err != nil {
				slog.Warn("discord_ingress_interaction_respond_failed", "channel_id", channelID.String(), "err", err.Error())
			}
		}()
	})

	if err := m.ensureCommands(ctx, channelID, token); err != nil {
		slog.Warn("discord_command_sync_failed", "channel_id", channelID.String(), "err", err.Error())
	}
	if err := session.Open(); err != nil {
		return err
	}
	defer session.Close()

	<-ctx.Done()
	return ctx.Err()
}

func (m *discordIngressManager) ensureCommands(ctx context.Context, channelID uuid.UUID, token string) error {
	ch, err := m.deps.ChannelsRepo.GetByID(ctx, channelID)
	if err != nil || ch == nil {
		return err
	}
	cfg, err := resolveDiscordConfig(ch.ChannelType, ch.ConfigJSON)
	if err != nil {
		return err
	}
	info, err := m.deps.DiscordClient.VerifyBot(ctx, token)
	if err != nil {
		return err
	}
	if merged, changed, mergeErr := mergeDiscordBotProfile(ch.ConfigJSON, info); mergeErr == nil && changed {
		if _, updateErr := m.deps.ChannelsRepo.Update(ctx, ch.ID, ch.AccountID, data.ChannelUpdate{ConfigJSON: &merged}); updateErr != nil {
			slog.Warn("discord_command_sync_config_failed", "channel_id", ch.ID.String(), "err", updateErr.Error())
		}
	}
	commands := discordCommands()
	if len(cfg.AllowedServerIDs) == 0 {
		if err := m.deps.DiscordClient.RegisterGlobalCommands(ctx, token, info.ApplicationID, commands); err != nil {
			return err
		}
		return nil
	}
	for _, guildID := range cfg.AllowedServerIDs {
		if err := m.deps.DiscordClient.RegisterGuildCommands(ctx, token, info.ApplicationID, guildID, commands); err != nil {
			slog.Warn("discord_guild_command_sync_failed", "channel_id", ch.ID.String(), "guild_id", guildID, "err", err.Error())
		}
	}
	return nil
}

func buildDiscordInputNotifier(pool data.DB, bus eventbus.EventBus) func(ctx context.Context, runID uuid.UUID) {
	if bus != nil {
		return func(ctx context.Context, runID uuid.UUID) {
			_ = bus.Publish(ctx, fmt.Sprintf("run_events:%s", runID.String()), "")
		}
	}
	if pool != nil {
		return func(ctx context.Context, runID uuid.UUID) {
			if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String()); err != nil {
				slog.Warn("discord_active_run_notify_failed", "run_id", runID.String(), "error", err)
			}
		}
	}
	return nil
}

func (c discordConnector) HandleMessageCreate(
	ctx context.Context,
	traceID string,
	channelID uuid.UUID,
	token string,
	event *discordgo.MessageCreate,
) error {
	if event == nil || event.Author == nil || event.Author.Bot {
		return nil
	}
	ch, err := c.channelsRepo.GetByID(ctx, channelID)
	if err != nil || ch == nil || !ch.IsActive || ch.ChannelType != "discord" {
		return err
	}
	if strings.TrimSpace(event.GuildID) != "" {
		return nil
	}
	persona, personaRef, err := mustValidateDiscordActivation(ctx, ch.AccountID, c.personasRepo, ch.PersonaID)
	if err != nil {
		return err
	}
	content := strings.TrimSpace(event.Content)
	if content == "" {
		return nil
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	accepted, err := c.channelReceiptsRepo.WithTx(tx).Record(ctx, ch.ID, event.ChannelID, event.ID)
	if err != nil {
		return err
	}
	if !accepted {
		return tx.Commit(ctx)
	}

	identity, err := upsertDiscordIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), event.Author)
	if err != nil {
		return err
	}
	threadID, err := c.resolveDiscordDMThreadID(ctx, tx, *ch, persona.ID, derefUUID(persona.ProjectID), identity)
	if err != nil {
		return err
	}

	if c.channelLedgerRepo != nil {
		ledgerMetadata, metaErr := json.Marshal(map[string]any{
			"source":            "discord",
			"conversation_type": "private",
		})
		if metaErr != nil {
			return metaErr
		}
		if _, ledgerErr := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               ch.ID,
			ChannelType:             ch.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			ThreadID:                &threadID,
			PlatformConversationID:  event.ChannelID,
			PlatformMessageID:       event.ID,
			PlatformParentMessageID: optionalDiscordReplyMessageID(event),
			SenderChannelIdentityID: &identity.ID,
			MetadataJSON:            ledgerMetadata,
		}); ledgerErr != nil {
			return ledgerErr
		}
	}

	messageTS := event.Timestamp
	rendered := renderDiscordInboundMessage(identity, content, messageTS)
	contentJSON := json.RawMessage(`{}`)
	metadataJSON := json.RawMessage(`{"source":"discord"}`)
	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(ctx, ch.AccountID, threadID, "user", rendered, contentJSON, metadataJSON, identity.UserID); err != nil {
		return err
	}

	runRepoTx := c.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, threadID); err != nil {
		return err
	}
	if activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, threadID); err != nil {
		return err
	} else if activeRun != nil {
		delivered, deliverErr := c.deliverDiscordMessageToActiveRun(ctx, runRepoTx, activeRun, rendered, traceID)
		if deliverErr != nil {
			return deliverErr
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

	runStartedData := buildDiscordRunStartedData(personaRef, resolveDiscordDefaultModel(ch.ConfigJSON))
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
		"source":           "discord",
		"channel_delivery": buildDiscordChannelDeliveryPayload(ch.ID, identity.ID, event),
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(ctx, ch.AccountID, run.ID, traceID, data.RunExecuteJobType, jobPayload, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c discordConnector) HandleInteraction(
	ctx context.Context,
	traceID string,
	channelID uuid.UUID,
	token string,
	event *discordgo.InteractionCreate,
) (*discordInteractionReply, error) {
	if event == nil || event.Type != discordgo.InteractionApplicationCommand {
		return nil, nil
	}
	ch, err := c.channelsRepo.GetByID(ctx, channelID)
	if err != nil || ch == nil || !ch.IsActive || ch.ChannelType != "discord" {
		return nil, err
	}
	cfg, err := resolveDiscordConfig(ch.ChannelType, ch.ConfigJSON)
	if err != nil {
		return nil, err
	}
	if !discordCommandAllowed(cfg, event.GuildID, event.ChannelID) {
		return &discordInteractionReply{Content: "当前服务器或频道未被授权。", Ephemeral: true}, nil
	}
	user := interactionAuthor(event)
	if user == nil {
		return nil, nil
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	identity, err := upsertDiscordIdentity(ctx, c.channelIdentitiesRepo.WithTx(tx), user)
	if err != nil {
		return nil, err
	}

	reply, err := handleDiscordCommand(ctx, tx, ch, identity, event, c.channelBindCodesRepo, c.channelIdentitiesRepo, c.channelIdentityLinksRepo, c.channelDMThreadsRepo, c.threadRepo)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return reply, nil
}

func (c discordConnector) resolveDiscordDMThreadID(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	personaID uuid.UUID,
	projectID uuid.UUID,
	identity data.ChannelIdentity,
) (uuid.UUID, error) {
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

func (c discordConnector) deliverDiscordMessageToActiveRun(
	ctx context.Context,
	repo *data.RunEventRepository,
	run *data.Run,
	content string,
	traceID string,
) (bool, error) {
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

func (c discordConnector) notifyActiveRunInput(ctx context.Context, runID uuid.UUID) {
	if c.inputNotify == nil || runID == uuid.Nil {
		return
	}
	c.inputNotify(ctx, runID)
}

func buildDiscordRunStartedData(personaRef string, defaultModel string) map[string]any {
	dataJSON := map[string]any{"persona_id": personaRef}
	if model := strings.TrimSpace(defaultModel); model != "" {
		dataJSON["model"] = model
	}
	return dataJSON
}

func resolveDiscordDefaultModel(raw json.RawMessage) string {
	cfg, err := resolveDiscordConfig("discord", raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.DefaultModel)
}

func buildDiscordChannelDeliveryPayload(channelID uuid.UUID, channelIdentityID uuid.UUID, event *discordgo.MessageCreate) map[string]any {
	payload := map[string]any{
		"channel_id":   channelID.String(),
		"channel_type": "discord",
		"conversation_ref": map[string]any{
			"target": event.ChannelID,
		},
		"inbound_message_ref": map[string]any{
			"message_id": event.ID,
		},
		"trigger_message_ref": map[string]any{
			"message_id": event.ID,
		},
		"platform_chat_id":           event.ChannelID,
		"platform_message_id":        event.ID,
		"reply_to_message_id":        event.ID,
		"sender_channel_identity_id": channelIdentityID.String(),
		"conversation_type":          "private",
	}
	if replyTo := optionalDiscordReplyMessageID(event); replyTo != nil {
		payload["inbound_reply_to_message_id"] = *replyTo
	}
	return payload
}

func optionalDiscordReplyMessageID(event *discordgo.MessageCreate) *string {
	if event == nil || event.MessageReference == nil {
		return nil
	}
	messageID := strings.TrimSpace(event.MessageReference.MessageID)
	if messageID == "" {
		return nil
	}
	return &messageID
}

func upsertDiscordIdentity(ctx context.Context, repo *data.ChannelIdentitiesRepository, user *discordgo.User) (data.ChannelIdentity, error) {
	if user == nil {
		return data.ChannelIdentity{}, fmt.Errorf("discord user required")
	}
	displayName := strings.TrimSpace(user.GlobalName)
	if displayName == "" {
		displayName = strings.TrimSpace(user.Username)
	}
	var displayNamePtr *string
	if displayName != "" {
		displayNamePtr = &displayName
	}
	var avatarURL *string
	if user.Avatar != "" {
		url := fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", user.ID, user.Avatar)
		avatarURL = &url
	}
	metadata, err := json.Marshal(map[string]any{
		"username":    strings.TrimSpace(user.Username),
		"global_name": strings.TrimSpace(user.GlobalName),
		"is_bot":      user.Bot,
	})
	if err != nil {
		return data.ChannelIdentity{}, err
	}
	return repo.Upsert(ctx, "discord", strings.TrimSpace(user.ID), displayNamePtr, avatarURL, metadata)
}

func interactionAuthor(evt *discordgo.InteractionCreate) *discordgo.User {
	if evt == nil || evt.Interaction == nil {
		return nil
	}
	if evt.Member != nil && evt.Member.User != nil {
		return evt.Member.User
	}
	return evt.User
}

func handleDiscordCommand(
	ctx context.Context,
	tx pgx.Tx,
	channel *data.Channel,
	identity data.ChannelIdentity,
	evt *discordgo.InteractionCreate,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	threadRepo *data.ThreadRepository,
) (*discordInteractionReply, error) {
	data := evt.ApplicationCommandData()
	switch strings.TrimSpace(data.Name) {
	case "help":
		return &discordInteractionReply{Content: "可用命令：/help /bind /new。私信可以直接聊天。", Ephemeral: true}, nil
	case "bind":
		code := ""
		if len(data.Options) > 0 {
			code = strings.TrimSpace(data.Options[0].StringValue())
		}
		replyText, err := bindDiscordIdentity(ctx, tx, channel, identity, code, channelBindCodesRepo, channelIdentitiesRepo, channelIdentityLinksRepo, channelDMThreadsRepo, threadRepo)
		if err != nil {
			return nil, err
		}
		return &discordInteractionReply{Content: replyText, Ephemeral: true}, nil
	case "new":
		if evt.GuildID != "" {
			return &discordInteractionReply{Content: "请在私信中使用 /new。", Ephemeral: true}, nil
		}
		if channel == nil || channel.PersonaID == nil || *channel.PersonaID == uuid.Nil {
			return &discordInteractionReply{Content: "当前会话未配置 persona。", Ephemeral: true}, nil
		}
		if err := channelDMThreadsRepo.WithTx(tx).DeleteByBinding(ctx, channel.ID, identity.ID, *channel.PersonaID); err != nil {
			return nil, err
		}
		return &discordInteractionReply{Content: "已开启新会话。", Ephemeral: true}, nil
	default:
		return &discordInteractionReply{Content: "暂不支持这个命令。", Ephemeral: true}, nil
	}
}

func bindDiscordIdentity(
	ctx context.Context,
	tx pgx.Tx,
	channel *data.Channel,
	identity data.ChannelIdentity,
	code string,
	channelBindCodesRepo *data.ChannelBindCodesRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
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
		return "当前 Discord 身份已绑定到其他账号。", nil
	}
	if identity.UserID != nil {
		if _, err := channelBindCodesRepo.WithTx(tx).ConsumeForChannel(ctx, code, identity.ID, channel.ChannelType); err != nil {
			return "", err
		}
		if channelIdentityLinksRepo != nil {
			if _, err := channelIdentityLinksRepo.WithTx(tx).Upsert(ctx, channel.ID, identity.ID); err != nil {
				return "", err
			}
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
	if channelIdentityLinksRepo != nil {
		if _, err := channelIdentityLinksRepo.WithTx(tx).Upsert(ctx, channel.ID, identity.ID); err != nil {
			return "", err
		}
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

func renderDiscordInboundMessage(identity data.ChannelIdentity, text string, ts time.Time) string {
	displayName := identity.PlatformSubjectID
	if identity.DisplayName != nil && strings.TrimSpace(*identity.DisplayName) != "" {
		displayName = strings.TrimSpace(*identity.DisplayName)
	}
	return fmt.Sprintf(`---
channel-identity-id: "%s"
display-name: "%s"
channel: "discord"
conversation-type: "private"
time: "%s"
---
%s`,
		identity.ID.String(),
		displayName,
		ts.UTC().Format(time.RFC3339),
		strings.TrimSpace(text),
	)
}
