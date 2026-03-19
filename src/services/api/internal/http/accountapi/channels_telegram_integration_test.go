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
	personaID    uuid.UUID
	channelsRepo *data.ChannelsRepository
	secretsRepo  *data.SecretsRepository
	keyRing      *apiCrypto.KeyRing
}

func setupTelegramChannelsTestEnv(t *testing.T, botClient *telegrambot.Client) telegramChannelsTestEnv {
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
		"auto",
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
		AuthService:             authService,
		AccountMembershipRepo:   membershipRepo,
		ThreadRepo:              threadRepo,
		ProjectRepo:             projectRepo,
		APIKeysRepo:             nil,
		Pool:                    pool,
		AccountRepo:             accountRepo,
		SecretsRepo:             secretsRepo,
		ChannelsRepo:            channelsRepo,
		ChannelIdentitiesRepo:   channelIdentitiesRepo,
		ChannelBindCodesRepo:    channelBindCodesRepo,
		ChannelDMThreadsRepo:    channelDMThreadsRepo,
		ChannelGroupThreadsRepo: channelGroupThreadsRepo,
		ChannelReceiptsRepo:     channelReceiptsRepo,
		UsersRepo:               userRepo,
		MessageRepo:             messageRepo,
		RunEventRepo:            runEventRepo,
		JobRepo:                 jobRepo,
		CreditsRepo:             creditsRepo,
		PersonasRepo:            personasRepo,
		AppBaseURL:              "https://app.example",
		TelegramBotClient:       botClient,
	})

	return telegramChannelsTestEnv{
		handler:      mux,
		pool:         pool,
		accessToken:  accessToken,
		accountID:    account.ID,
		userID:       user.ID,
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
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs`, 1)

	var source string
	if err := env.pool.QueryRow(context.Background(), `SELECT u.source
		FROM users u
		JOIN channel_identities ci ON ci.user_id = u.id
		LIMIT 1`).Scan(&source); err != nil {
		t.Fatalf("query shadow source: %v", err)
	}
	if source != "channel_shadow" {
		t.Fatalf("unexpected shadow source: %s", source)
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
	if _, ok := jobPayload["channel_delivery"]; !ok {
		t.Fatalf("expected channel_delivery in payload: %#v", jobPayload)
	}
	if _, ok := jobPayload["model"]; ok {
		t.Fatalf("did not expect model in job payload: %#v", jobPayload)
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
	if !strings.Contains(content.Parts[0].Text, `platform-message-id: "7"`) {
		t.Fatalf("expected platform metadata in content_json, got %s", content.Parts[0].Text)
	}

	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode metadata_json: %v", err)
	}
	attachments, _ := metadata["media_attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("expected one media attachment in metadata, got %#v", metadata["media_attachments"])
	}
	if got := asString(metadata["platform_message_id"]); got != "7" {
		t.Fatalf("unexpected platform_message_id: %q", got)
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
