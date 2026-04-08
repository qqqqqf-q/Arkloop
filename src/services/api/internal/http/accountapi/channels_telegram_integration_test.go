//go:build !desktop

package accountapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type telegramChannelsTestEnv struct {
	handler      nethttp.Handler
	pool         *pgxpool.Pool
	accessToken  string
	accountID    uuid.UUID
	userID       uuid.UUID
	projectID    uuid.UUID
	personaID    uuid.UUID
	channelsRepo *data.ChannelsRepository
	secretsRepo  *data.SecretsRepository
	keyRing      *apiCrypto.KeyRing
}

func setupTelegramChannelsTestEnv(t *testing.T, botClient *telegrambot.Client) telegramChannelsTestEnv {
	return setupTelegramChannelsTestEnvWithAttachmentStore(t, botClient, nil)
}

type failingAttachmentPutStore struct{}

func (failingAttachmentPutStore) PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error {
	_ = ctx
	_ = key
	_ = data
	_ = options
	return io.ErrUnexpectedEOF
}

func setupTelegramChannelsTestEnvWithAttachmentStore(
	t *testing.T,
	botClient *telegrambot.Client,
	attachmentStore MessageAttachmentPutStore,
) telegramChannelsTestEnv {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_channels_telegram")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := data.NewPool(context.Background(), db.DSN, data.PoolLimits{MaxConns: 16, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	SetTelegramPassiveIngestSyncForTest(true)
	t.Cleanup(func() { SetTelegramPassiveIngestSyncForTest(false) })

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	userCredRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("user credential repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh token repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	accountRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("account repo: %v", err)
	}
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("project repo: %v", err)
	}
	personasRepo, err := data.NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("personas repo: %v", err)
	}
	channelsRepo, err := data.NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("channels repo: %v", err)
	}
	channelIdentitiesRepo, err := data.NewChannelIdentitiesRepository(pool)
	if err != nil {
		t.Fatalf("channel identities repo: %v", err)
	}
	channelIdentityLinksRepo, err := data.NewChannelIdentityLinksRepository(pool)
	if err != nil {
		t.Fatalf("channel identity links repo: %v", err)
	}
	channelBindCodesRepo, err := data.NewChannelBindCodesRepository(pool)
	if err != nil {
		t.Fatalf("bind repo: %v", err)
	}
	channelDMThreadsRepo, err := data.NewChannelDMThreadsRepository(pool)
	if err != nil {
		t.Fatalf("dm threads repo: %v", err)
	}
	channelGroupThreadsRepo, err := data.NewChannelGroupThreadsRepository(pool)
	if err != nil {
		t.Fatalf("group threads repo: %v", err)
	}
	channelReceiptsRepo, err := data.NewChannelMessageReceiptsRepository(pool)
	if err != nil {
		t.Fatalf("receipts repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("message repo: %v", err)
	}
	runEventRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("run repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("job repo: %v", err)
	}
	creditsRepo, err := data.NewCreditsRepository(pool)
	if err != nil {
		t.Fatalf("credits repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	keyRing, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, keyRing)
	if err != nil {
		t.Fatalf("secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("token service: %v", err)
	}
	authService, err := auth.NewService(userRepo, userCredRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, projectRepo)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}

	account, err := accountRepo.Create(context.Background(), "telegram-account", "Telegram Account", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	user, err := userRepo.Create(context.Background(), "telegram-owner", "owner@test.com", "zh")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := membershipRepo.Create(context.Background(), account.ID, user.ID, auth.RoleAccountAdmin); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	project, err := projectRepo.CreateDefaultForOwner(context.Background(), account.ID, user.ID)
	if err != nil {
		t.Fatalf("create default project: %v", err)
	}
	persona, err := personasRepo.Create(
		context.Background(),
		project.ID,
		"telegram-persona",
		"1",
		"Telegram Persona",
		nil,
		"hello",
		nil,
		nil,
		json.RawMessage(`{}`),
		json.RawMessage(`{}`),
		nil,
		nil,
		nil,
		"auto",
		true,
		"none",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create persona: %v", err)
	}

	accessToken, err := tokenService.Issue(user.ID, account.ID, auth.RoleAccountAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	mux := nethttp.NewServeMux()
	RegisterRoutes(mux, Deps{
		AuthService:              authService,
		AccountMembershipRepo:    membershipRepo,
		ThreadRepo:               threadRepo,
		ProjectRepo:              projectRepo,
		APIKeysRepo:              nil,
		Pool:                     pool,
		AccountRepo:              accountRepo,
		SecretsRepo:              secretsRepo,
		ChannelsRepo:             channelsRepo,
		ChannelIdentitiesRepo:    channelIdentitiesRepo,
		ChannelIdentityLinksRepo: channelIdentityLinksRepo,
		ChannelBindCodesRepo:     channelBindCodesRepo,
		ChannelDMThreadsRepo:     channelDMThreadsRepo,
		ChannelGroupThreadsRepo:  channelGroupThreadsRepo,
		ChannelReceiptsRepo:      channelReceiptsRepo,
		UsersRepo:                userRepo,
		MessageRepo:              messageRepo,
		RunEventRepo:             runEventRepo,
		JobRepo:                  jobRepo,
		CreditsRepo:              creditsRepo,
		PersonasRepo:             personasRepo,
		AppBaseURL:               "https://app.example",
		TelegramBotClient:        botClient,
		MessageAttachmentStore:   attachmentStore,
	})

	return telegramChannelsTestEnv{
		handler:      mux,
		pool:         pool,
		accessToken:  accessToken,
		accountID:    account.ID,
		userID:       user.ID,
		projectID:    project.ID,
		personaID:    persona.ID,
		channelsRepo: channelsRepo,
		secretsRepo:  secretsRepo,
		keyRing:      keyRing,
	}
}

func TestUpdateTelegramChannelActivationRegistersWebhookAndCommands(t *testing.T) {
	var paths []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	createResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels",
		map[string]any{
			"channel_type": "telegram",
			"bot_token":    "bot-token",
			"persona_id":   env.personaID.String(),
			"config_json": map[string]any{
				"allowed_user_ids": []string{"10001"},
			},
		},
		authHeader(env.accessToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create channel: %d %s", createResp.Code, createResp.Body.String())
	}
	channel := decodeJSONBodyAccount[channelResponse](t, createResp.Body.Bytes())

	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID,
		map[string]any{"is_active": true},
		authHeader(env.accessToken),
	)
	if updateResp.Code != nethttp.StatusOK {
		t.Fatalf("activate channel: %d %s", updateResp.Code, updateResp.Body.String())
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 telegram calls, got %d", len(paths))
	}
	if paths[0] != "/botbot-token/setWebhook" || paths[1] != "/botbot-token/setMyCommands" {
		t.Fatalf("unexpected telegram calls: %#v", paths)
	}
	if got := updateResp.Body.String(); !strings.Contains(got, `"command":"new"`) {
		t.Fatalf("expected /new command to be registered, got %s", got)
	}
	updated, err := env.channelsRepo.GetByID(context.Background(), mustUUID(t, channel.ID))
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if updated == nil || !updated.IsActive {
		t.Fatal("channel should be active after successful activation")
	}
}

func TestUpdateTelegramChannelActivationFailClosed(t *testing.T) {
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/botbot-token/setWebhook" {
			_, _ = io.WriteString(w, `{"ok":false,"description":"boom"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	createResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels",
		map[string]any{
			"channel_type": "telegram",
			"bot_token":    "bot-token",
			"persona_id":   env.personaID.String(),
			"config_json": map[string]any{
				"allowed_user_ids": []string{"10001"},
			},
		},
		authHeader(env.accessToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create channel: %d %s", createResp.Code, createResp.Body.String())
	}
	channel := decodeJSONBodyAccount[channelResponse](t, createResp.Body.Bytes())

	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID,
		map[string]any{"is_active": true},
		authHeader(env.accessToken),
	)
	if updateResp.Code != nethttp.StatusBadGateway {
		t.Fatalf("expected bad gateway, got %d %s", updateResp.Code, updateResp.Body.String())
	}
	updated, err := env.channelsRepo.GetByID(context.Background(), mustUUID(t, channel.ID))
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if updated == nil || updated.IsActive {
		t.Fatal("channel should remain inactive after failed activation")
	}
}

func TestTelegramWebhookCreatesRunAndDedupes(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "demo-cred^gpt-5-mini")

	payload := map[string]any{
		"message": map[string]any{
			"message_id": 42,
			"date":       1710000000,
			"text":       "hello from telegram",
			"chat": map[string]any{
				"id":   10001,
				"type": "private",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		payload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
	}

	resp2 := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		payload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp2.Code != nethttp.StatusOK {
		t.Fatalf("webhook duplicate status: %d %s", resp2.Code, resp2.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_dm_threads`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE direction = 'inbound'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM users WHERE source = 'channel_shadow'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities WHERE user_id IS NOT NULL`, 0)

	var payloadJSON []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT payload_json::text::jsonb FROM jobs LIMIT 1`).Scan(&payloadJSON); err != nil {
		t.Fatalf("query job payload: %v", err)
	}
	var jobEnvelope map[string]any
	if err := json.Unmarshal(payloadJSON, &jobEnvelope); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	jobPayload, _ := jobEnvelope["payload"].(map[string]any)
	delivery, ok := jobPayload["channel_delivery"].(map[string]any)
	if !ok {
		t.Fatalf("expected channel_delivery in payload: %#v", jobPayload)
	}
	if _, ok := jobPayload["model"]; ok {
		t.Fatalf("did not expect model in job payload: %#v", jobPayload)
	}
	conversationRef, _ := delivery["conversation_ref"].(map[string]any)
	if got := asString(conversationRef["target"]); got != "10001" {
		t.Fatalf("unexpected conversation_ref: %#v", delivery)
	}
	triggerRef, _ := delivery["trigger_message_ref"].(map[string]any)
	if got := asString(triggerRef["message_id"]); got != "1" {
		t.Fatalf("unexpected trigger_message_ref: %#v", delivery)
	}

	var startedJSON []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT data_json::text::jsonb FROM run_events WHERE type = 'run.started' LIMIT 1`).Scan(&startedJSON); err != nil {
		t.Fatalf("query run.started: %v", err)
	}
	var started map[string]any
	if err := json.Unmarshal(startedJSON, &started); err != nil {
		t.Fatalf("decode run.started: %v", err)
	}
	if got := strings.TrimSpace(asString(started["model"])); got != "demo-cred^gpt-5-mini" {
		t.Fatalf("unexpected run.started model: %q", got)
	}
}

func TestTelegramWebhookRejectsInvalidSignature(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		map[string]any{
			"message": map[string]any{
				"message_id": 1,
				"date":       1710000000,
				"text":       "hello",
				"chat": map[string]any{
					"id":   10001,
					"type": "private",
				},
				"from": map[string]any{
					"id":     10001,
					"is_bot": false,
				},
			},
		},
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": "wrong-secret"},
	)
	if resp.Code != nethttp.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d %s", resp.Code, resp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_dm_threads`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)
}

func TestCreateTelegramChannelRejectsInvalidDefaultModelSelector(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels",
		map[string]any{
			"channel_type": "telegram",
			"bot_token":    "bot-token",
			"persona_id":   env.personaID.String(),
			"config_json": map[string]any{
				"allowed_user_ids": []string{"10001"},
				"default_model":    "bad^selector",
			},
		},
		authHeader(env.accessToken),
	)
	if resp.Code != nethttp.StatusUnprocessableEntity {
		t.Fatalf("expected validation error, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestUpdateTelegramChannelRejectsInvalidDefaultModelSelector(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String(),
		map[string]any{
			"config_json": map[string]any{
				"default_model": "bad^selector",
			},
		},
		authHeader(env.accessToken),
	)
	if resp.Code != nethttp.StatusUnprocessableEntity {
		t.Fatalf("expected validation error, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestTelegramHeartbeatModelRejectsInvalidSelectorWithoutPersisting(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	payload := map[string]any{
		"message": map[string]any{
			"message_id": 51,
			"date":       1710000000,
			"text":       "/heartbeat model bad^selector",
			"chat": map[string]any{
				"id":   -100123,
				"type": "group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		payload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
	}

	var heartbeatModel string
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT heartbeat_model FROM channel_identities WHERE channel_type = 'telegram' AND platform_subject_id = $1`,
		"-100123",
	).Scan(&heartbeatModel); err != nil {
		t.Fatalf("query heartbeat model: %v", err)
	}
	if strings.TrimSpace(heartbeatModel) != "" {
		t.Fatalf("expected heartbeat_model to remain empty, got %q", heartbeatModel)
	}
}

func TestTelegramHeartbeatOnCreatesScheduledTriggerImmediately(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	seedTelegramSelectorRoute(t, env, "demo-cred", "gpt-5-mini")
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	setModelPayload := map[string]any{
		"message": map[string]any{
			"message_id": 61,
			"date":       1710000000,
			"text":       "/heartbeat model demo-cred^gpt-5-mini",
			"chat": map[string]any{
				"id":   -100456,
				"type": "group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		setModelPayload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("set model webhook status: %d %s", resp.Code, resp.Body.String())
	}

	enablePayload := map[string]any{
		"message": map[string]any{
			"message_id": 62,
			"date":       1710000060,
			"text":       "/heartbeat on",
			"chat": map[string]any{
				"id":   -100456,
				"type": "group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		enablePayload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("enable heartbeat webhook status: %d %s", resp.Code, resp.Body.String())
	}

	var gotModel string
	var gotInterval int
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT st.model, st.interval_min
		   FROM scheduled_triggers st
		   JOIN channel_identities ci ON ci.id = st.channel_identity_id
		  WHERE ci.channel_type = 'telegram' AND ci.platform_subject_id = $1`,
		"-100456",
	).Scan(&gotModel, &gotInterval); err != nil {
		t.Fatalf("query scheduled trigger: %v", err)
	}
	if gotModel != "demo-cred^gpt-5-mini" {
		t.Fatalf("unexpected scheduled trigger model: %q", gotModel)
	}
	if gotInterval != 30 {
		t.Fatalf("unexpected scheduled trigger interval: %d", gotInterval)
	}
}

func TestUpdateTelegramChannelInactiveDeletesScheduledTriggerImmediately(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	enablePayload := map[string]any{
		"message": map[string]any{
			"message_id": 71,
			"date":       1710000000,
			"text":       "/heartbeat on",
			"chat": map[string]any{
				"id":   -100789,
				"type": "group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		enablePayload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("enable heartbeat webhook status: %d %s", resp.Code, resp.Body.String())
	}

	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String(),
		map[string]any{"is_active": false},
		authHeader(env.accessToken),
	)
	if updateResp.Code != nethttp.StatusOK {
		t.Fatalf("update channel inactive status: %d %s", updateResp.Code, updateResp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM scheduled_triggers`, 0)
}

func TestDeleteTelegramChannelDeletesScheduledTriggerImmediately(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	enablePayload := map[string]any{
		"message": map[string]any{
			"message_id": 81,
			"date":       1710000000,
			"text":       "/heartbeat on",
			"chat": map[string]any{
				"id":   -100987,
				"type": "group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		enablePayload,
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("enable heartbeat webhook status: %d %s", resp.Code, resp.Body.String())
	}

	deleteResp := doJSONAccount(
		env.handler,
		nethttp.MethodDelete,
		"/v1/channels/"+channel.ID.String(),
		nil,
		authHeader(env.accessToken),
	)
	if deleteResp.Code != nethttp.StatusOK {
		t.Fatalf("delete channel status: %d %s", deleteResp.Code, deleteResp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM scheduled_triggers`, 0)
}

func TestTelegramWebhookRejectsUserOutsideAllowlistWithoutCreatingConversation(t *testing.T) {
	var (
		paths        []string
		sendMessages []telegrambot.SendMessageRequest
	)
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		paths = append(paths, r.URL.Path)
		var body telegrambot.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			sendMessages = append(sendMessages, body)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		map[string]any{
			"message": map[string]any{
				"message_id": 99,
				"date":       1710000000,
				"text":       "hello",
				"chat": map[string]any{
					"id":   99999,
					"type": "private",
				},
				"from": map[string]any{
					"id":         99999,
					"is_bot":     false,
					"first_name": "Mallory",
				},
			},
		},
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_receipts`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_dm_threads`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 0)

	if len(paths) != 1 || paths[0] != "/botbot-token/sendMessage" {
		t.Fatalf("unexpected telegram calls: %#v", paths)
	}
	if len(sendMessages) != 1 {
		t.Fatalf("expected 1 sendMessage request, got %d", len(sendMessages))
	}
	if sendMessages[0].ChatID != "99999" {
		t.Fatalf("unexpected chat_id: %#v", sendMessages[0])
	}
	if strings.TrimSpace(sendMessages[0].Text) != "当前账号未被授权使用这个机器人。" {
		t.Fatalf("unexpected rejection text: %q", sendMessages[0].Text)
	}
}

func TestUpdateTelegramChannelDisableDeletesWebhook(t *testing.T) {
	var paths []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID.String(),
		map[string]any{"is_active": false},
		authHeader(env.accessToken),
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("disable channel: %d %s", resp.Code, resp.Body.String())
	}
	if len(paths) != 1 || paths[0] != "/botbot-token/deleteWebhook" {
		t.Fatalf("unexpected telegram calls: %#v", paths)
	}

	updated, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if updated == nil || updated.IsActive {
		t.Fatal("channel should be inactive after disable")
	}
}

func TestDeleteTelegramChannelDeletesWebhook(t *testing.T) {
	var paths []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	req := httptest.NewRequest(nethttp.MethodDelete, "/v1/channels/"+channel.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+env.accessToken)
	recorder := httptest.NewRecorder()
	env.handler.ServeHTTP(recorder, req)
	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("delete channel: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(paths) != 1 || paths[0] != "/botbot-token/deleteWebhook" {
		t.Fatalf("unexpected telegram calls: %#v", paths)
	}

	deleted, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get deleted channel: %v", err)
	}
	if deleted != nil {
		t.Fatal("channel should be deleted")
	}
}

func TestTelegramWebhookNewCommandStartsFreshDMThread(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "")

	firstMessage := map[string]any{
		"message": map[string]any{
			"message_id": 1,
			"date":       1710000000,
			"text":       "first",
			"chat": map[string]any{
				"id":   10001,
				"type": "private",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	newCommand := map[string]any{
		"message": map[string]any{
			"message_id": 2,
			"date":       1710000001,
			"text":       "/new",
			"chat": map[string]any{
				"id":   10001,
				"type": "private",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	secondMessage := map[string]any{
		"message": map[string]any{
			"message_id": 3,
			"date":       1710000002,
			"text":       "second",
			"chat": map[string]any{
				"id":   10001,
				"type": "private",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	for _, payload := range []map[string]any{firstMessage, newCommand, secondMessage} {
		resp := doJSONAccount(
			env.handler,
			nethttp.MethodPost,
			"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
			payload,
			headers,
		)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
		}
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_dm_threads`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 2)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 2)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 2)

	rows, err := env.pool.Query(context.Background(), `SELECT DISTINCT thread_id FROM messages ORDER BY thread_id ASC`)
	if err != nil {
		t.Fatalf("query message threads: %v", err)
	}
	defer rows.Close()
	var threadIDs []uuid.UUID
	for rows.Next() {
		var threadID uuid.UUID
		if err := rows.Scan(&threadID); err != nil {
			t.Fatalf("scan thread id: %v", err)
		}
		threadIDs = append(threadIDs, threadID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate thread ids: %v", err)
	}
	if len(threadIDs) != 2 {
		t.Fatalf("expected 2 message threads after /new, got %d", len(threadIDs))
	}

	var mappedThreadID uuid.UUID
	if err := env.pool.QueryRow(context.Background(), `SELECT thread_id FROM channel_dm_threads LIMIT 1`).Scan(&mappedThreadID); err != nil {
		t.Fatalf("query mapped thread: %v", err)
	}
	if mappedThreadID != threadIDs[1] {
		t.Fatalf("expected latest thread to stay mapped, got %s want %s", mappedThreadID, threadIDs[1])
	}
}

func TestTelegramWebhookStoresStructuredInboundMessage(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_user_ids": []string{"10001"},
	})

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		map[string]any{
			"message": map[string]any{
				"message_id": 7,
				"date":       1710000000,
				"caption":    "图像说明",
				"chat": map[string]any{
					"id":   10001,
					"type": "private",
				},
				"photo": []map[string]any{
					{"file_id": "small-photo", "file_size": 64, "width": 32, "height": 32},
					{"file_id": "large-photo", "file_size": 256, "width": 128, "height": 128},
				},
				"from": map[string]any{
					"id":         10001,
					"is_bot":     false,
					"first_name": "Alice",
					"username":   "alice",
				},
			},
		},
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
	}

	var contentJSON []byte
	var metadataJSON []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT content_json::text::jsonb, metadata_json::text::jsonb FROM messages LIMIT 1`).Scan(&contentJSON, &metadataJSON); err != nil {
		t.Fatalf("query structured message: %v", err)
	}

	var content struct {
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(contentJSON, &content); err != nil {
		t.Fatalf("decode content_json: %v", err)
	}
	if len(content.Parts) != 1 || content.Parts[0].Type != "text" {
		t.Fatalf("unexpected content_json: %s", string(contentJSON))
	}
	if !strings.Contains(content.Parts[0].Text, `[图片: image]`) {
		t.Fatalf("expected attachment placeholder in content_json, got %s", content.Parts[0].Text)
	}

	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode metadata_json: %v", err)
	}
	senderRef := `sender-ref: "` + asString(metadata["channel_identity_id"]) + `"`
	if !strings.Contains(content.Parts[0].Text, senderRef) {
		t.Fatalf("expected prompt header to include sender ref %q, got %s", senderRef, content.Parts[0].Text)
	}
	for _, forbidden := range []string{`platform-message-id: "7"`, `platform-chat-id: "10001"`, `channel-identity-id:`} {
		if strings.Contains(content.Parts[0].Text, forbidden) {
			t.Fatalf("expected prompt header to omit transport metadata %q, got %s", forbidden, content.Parts[0].Text)
		}
	}
	if !strings.Contains(content.Parts[0].Text, `message-id: "`) {
		t.Fatalf("expected prompt header to contain message-id field")
	}
	attachments, _ := metadata["media_attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("expected one media attachment in metadata, got %#v", metadata["media_attachments"])
	}
	if got := asString(metadata["platform_message_id"]); got != "7" {
		t.Fatalf("unexpected platform_message_id: %q", got)
	}
}

func TestTelegramWebhookSendsImmediateTypingForReplyableMessage(t *testing.T) {
	var paths []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_user_ids":          []string{"10001"},
		"telegram_typing_indicator": true,
	})

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		map[string]any{
			"message": map[string]any{
				"message_id": 17,
				"date":       1710000000,
				"text":       "hello",
				"chat": map[string]any{
					"id":   10001,
					"type": "private",
				},
				"from": map[string]any{
					"id":         10001,
					"is_bot":     false,
					"first_name": "Alice",
				},
			},
		},
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
	}
	if len(paths) != 1 || paths[0] != "/botbot-token/sendChatAction" {
		t.Fatalf("unexpected telegram calls: %#v", paths)
	}
}

func TestTelegramWebhookHeartbeatCommandDoesNotSendImmediateTyping(t *testing.T) {
	var paths []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient(server.URL, server.Client()))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_user_ids":          []string{"10001"},
		"telegram_typing_indicator": true,
		"bot_username":              "arkloopbot",
	})

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/telegram/"+channel.ID.String()+"/webhook",
		map[string]any{
			"message": map[string]any{
				"message_id": 18,
				"date":       1710000000,
				"text":       "/heartbeat",
				"chat": map[string]any{
					"id":   10001,
					"type": "private",
				},
				"from": map[string]any{
					"id":         10001,
					"is_bot":     false,
					"first_name": "Alice",
				},
			},
		},
		map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)},
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("webhook status: %d %s", resp.Code, resp.Body.String())
	}
	for _, path := range paths {
		if path == "/botbot-token/sendChatAction" {
			t.Fatalf("heartbeat command should not send immediate typing: %#v", paths)
		}
	}
}

func TestTelegramWebhookGroupMessagePassiveAndActive(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_user_ids": []string{"10001"},
		"bot_username":     "arkloopbot",
	})
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	passive := map[string]any{
		"message": map[string]any{
			"message_id": 11,
			"date":       1710000000,
			"text":       "群里闲聊",
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", passive, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("passive webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)

	active := map[string]any{
		"message": map[string]any{
			"message_id": 12,
			"date":       1710000001,
			"text":       "@arkloopbot 帮我看看",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", active, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("active webhook status: %d %s", resp.Code, resp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 2)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)

	var payloadJSON []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT payload_json::text::jsonb FROM jobs LIMIT 1`).Scan(&payloadJSON); err != nil {
		t.Fatalf("query job payload: %v", err)
	}
	var jobEnvelope map[string]any
	if err := json.Unmarshal(payloadJSON, &jobEnvelope); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	jobPayload, _ := jobEnvelope["payload"].(map[string]any)
	delivery, _ := jobPayload["channel_delivery"].(map[string]any)
	if got := asString(delivery["conversation_type"]); got != "supergroup" {
		t.Fatalf("unexpected conversation_type: %#v", delivery)
	}
	if got := asString(delivery["reply_to_message_id"]); got != "12" {
		t.Fatalf("unexpected reply_to_message_id: %#v", delivery)
	}
	if got := asString(delivery["platform_message_id"]); got != "12" {
		t.Fatalf("unexpected platform_message_id: %#v", delivery)
	}
	conversationRef, _ := delivery["conversation_ref"].(map[string]any)
	if got := asString(conversationRef["target"]); got != "-20001" {
		t.Fatalf("unexpected conversation_ref: %#v", delivery)
	}

	var contentJSON []byte
	var metadataJSON []byte
	if err := env.pool.QueryRow(context.Background(), `
		SELECT content_json::text::jsonb, metadata_json::text::jsonb
		  FROM messages
		 ORDER BY created_at DESC
		 LIMIT 1`,
	).Scan(&contentJSON, &metadataJSON); err != nil {
		t.Fatalf("query latest message: %v", err)
	}
	var content struct {
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(contentJSON, &content); err != nil {
		t.Fatalf("decode group content_json: %v", err)
	}
	if len(content.Parts) != 1 || content.Parts[0].Type != "text" {
		t.Fatalf("unexpected group content_json: %s", string(contentJSON))
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode group metadata_json: %v", err)
	}
	senderRef := `sender-ref: "` + asString(metadata["channel_identity_id"]) + `"`
	if !strings.Contains(content.Parts[0].Text, senderRef) {
		t.Fatalf("expected group prompt header to include sender ref %q, got %s", senderRef, content.Parts[0].Text)
	}
	for _, forbidden := range []string{`platform-message-id: "12"`, `platform-chat-id: "-20001"`} {
		if strings.Contains(content.Parts[0].Text, forbidden) {
			t.Fatalf("expected group prompt header to omit transport metadata %q, got %s", forbidden, content.Parts[0].Text)
		}
	}
	if !strings.Contains(content.Parts[0].Text, `message-id: "`) {
		t.Fatalf("expected group prompt header to contain message-id field")
	}
}

func TestTelegramWebhookReplyUsesParentAsTriggerMessage(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"bot_username":         "arkloopbot",
		"telegram_bot_user_id": 777002,
	})

	headers := authHeader(env.accessToken)
	replyPayload := map[string]any{
		"message": map[string]any{
			"message_id": 14,
			"date":       1710000002,
			"text":       "继续说",
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
			"reply_to_message": map[string]any{
				"message_id": 11,
				"date":       1710000001,
				"text":       "bot old message",
				"chat": map[string]any{
					"id":    -20001,
					"type":  "supergroup",
					"title": "Arkloop Group",
				},
				"from": map[string]any{
					"id":         777002,
					"is_bot":     true,
					"first_name": "Arkloop",
				},
			},
		},
	}

	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", replyPayload, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("reply webhook status: %d %s", resp.Code, resp.Body.String())
	}

	var payloadJSON []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT payload_json::text::jsonb FROM jobs LIMIT 1`).Scan(&payloadJSON); err != nil {
		t.Fatalf("query job payload: %v", err)
	}
	var jobEnvelope map[string]any
	if err := json.Unmarshal(payloadJSON, &jobEnvelope); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	jobPayload, _ := jobEnvelope["payload"].(map[string]any)
	delivery, _ := jobPayload["channel_delivery"].(map[string]any)
	triggerRef, _ := delivery["trigger_message_ref"].(map[string]any)
	if got := asString(triggerRef["message_id"]); got != "11" {
		t.Fatalf("unexpected trigger_message_ref: %#v", delivery)
	}
	if got := asString(delivery["reply_to_message_id"]); got != "11" {
		t.Fatalf("unexpected reply_to_message_id: %#v", delivery)
	}
	if got := asString(delivery["inbound_reply_to_message_id"]); got != "11" {
		t.Fatalf("unexpected inbound_reply_to_message_id: %#v", delivery)
	}
}

func TestTelegramWebhookPassiveMediaFailureDoesNotPersistReceipt(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile") && r.Method == nethttp.MethodPost:
			_, _ = io.WriteString(w, `{"ok":true,"result":{"file_path":"photos/a.png"}}`)
		case strings.HasPrefix(r.URL.Path, "/file/bot"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			nethttp.NotFound(w, r)
		}
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnvWithAttachmentStore(
		t,
		telegrambot.NewClient(server.URL, server.Client()),
		failingAttachmentPutStore{},
	)
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"bot_username":         "arkloopbot",
		"telegram_bot_user_id": 999,
	})
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	passive := map[string]any{
		"message": map[string]any{
			"message_id": 31,
			"date":       1710000200,
			"caption":    "图来了",
			"photo": []map[string]any{
				{"file_id": "fid", "width": 10, "height": 10, "file_size": len(png)},
			},
			"chat": map[string]any{
				"id":    -20003,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", passive, headers)
	if resp.Code != nethttp.StatusInternalServerError {
		t.Fatalf("passive webhook status: %d %s", resp.Code, resp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_receipts`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE direction = 'inbound'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 0)
}

