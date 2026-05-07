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
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/qqbotclient"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type qqbotChannelConfig struct {
	AppID           string   `json:"app_id"`
	AllowedUserIDs  []string `json:"allowed_user_ids,omitempty"`
	AllowedGroupIDs []string `json:"allowed_group_ids,omitempty"`
	DefaultModel    string   `json:"default_model,omitempty"`
}

func normalizeQQBotChannelConfig(raw json.RawMessage) (json.RawMessage, qqbotChannelConfig, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var cfg qqbotChannelConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, qqbotChannelConfig{}, fmt.Errorf("config_json must be a valid JSON object")
	}
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.DefaultModel = strings.TrimSpace(cfg.DefaultModel)
	cfg.AllowedUserIDs = normalizeQQBotIDList(cfg.AllowedUserIDs)
	cfg.AllowedGroupIDs = normalizeQQBotIDList(cfg.AllowedGroupIDs)
	if cfg.AppID == "" {
		return nil, qqbotChannelConfig{}, fmt.Errorf("qqbot config_json.app_id must not be empty")
	}
	normalized, err := json.Marshal(cfg)
	return normalized, cfg, err
}

func resolveQQBotChannelConfig(raw json.RawMessage) (qqbotChannelConfig, error) {
	_, cfg, err := normalizeQQBotChannelConfig(raw)
	return cfg, err
}

func mustValidateQQBotActivation(
	ctx context.Context,
	accountID uuid.UUID,
	personasRepo *data.PersonasRepository,
	personaID *uuid.UUID,
	configJSON json.RawMessage,
) (*data.Persona, string, qqbotChannelConfig, error) {
	if personaID == nil || *personaID == uuid.Nil {
		return nil, "", qqbotChannelConfig{}, fmt.Errorf("qqbot channel requires persona_id before activation")
	}
	persona, err := personasRepo.GetByIDForAccount(ctx, accountID, *personaID)
	if err != nil {
		return nil, "", qqbotChannelConfig{}, err
	}
	if persona == nil || !persona.IsActive {
		return nil, "", qqbotChannelConfig{}, fmt.Errorf("persona not found or inactive")
	}
	if persona.ProjectID == nil || *persona.ProjectID == uuid.Nil {
		return nil, "", qqbotChannelConfig{}, fmt.Errorf("qqbot channel persona must belong to a project")
	}
	cfg, err := resolveQQBotChannelConfig(configJSON)
	if err != nil {
		return nil, "", qqbotChannelConfig{}, err
	}
	return persona, buildPersonaRef(*persona), cfg, nil
}

func normalizeQQBotIDList(values []string) []string {
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
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			out = append(out, cleaned)
		}
	}
	return out
}