func TestTelegramWebhookActiveRunSecondMessageUsesProvidedInput(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "tg-token-active-run", nil, "")
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	first := map[string]any{
		"message": map[string]any{
			"message_id": 21,
			"date":       1710000100,
			"text":       "@arkloopbot first",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20002,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", first, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("first webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)

	second := map[string]any{
		"message": map[string]any{
			"message_id": 22,
			"date":       1710000101,
			"text":       "@arkloopbot second",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20002,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", second, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("second webhook status: %d %s", resp.Code, resp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 2)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM run_events WHERE type = 'run.input_provided'`, 1)
}

func TestTelegramWebhookMentionDoesNotInjectIntoActiveHeartbeatRun(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "tg-token-heartbeat-run", nil, "")
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	channelGroupThreadsRepo, err := data.NewChannelGroupThreadsRepository(env.pool)
	if err != nil {
		t.Fatalf("group threads repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(env.pool)
	if err != nil {
		t.Fatalf("thread repo: %v", err)
	}
	runEventRepo, err := data.NewRunEventRepository(env.pool)
	if err != nil {
		t.Fatalf("run repo: %v", err)
	}

	thread, err := threadRepo.Create(context.Background(), env.accountID, &env.userID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := channelGroupThreadsRepo.Create(context.Background(), channel.ID, "-20009", env.personaID, thread.ID); err != nil {
		t.Fatalf("bind group thread: %v", err)
	}
	heartbeatRun, _, err := runEventRepo.CreateRunWithStartedEvent(
		context.Background(),
		env.accountID,
		thread.ID,
		&env.userID,
		"run.started",
		map[string]any{
			"persona_id": env.personaID.String(),
			"run_kind":   "heartbeat",
		},
	)
	if err != nil {
		t.Fatalf("create heartbeat run: %v", err)
	}

	payload := map[string]any{
		"message": map[string]any{
			"message_id": 31,
			"date":       1710000102,
			"text":       "@arkloopbot help",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20009,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", payload, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("mention webhook status: %d %s", resp.Code, resp.Body.String())
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 2)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM run_events WHERE run_id = '`+heartbeatRun.ID.String()+`' AND type = 'run.input_provided'`, 0)
}

func TestTelegramPollPassiveMediaFailureDoesNotPersistReceipt(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile") && r.Method == nethttp.MethodPost:
			_, _ = io.WriteString(w, `{"ok":true,"result":{"file_path":"photos/a.png"}}`)
		case strings.HasPrefix(r.URL.Path, "/file/bot"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			nethttp.NotFound(w, r)
		}
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"bot_username":         "arkloopbot",
		"telegram_bot_user_id": 999,
	})

	channelIdentitiesRepo, err := data.NewChannelIdentitiesRepository(env.pool)
	if err != nil {
		t.Fatalf("channel identities repo: %v", err)
	}
	channelGroupThreadsRepo, err := data.NewChannelGroupThreadsRepository(env.pool)
	if err != nil {
		t.Fatalf("channel group threads repo: %v", err)
	}
	channelReceiptsRepo, err := data.NewChannelMessageReceiptsRepository(env.pool)
	if err != nil {
		t.Fatalf("channel receipts repo: %v", err)
	}
	channelLedgerRepo, err := data.NewChannelMessageLedgerRepository(env.pool)
	if err != nil {
		t.Fatalf("channel ledger repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(env.pool)
	if err != nil {
		t.Fatalf("thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(env.pool)
	if err != nil {
		t.Fatalf("message repo: %v", err)
	}
	runEventRepo, err := data.NewRunEventRepository(env.pool)
	if err != nil {
		t.Fatalf("run event repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(env.pool)
	if err != nil {
		t.Fatalf("job repo: %v", err)
	}
	personasRepo, err := data.NewPersonasRepository(env.pool)
	if err != nil {
		t.Fatalf("personas repo: %v", err)
	}

	connector := telegramConnector{
		channelsRepo:            env.channelsRepo,
		channelIdentitiesRepo:   channelIdentitiesRepo,
		channelGroupThreadsRepo: channelGroupThreadsRepo,
		channelReceiptsRepo:     channelReceiptsRepo,
		channelLedgerRepo:       channelLedgerRepo,
		personasRepo:            personasRepo,
		threadRepo:              threadRepo,
		messageRepo:             messageRepo,
		runEventRepo:            runEventRepo,
		jobRepo:                 jobRepo,
		pool:                    env.pool,
		telegramClient:          telegrambot.NewClient(server.URL, server.Client()),
		attachmentStore:         failingAttachmentPutStore{},
	}

	update := telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			MessageID: 41,
			Date:      1710000300,
			Caption:   "图来了",
			Photo: []telegramPhotoSize{
				{FileID: "fid", Width: 10, Height: 10, FileSize: int64(len(png))},
			},
			Chat: telegramChat{
				ID:   -20004,
				Type: "supergroup",
				Title: func() *string {
					value := "Arkloop Group"
					return &value
				}(),
			},
			From: &telegramUser{
				ID:    10001,
				IsBot: false,
				FirstName: func() *string {
					value := "Alice"
					return &value
				}(),
			},
		},
	}

	err = connector.HandleUpdateForPoll(context.Background(), uuid.NewString(), channel, "bot-token", update)
	if err == nil {
		t.Fatal("expected poll passive media failure")
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_receipts`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE direction = 'inbound'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 0)

	err = connector.HandleUpdateForPoll(context.Background(), uuid.NewString(), channel, "bot-token", update)
	if err == nil {
		t.Fatal("expected second poll passive media failure")
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_receipts`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE direction = 'inbound'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 0)
}

func TestTelegramPollDuplicateMessageCreatesSingleRun(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannel(t, env, "bot-token", []string{"10001"}, "demo-cred^gpt-5-mini")

	channelIdentitiesRepo, err := data.NewChannelIdentitiesRepository(env.pool)
	if err != nil {
		t.Fatalf("channel identities repo: %v", err)
	}
	channelIdentityLinksRepo, err := data.NewChannelIdentityLinksRepository(env.pool)
	if err != nil {
		t.Fatalf("channel identity links repo: %v", err)
	}
	channelBindCodesRepo, err := data.NewChannelBindCodesRepository(env.pool)
	if err != nil {
		t.Fatalf("channel bind repo: %v", err)
	}
	channelDMThreadsRepo, err := data.NewChannelDMThreadsRepository(env.pool)
	if err != nil {
		t.Fatalf("channel dm threads repo: %v", err)
	}
	channelGroupThreadsRepo, err := data.NewChannelGroupThreadsRepository(env.pool)
	if err != nil {
		t.Fatalf("channel group threads repo: %v", err)
	}
	channelReceiptsRepo, err := data.NewChannelMessageReceiptsRepository(env.pool)
	if err != nil {
		t.Fatalf("channel receipts repo: %v", err)
	}
	channelLedgerRepo, err := data.NewChannelMessageLedgerRepository(env.pool)
	if err != nil {
		t.Fatalf("channel ledger repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(env.pool)
	if err != nil {
		t.Fatalf("thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(env.pool)
	if err != nil {
		t.Fatalf("message repo: %v", err)
	}
	runEventRepo, err := data.NewRunEventRepository(env.pool)
	if err != nil {
		t.Fatalf("run event repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(env.pool)
	if err != nil {
		t.Fatalf("job repo: %v", err)
	}
	personasRepo, err := data.NewPersonasRepository(env.pool)
	if err != nil {
		t.Fatalf("personas repo: %v", err)
	}

	connector := telegramConnector{
		channelsRepo:             env.channelsRepo,
		channelIdentitiesRepo:    channelIdentitiesRepo,
		channelIdentityLinksRepo: channelIdentityLinksRepo,
		channelBindCodesRepo:     channelBindCodesRepo,
		channelDMThreadsRepo:     channelDMThreadsRepo,
		channelGroupThreadsRepo:  channelGroupThreadsRepo,
		channelReceiptsRepo:      channelReceiptsRepo,
		channelLedgerRepo:        channelLedgerRepo,
		personasRepo:             personasRepo,
		threadRepo:               threadRepo,
		messageRepo:              messageRepo,
		runEventRepo:             runEventRepo,
		jobRepo:                  jobRepo,
		pool:                     env.pool,
		telegramClient:           telegrambot.NewClient("https://api.telegram.org", nil),
	}

	update := telegramUpdate{
		UpdateID: 7,
		Message: &telegramMessage{
			MessageID: 42,
			Date:      1710000000,
			Text:      "hello from telegram",
			Chat: telegramChat{
				ID:   10001,
				Type: "private",
			},
			From: &telegramUser{
				ID:         10001,
				IsBot:      false,
				FirstName:  func() *string { value := "Alice"; return &value }(),
				Username:   func() *string { value := "alice"; return &value }(),
				LastName:   nil,
			},
		},
	}

	if err := connector.HandleUpdateForPoll(context.Background(), uuid.NewString(), channel, "bot-token", update); err != nil {
		t.Fatalf("first poll update: %v", err)
	}
	if err := connector.HandleUpdateForPoll(context.Background(), uuid.NewString(), channel, "bot-token", update); err != nil {
		t.Fatalf("duplicate poll update: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_receipts`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_dm_threads`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE direction = 'inbound'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE direction = 'inbound' AND run_id IS NOT NULL`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)
}

func TestTelegramWebhookGroupNewDeniedWithoutBind(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_user_ids": []string{"10001"},
		"bot_username":     "arkloopbot",
	})
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	active := map[string]any{
		"message": map[string]any{
			"message_id": 12,
			"date":       1710000001,
			"text":       "@arkloopbot hi",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", active, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("active webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 1)

	newCmd := map[string]any{
		"message": map[string]any{
			"message_id": 13,
			"date":       1710000002,
			"text":       "/new",
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", newCmd, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("/new webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 1)
}

func TestTelegramWebhookGroupNewClearsBindingWhenBound(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_user_ids": []string{"10001"},
		"bot_username":     "arkloopbot",
	})
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	bindCreate := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/me/channel-binds", map[string]any{}, authHeader(env.accessToken))
	if bindCreate.Code != nethttp.StatusCreated {
		t.Fatalf("create bind code: %d %s", bindCreate.Code, bindCreate.Body.String())
	}
	var bindBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(bindCreate.Body.Bytes(), &bindBody); err != nil {
		t.Fatalf("decode bind response: %v", err)
	}
	if strings.TrimSpace(bindBody.Token) == "" {
		t.Fatal("empty bind token")
	}

	bindPrivate := map[string]any{
		"message": map[string]any{
			"message_id": 1,
			"date":       1710000000,
			"text":       "/bind " + bindBody.Token,
			"chat": map[string]any{
				"id":   10001,
				"type": "private",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", bindPrivate, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("bind webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities WHERE user_id IS NOT NULL`, 1)

	updatedChannel, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel owner: %v", err)
	}
	if updatedChannel == nil || updatedChannel.OwnerUserID == nil || *updatedChannel.OwnerUserID != env.userID {
		t.Fatalf("expected channel owner to be env user, got %#v", updatedChannel.OwnerUserID)
	}

	active := map[string]any{
		"message": map[string]any{
			"message_id": 12,
			"date":       1710000001,
			"text":       "@arkloopbot hi",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", active, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("active webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 1)

	newCmd := map[string]any{
		"message": map[string]any{
			"message_id": 13,
			"date":       1710000002,
			"text":       "/new",
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", newCmd, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("/new webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 0)

	activeAgain := map[string]any{
		"message": map[string]any{
			"message_id": 14,
			"date":       1710000003,
			"text":       "@arkloopbot again",
			"entities": []map[string]any{
				{"type": "mention", "offset": 0, "length": 11},
			},
			"chat": map[string]any{
				"id":    -20001,
				"type":  "supergroup",
				"title": "Arkloop Group",
			},
			"from": map[string]any{
				"id":         10001,
				"is_bot":     false,
				"first_name": "Alice",
			},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", activeAgain, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("second active webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_group_threads`, 1)
}

func TestTelegramWebhookGroupKeywordTriggerCreatesRun(t *testing.T) {
	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))
	channel := createActiveTelegramChannelWithConfig(t, env, "bot-token", map[string]any{
		"bot_username":     "arkloopbot",
		"bot_first_name":   "Arkloop",
		"trigger_keywords": []string{"草洛"},
	})
	headers := map[string]string{"X-Telegram-Bot-Api-Secret-Token": derefString(t, channel.WebhookSecret)}

	// passive: 不含关键词，不触发 run
	passive := map[string]any{
		"message": map[string]any{
			"message_id": 101,
			"date":       1710000000,
			"text":       "群里聊天不提 bot",
			"chat":       map[string]any{"id": -30001, "type": "supergroup", "title": "KW Group"},
			"from":       map[string]any{"id": 10001, "is_bot": false, "first_name": "Alice"},
		},
	}
	resp := doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", passive, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("passive webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)

	// keyword: 包含配置的关键词 "草洛"，触发 run
	keyword := map[string]any{
		"message": map[string]any{
			"message_id": 102,
			"date":       1710000001,
			"text":       "草洛出来",
			"chat":       map[string]any{"id": -30001, "type": "supergroup", "title": "KW Group"},
			"from":       map[string]any{"id": 10001, "is_bot": false, "first_name": "Alice"},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", keyword, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("keyword webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 2)

	// bot_first_name 也作为隐含关键词
	firstName := map[string]any{
		"message": map[string]any{
			"message_id": 103,
			"date":       1710000002,
			"text":       "arkloop 你好",
			"chat":       map[string]any{"id": -30001, "type": "supergroup", "title": "KW Group"},
			"from":       map[string]any{"id": 10001, "is_bot": false, "first_name": "Alice"},
		},
	}
	resp = doJSONAccount(env.handler, nethttp.MethodPost, "/v1/channels/telegram/"+channel.ID.String()+"/webhook", firstName, headers)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("first_name keyword webhook status: %d %s", resp.Code, resp.Body.String())
	}
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 2)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 3)

	// metadata 中 matches_keyword = true
	var metadataJSON []byte
	if err := env.pool.QueryRow(context.Background(),
		`SELECT metadata_json::text::jsonb FROM messages ORDER BY created_at DESC LIMIT 1`,
	).Scan(&metadataJSON); err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metadataJSON, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if got, _ := meta["matches_keyword"].(bool); !got {
		t.Fatalf("expected matches_keyword=true in metadata, got %v", meta["matches_keyword"])
	}
}