func qqbotUserAllowed(cfg qqbotChannelConfig, userID, groupID string) bool {
	if len(cfg.AllowedUserIDs) == 0 && len(cfg.AllowedGroupIDs) == 0 {
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

type QQBotIngressRunnerDeps struct {
	ChannelsRepo             *data.ChannelsRepository
	ChannelIdentitiesRepo    *data.ChannelIdentitiesRepository
	ChannelBindCodesRepo     *data.ChannelBindCodesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	ChannelDMThreadsRepo     *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo  *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo      *data.ChannelMessageReceiptsRepository
	ChannelLedgerRepo        *data.ChannelMessageLedgerRepository
	SecretsRepo              *data.SecretsRepository
	PersonasRepo             *data.PersonasRepository
	ThreadRepo               *data.ThreadRepository
	MessageRepo              *data.MessageRepository
	RunEventRepo             *data.RunEventRepository
	JobRepo                  *data.JobRepository
	Pool                     data.DB
	Bus                      eventbus.EventBus
	ScanInterval             time.Duration
}

type qqbotSessionState struct {
	key    string
	cancel context.CancelFunc
	done   chan struct{}
}

type qqbotIngressManager struct {
	deps     QQBotIngressRunnerDeps
	mu       sync.Mutex
	sessions map[uuid.UUID]qqbotSessionState
}

type qqbotConnector struct {
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
	client                   *qqbotclient.Client
	inputNotify              func(ctx context.Context, runID uuid.UUID)
}

func StartQQBotIngressRunner(ctx context.Context, deps QQBotIngressRunnerDeps) {
	if ctx == nil || deps.ChannelsRepo == nil || deps.ChannelIdentitiesRepo == nil ||
		deps.ChannelBindCodesRepo == nil || deps.ChannelIdentityLinksRepo == nil ||
		deps.ChannelDMThreadsRepo == nil || deps.ChannelGroupThreadsRepo == nil ||
		deps.ChannelReceiptsRepo == nil || deps.SecretsRepo == nil || deps.PersonasRepo == nil ||
		deps.ThreadRepo == nil || deps.MessageRepo == nil || deps.RunEventRepo == nil ||
		deps.JobRepo == nil || deps.Pool == nil {
		slog.Warn("qqbot_ingress_runner_skip", "reason", "deps")
		return
	}
	if deps.ChannelLedgerRepo == nil {
		repo, err := data.NewChannelMessageLedgerRepository(deps.Pool)
		if err != nil {
			slog.Warn("qqbot_ingress_runner_skip", "reason", "ledger_repo", "err", err)
			return
		}
		deps.ChannelLedgerRepo = repo
	}
	if deps.ScanInterval <= 0 {
		deps.ScanInterval = 5 * time.Second
	}
	manager := &qqbotIngressManager{
		deps:     deps,
		sessions: make(map[uuid.UUID]qqbotSessionState),
	}
	go manager.run(ctx)
}

func (m *qqbotIngressManager) run(ctx context.Context) {
	ticker := time.NewTicker(m.deps.ScanInterval)
	defer ticker.Stop()
	for {
		if err := m.sync(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("qqbot_ingress_sync_failed", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-ticker.C:
		}
	}
}

func (m *qqbotIngressManager) sync(ctx context.Context) error {
	channels, err := m.deps.ChannelsRepo.ListActiveByType(ctx, "qqbot")
	if err != nil {
		return err
	}
	active := make(map[uuid.UUID]struct{}, len(channels))
	for _, ch := range channels {
		creds, err := m.loadCredentials(ctx, ch)
		if err != nil {
			slog.Warn("qqbot_ingress_credentials_failed", "channel_id", ch.ID.String(), "err", err.Error())
			continue
		}
		active[ch.ID] = struct{}{}
		m.ensureSession(ctx, ch, creds)
	}
	m.stopInactive(active)
	return nil
}

func (m *qqbotIngressManager) loadCredentials(ctx context.Context, ch data.Channel) (qqbotclient.Credentials, error) {
	if ch.CredentialsID == nil || *ch.CredentialsID == uuid.Nil {
		return qqbotclient.Credentials{}, fmt.Errorf("missing credentials")
	}
	token, err := m.deps.SecretsRepo.DecryptByID(ctx, *ch.CredentialsID)
	if err != nil {
		return qqbotclient.Credentials{}, err
	}
	if token == nil {
		return qqbotclient.Credentials{}, fmt.Errorf("empty credentials")
	}
	return qqbotclient.ParseCredentials(ch.ConfigJSON, *token)
}

func (m *qqbotIngressManager) ensureSession(parent context.Context, ch data.Channel, creds qqbotclient.Credentials) {
	key := creds.AppID + "\x00" + creds.ClientSecret + "\x00" + string(ch.ConfigJSON) + "\x00" + derefUUID(ch.PersonaID).String()
	m.mu.Lock()
	state, exists := m.sessions[ch.ID]
	if exists && state.key == key {
		m.mu.Unlock()
		return
	}
	if exists {
		state.cancel()
		delete(m.sessions, ch.ID)
	}
	childCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.sessions[ch.ID] = qqbotSessionState{key: key, cancel: cancel, done: done}
	m.mu.Unlock()

	go func() {
		defer close(done)
		client := qqbotclient.NewClient(creds, nil)
		connector := qqbotConnector{
			channelIdentitiesRepo:    m.deps.ChannelIdentitiesRepo,
			channelBindCodesRepo:     m.deps.ChannelBindCodesRepo,
			channelIdentityLinksRepo: m.deps.ChannelIdentityLinksRepo,
			channelDMThreadsRepo:     m.deps.ChannelDMThreadsRepo,
			channelGroupThreadsRepo:  m.deps.ChannelGroupThreadsRepo,
			channelReceiptsRepo:      m.deps.ChannelReceiptsRepo,
			channelLedgerRepo:        m.deps.ChannelLedgerRepo,
			personasRepo:             m.deps.PersonasRepo,
			threadRepo:               m.deps.ThreadRepo,
			messageRepo:              m.deps.MessageRepo,
			runEventRepo:             m.deps.RunEventRepo,
			jobRepo:                  m.deps.JobRepo,
			pool:                     m.deps.Pool,
			client:                   client,
			inputNotify:              buildQQBotInputNotifier(m.deps.Pool, m.deps.Bus),
		}
		listener := qqbotclient.NewGatewayListener(client, func(evCtx context.Context, event qqbotclient.GatewayEvent) {
			if err := connector.HandleGatewayEvent(evCtx, observability.NewTraceID(), ch, event); err != nil {
				slog.Warn("qqbot_ingress_event_failed", "channel_id", ch.ID.String(), "event_type", event.Type, "err", err.Error())
			}
		}, nil)
		if err := listener.Run(childCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("qqbot_ingress_session_failed", "channel_id", ch.ID.String(), "err", err.Error())
		}
		m.mu.Lock()
		current, ok := m.sessions[ch.ID]
		if ok && current.done == done {
			delete(m.sessions, ch.ID)
		}
		m.mu.Unlock()
	}()
}

func (m *qqbotIngressManager) stopInactive(active map[uuid.UUID]struct{}) {
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

func (m *qqbotIngressManager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for channelID, state := range m.sessions {
		state.cancel()
		delete(m.sessions, channelID)
	}
}

func buildQQBotInputNotifier(pool data.DB, bus eventbus.EventBus) func(ctx context.Context, runID uuid.UUID) {
	if bus != nil {
		return func(ctx context.Context, runID uuid.UUID) {
			if runID != uuid.Nil {
				_ = bus.Publish(ctx, fmt.Sprintf("run_events:%s", runID.String()), "")
			}
		}
	}
	if pool != nil {
		return func(ctx context.Context, runID uuid.UUID) {
			if runID != uuid.Nil {
				_, _ = pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String())
			}
		}
	}
	return nil
}

func (c qqbotConnector) HandleGatewayEvent(ctx context.Context, traceID string, ch data.Channel, event qqbotclient.GatewayEvent) error {
	switch event.Type {
	case qqbotclient.EventReady:
		return nil
	case qqbotclient.EventC2CMessageCreate, qqbotclient.EventGroupAtMessageCreate:
		var msg qqbotclient.MessageCreateEvent
		if err := json.Unmarshal(event.Data, &msg); err != nil {
			return err
		}
		return c.HandleMessage(ctx, traceID, ch, event.Type, msg)
	default:
		return nil
	}
}

func (c qqbotConnector) HandleMessage(ctx context.Context, traceID string, ch data.Channel, eventType string, msg qqbotclient.MessageCreateEvent) error {
	cfg, err := resolveQQBotChannelConfig(ch.ConfigJSON)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(msg.Content)
	messageID := strings.TrimSpace(msg.ID)
	senderID := msg.SenderOpenID()
	conversationType := "private"
	platformChatID := senderID
	groupID := ""
	if eventType == qqbotclient.EventGroupAtMessageCreate {
		conversationType = "group"
		text = msg.ContentWithoutSelfMentions()
		groupID = msg.GroupTarget()
		platformChatID = groupID
	}
	if text == "" || messageID == "" || senderID == "" {
		return nil
	}
	if platformChatID == "" || !qqbotUserAllowed(cfg, senderID, groupID) {
		return nil
	}
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	accepted, err := c.channelReceiptsRepo.WithTx(tx).Record(ctx, ch.ID, platformChatID, messageID)
	if err != nil {
		return err
	}
	if !accepted {
		return tx.Commit(ctx)
	}

	displayName := msg.SenderDisplayName()
	identity, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, "qqbot", senderID, &displayName, nil, nil)
	if err != nil {
		return err
	}
	if groupID != "" {
		if _, err := c.channelIdentitiesRepo.WithTx(tx).Upsert(ctx, "qqbot", groupID, nil, nil, nil); err != nil {
			return err
		}
	}

	handled, replyText, err := c.handleCommand(ctx, tx, &ch, identity, text)
	if err != nil {
		return err
	}
	if handled {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		replyScope := qqbotclient.ScopeC2C
		if conversationType == "group" {
			replyScope = qqbotclient.ScopeGroup
		}
		c.sendTextReply(ctx, replyScope, platformChatID, replyText, messageID)
		return nil
	}

	persona, personaRef, err := c.resolvePersona(ctx, ch)
	if err != nil {
		return err
	}

	threadProjectID := derefUUID(persona.ProjectID)
	if threadProjectID == uuid.Nil && ch.OwnerUserID != nil {
		if pid, err := c.personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, *ch.OwnerUserID); err == nil {
			threadProjectID = pid
		}
	}
	if threadProjectID == uuid.Nil {
		return fmt.Errorf("cannot resolve project for qqbot persona %s", persona.ID)
	}
	threadID, err := c.resolveThreadID(ctx, tx, ch, persona.ID, threadProjectID, identity, conversationType == "private", platformChatID, displayName)
	if err != nil {
		return err
	}

	projection := buildQQBotEnvelopeText(identity.ID, displayName, conversationType, text, msg.Timestamp, senderID, messageID)
	contentJSON, err := messagecontent.FromText(projection).JSON()
	if err != nil {
		return err
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"source":              "qqbot",
		"channel_identity_id": identity.ID.String(),
		"display_name":        displayName,
		"platform_chat_id":    platformChatID,
		"platform_message_id": messageID,
		"platform_user_id":    senderID,
		"conversation_type":   conversationType,
		"event_type":          eventType,
	})
	if _, err := c.messageRepo.WithTx(tx).CreateStructuredWithMetadata(ctx, ch.AccountID, threadID, "user", projection, contentJSON, metadataJSON, identity.UserID); err != nil {
		return err
	}
	if _, err := c.channelLedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:               ch.ID,
		ChannelType:             "qqbot",
		Direction:               data.ChannelMessageDirectionInbound,
		ThreadID:                &threadID,
		PlatformConversationID:  platformChatID,
		PlatformMessageID:       messageID,
		SenderChannelIdentityID: &identity.ID,
		MetadataJSON:            metadataJSON,
	}); err != nil {
		return err
	}

	runRepoTx := c.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, threadID); err != nil {
		return err
	}
	if activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, threadID); err != nil {
		return err
	} else if activeRun != nil {
		if _, err := runRepoTx.ProvideInputWithKey(ctx, activeRun.ID, projection, traceID, "qqbot:"+platformChatID+":"+messageID); err != nil {
			var notActive data.RunNotActiveError
			if !errors.As(err, &notActive) {
				return err
			}
		} else {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			c.notifyInput(ctx, activeRun.ID)
			return nil
		}
	}

	if !channelAgentTriggerConsume(ch.ID) {
		return tx.Commit(ctx)
	}
	deliveryPayload := buildQQBotChannelDeliveryPayload(ch.ID, identity.ID, platformChatID, messageID, conversationType)
	runData := buildChannelRunStartedData(personaRef, cfg.DefaultModel, "", deliveryPayload)
	run, _, err := runRepoTx.CreateRunWithStartedEvent(ctx, ch.AccountID, threadID, channelOwnerUserID(ch), "run.started", runData)
	if err != nil {
		return err
	}
	jobPayload := map[string]any{
		"source":           "qqbot",
		"channel_delivery": deliveryPayload,
	}
	if _, err := c.jobRepo.WithTx(tx).EnqueueRun(ctx, ch.AccountID, run.ID, traceID, data.RunExecuteJobType, jobPayload, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c qqbotConnector) handleCommand(ctx context.Context, tx pgx.Tx, ch *data.Channel, identity data.ChannelIdentity, text string) (bool, string, error) {
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false, "", nil
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false, "", nil
	}
	switch strings.TrimSpace(parts[0]) {
	case "/help":
		return true, "/bind <code> — 绑定你的账号\n/help — 显示帮助", nil
	case "/start":
		return true, "已连接 Arkloop\n\n使用 /bind <code> 绑定账号。", nil
	case "/bind":
		if len(parts) < 2 {
			return true, "用法：/bind <code>", nil
		}
		replyText, err := bindChannelIdentity(ctx, tx, ch, identity, parts[1], "QQ", c.channelBindCodesRepo, c.channelIdentitiesRepo, c.channelIdentityLinksRepo, c.channelDMThreadsRepo, c.threadRepo)
		return true, replyText, err
	default:
		return false, "", nil
	}
}

func (c qqbotConnector) sendTextReply(ctx context.Context, scope, target, content, msgID string) {
	if c.client == nil || strings.TrimSpace(content) == "" {
		return
	}
	if _, err := c.client.SendText(ctx, scope, target, content, msgID); err != nil {
		slog.WarnContext(ctx, "qqbot_reply_failed", "err", err.Error())
	}
}

func (c qqbotConnector) resolvePersona(ctx context.Context, ch data.Channel) (*data.Persona, string, error) {
	persona, personaRef, _, err := mustValidateQQBotActivation(ctx, ch.AccountID, c.personasRepo, ch.PersonaID, ch.ConfigJSON)
	if err != nil {
		return nil, "", err
	}
	return persona, personaRef, nil
}

func (c qqbotConnector) resolveThreadID(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	personaID uuid.UUID,
	projectID uuid.UUID,
	identity data.ChannelIdentity,
	isPrivate bool,
	platformChatID string,
	displayName string,
) (uuid.UUID, error) {
	threadRepoTx := c.threadRepo.WithTx(tx)
	if isPrivate {
		existing, err := c.channelDMThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, identity.ID, personaID, "")
		if err != nil {
			return uuid.Nil, err
		}
		if existing != nil {
			return existing.ThreadID, nil
		}
		titleText := strings.TrimSpace(displayName)
		if titleText == "" {
			titleText = platformChatID
		}
		titleText += " (QQBot 私聊)"
		thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, &titleText, false)
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := c.channelDMThreadsRepo.WithTx(tx).Create(ctx, ch.ID, identity.ID, personaID, "", thread.ID); err != nil {
			return uuid.Nil, err
		}
		return thread.ID, nil
	}

	existing, err := c.channelGroupThreadsRepo.WithTx(tx).GetByBinding(ctx, ch.ID, platformChatID, personaID)
	if err != nil {
		return uuid.Nil, err
	}
	if existing != nil {
		return existing.ThreadID, nil
	}
	titleText := "QQBot 群 " + platformChatID
	thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, &titleText, false)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := c.channelGroupThreadsRepo.WithTx(tx).Create(ctx, ch.ID, platformChatID, personaID, thread.ID); err != nil {
		return uuid.Nil, err
	}
	return thread.ID, nil
}