func doJSONAccount(handler nethttp.Handler, method string, path string, payload any, headers map[string]string) *httptest.ResponseRecorder {
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeJSONBodyAccount[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var dst T
	if err := json.Unmarshal(raw, &dst); err != nil {
		t.Fatalf("decode json: %v raw=%s", err, string(raw))
	}
	return dst
}

func createActiveTelegramChannel(t *testing.T, env telegramChannelsTestEnv, botToken string, allowedUserIDs []string, defaultModel string) data.Channel {
	t.Helper()
	config := map[string]any{"allowed_user_ids": allowedUserIDs}
	if strings.TrimSpace(defaultModel) != "" {
		config["default_model"] = strings.TrimSpace(defaultModel)
	}
	return createActiveTelegramChannelWithConfig(t, env, botToken, config)
}

func seedTelegramSelectorRoute(t *testing.T, env telegramChannelsTestEnv, credentialName string, model string) {
	t.Helper()

	credentialsRepo, err := data.NewLlmCredentialsRepository(env.pool)
	if err != nil {
		t.Fatalf("llm credentials repo: %v", err)
	}
	routesRepo, err := data.NewLlmRoutesRepository(env.pool)
	if err != nil {
		t.Fatalf("llm routes repo: %v", err)
	}

	credID := uuid.New()
	if _, err := credentialsRepo.Create(
		context.Background(),
		credID,
		"user",
		&env.userID,
		"openai",
		credentialName,
		nil,
		nil,
		nil,
		nil,
		map[string]any{},
	); err != nil {
		t.Fatalf("create llm credential: %v", err)
	}

	if _, err := routesRepo.Create(context.Background(), data.CreateLlmRouteParams{
		AccountID:    env.accountID,
		Scope:        data.LlmRouteScopeUser,
		CredentialID: credID,
		Model:        model,
		Priority:     100,
		ShowInPicker: true,
		WhenJSON:     json.RawMessage(`{}`),
		AdvancedJSON: map[string]any{},
		Multiplier:   1.0,
	}); err != nil {
		t.Fatalf("create llm route: %v", err)
	}
}

func createActiveTelegramChannelWithConfig(t *testing.T, env telegramChannelsTestEnv, botToken string, config map[string]any) data.Channel {
	t.Helper()
	channelID := uuid.New()
	secret, err := env.secretsRepo.Create(context.Background(), env.userID, data.ChannelSecretName(channelID), botToken)
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	webhookSecret := "telegram-secret"
	webhookURL := "https://app.example/v1/channels/telegram/" + channelID.String() + "/webhook"
	channel, err := env.channelsRepo.Create(
		context.Background(),
		channelID,
		env.accountID,
		"telegram",
		&env.personaID,
		&secret.ID,
		&env.userID,
		webhookSecret,
		webhookURL,
		configJSON,
	)
	if err != nil {
		t.Fatalf("create channel repo: %v", err)
	}
	active := true
	updated, err := env.channelsRepo.Update(context.Background(), channel.ID, env.accountID, data.ChannelUpdate{IsActive: &active})
	if err != nil {
		t.Fatalf("activate channel repo: %v", err)
	}
	if updated == nil {
		t.Fatal("channel activation returned nil")
	}
	return *updated
}

func authHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func derefString(t *testing.T, value *string) string {
	t.Helper()
	if value == nil {
		t.Fatal("expected non-nil string")
	}
	return *value
}

func mustUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	return id
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func assertCountAccount(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected count for %q: got %d want %d", query, got, want)
	}
}