func (c qqbotConnector) notifyInput(ctx context.Context, runID uuid.UUID) {
	if c.inputNotify != nil && runID != uuid.Nil {
		c.inputNotify(ctx, runID)
	}
}

func buildQQBotChannelDeliveryPayload(channelID uuid.UUID, identityID uuid.UUID, platformChatID, messageID, conversationType string) map[string]any {
	return map[string]any{
		"channel_id":   channelID.String(),
		"channel_type": "qqbot",
		"conversation_ref": map[string]any{
			"target": platformChatID,
		},
		"inbound_message_ref": map[string]any{
			"message_id": messageID,
		},
		"trigger_message_ref": map[string]any{
			"message_id": messageID,
		},
		"platform_chat_id":           platformChatID,
		"platform_message_id":        messageID,
		"sender_channel_identity_id": identityID.String(),
		"conversation_type":          conversationType,
	}
}

func buildQQBotEnvelopeText(identityID uuid.UUID, displayName, conversationType, body, timestamp, senderID, messageID string) string {
	lines := []string{
		fmt.Sprintf(`display-name: "%s"`, escapeEnvelopeValue(displayName)),
		`channel: "qqbot"`,
		fmt.Sprintf(`conversation-type: "%s"`, conversationType),
		fmt.Sprintf(`sender-ref: "%s"`, identityID.String()),
		fmt.Sprintf(`platform-user-id: "%s"`, escapeEnvelopeValue(senderID)),
		fmt.Sprintf(`message-id: "%s"`, escapeEnvelopeValue(messageID)),
	}
	if strings.TrimSpace(timestamp) != "" {
		lines = append(lines, fmt.Sprintf(`time: "%s"`, escapeEnvelopeValue(timestamp)))
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + strings.TrimSpace(body)
}
