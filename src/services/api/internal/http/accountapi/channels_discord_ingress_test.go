//go:build !desktop

package accountapi

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
	"arkloop/services/shared/discordbot"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type discordChannelsTestEnv struct {
	handler                  nethttp.Handler
	pool                     *pgxpool.Pool
	accessToken              string
	accountID                uuid.UUID
	userID                   uuid.UUID
	personaID                uuid.UUID
	projectID                uuid.UUID
	channelsRepo             *data.ChannelsRepository
	channelIdentitiesRepo    *data.ChannelIdentitiesRepository
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	channelBindCodesRepo     *data.ChannelBindCodesRepository
	channelDMThreadsRepo     *data.ChannelDMThreadsRepository
	channelReceiptsRepo      *data.ChannelMessageReceiptsRepository
	channelLedgerRepo        *data.ChannelMessageLedgerRepository
	personasRepo             *data.PersonasRepository
	usersRepo                *data.UserRepository
	accountRepo              *data.AccountRepository
	threadRepo               *data.ThreadRepository
	messageRepo              *data.MessageRepository
	runEventRepo             *data.RunEventRepository
	jobRepo                  *data.JobRepository
	creditsRepo              *data.CreditsRepository
	secretsRepo              *data.SecretsRepository
}

func setupDiscordChannelsTestEnv(t *testing.T, botClient *discordbot.Client) discordChannelsTestEnv {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_channels_discord")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := data.NewPool(context.Background(), db.DSN, data.PoolLimits{MaxConns: 16, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	channelRunTriggerLog.Lock()
	channelRunTriggerByChannel = map[uuid.UUID][]time.Time{}
	channelRunTriggerLog.Unlock()

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	userCredRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("credential repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh repo: %v", err)
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
	channelLedgerRepo, err := data.NewChannelMessageLedgerRepository(pool)
	if err != nil {
		t.Fatalf("ledger repo: %v", err)
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

	account, err := accountRepo.Create(context.Background(), "discord-account", "Discord Account", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	user, err := userRepo.Create(context.Background(), "discord-owner", "discord-owner@test.com", "zh")
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
		"discord-persona",
		"1",
		"Discord Persona",
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
		json.RawMessage(`{}`),
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
		DiscordBotClient:         botClient,
	})

	return discordChannelsTestEnv{
		handler:                  mux,
		pool:                     pool,
		accessToken:              accessToken,
		accountID:                account.ID,
		userID:                   user.ID,
		personaID:                persona.ID,
		projectID:                project.ID,
		channelsRepo:             channelsRepo,
		channelIdentitiesRepo:    channelIdentitiesRepo,
		channelIdentityLinksRepo: channelIdentityLinksRepo,
		channelBindCodesRepo:     channelBindCodesRepo,
		channelDMThreadsRepo:     channelDMThreadsRepo,
		channelReceiptsRepo:      channelReceiptsRepo,
		channelLedgerRepo:        channelLedgerRepo,
		personasRepo:             personasRepo,
		usersRepo:                userRepo,
		accountRepo:              accountRepo,
		threadRepo:               threadRepo,
		messageRepo:              messageRepo,
		runEventRepo:             runEventRepo,
		jobRepo:                  jobRepo,
		creditsRepo:              creditsRepo,
		secretsRepo:              secretsRepo,
	}
}

func (e discordChannelsTestEnv) connector() discordConnector {
	return discordConnector{
		channelsRepo:             e.channelsRepo,
		channelIdentitiesRepo:    e.channelIdentitiesRepo,
		channelIdentityLinksRepo: e.channelIdentityLinksRepo,
		channelBindCodesRepo:     e.channelBindCodesRepo,
		channelDMThreadsRepo:     e.channelDMThreadsRepo,
		channelReceiptsRepo:      e.channelReceiptsRepo,
		channelLedgerRepo:        e.channelLedgerRepo,
		personasRepo:             e.personasRepo,
		usersRepo:                e.usersRepo,
		accountRepo:              e.accountRepo,
		threadRepo:               e.threadRepo,
		messageRepo:              e.messageRepo,
		runEventRepo:             e.runEventRepo,
		jobRepo:                  e.jobRepo,
		creditsRepo:              e.creditsRepo,
		pool:                     e.pool,
	}
}

func createActiveDiscordChannelWithConfig(t *testing.T, env discordChannelsTestEnv, botToken string, config map[string]any) data.Channel {
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
	channel, err := env.channelsRepo.Create(
		context.Background(),
		channelID,
		env.accountID,
		"discord",
		&env.personaID,
		&secret.ID,
		&env.userID,
		"",
		"",
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

func newDiscordMessageCreate(messageID, channelID, userID, username, content string) *discordgo.MessageCreate {
	return &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        messageID,
			ChannelID: channelID,
			Content:   content,
			Author:    &discordgo.User{ID: userID, Username: username},
			Timestamp: time.Now().UTC(),
		},
	}
}

func newDiscordInteractionCommand(name, guildID, channelID, userID, username, code string) *discordgo.InteractionCreate {
	data := discordgo.ApplicationCommandInteractionData{
		Name:        name,
		CommandType: discordgo.ChatApplicationCommand,
	}
	if code != "" {
		data.Options = []*discordgo.ApplicationCommandInteractionDataOption{{
			Name:  "code",
			Type:  discordgo.ApplicationCommandOptionString,
			Value: code,
		}}
	}
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:      discordgo.InteractionApplicationCommand,
			GuildID:   guildID,
			ChannelID: channelID,
			User:      &discordgo.User{ID: userID, Username: username},
			Data:      data,
		},
	}
}

func mustLinkDiscordIdentity(t *testing.T, env discordChannelsTestEnv, channelID uuid.UUID, userID, username string) data.ChannelIdentity {
	t.Helper()

	identity, err := upsertDiscordIdentity(context.Background(), env.channelIdentitiesRepo, &discordgo.User{ID: userID, Username: username})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	if _, err := env.channelIdentityLinksRepo.Upsert(context.Background(), channelID, identity.ID); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	return identity
}

func TestDiscordIngressDMFirstMessageEntersPendingBatch(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{
		"default_model": "openai^gpt-4.1-mini",
	})
	mustLinkDiscordIdentity(t, env, channel.ID, "u-1", "alice")

	err := env.connector().HandleMessageCreate(
		context.Background(),
		"trace-discord-first",
		channel.ID,
		"",
		newDiscordMessageCreate("m-1", "dm-1", "u-1", "alice", "hello"),
	)
	if err != nil {
		t.Fatalf("handle message create: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_identities`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_dm_threads`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND metadata_json->>'ingress_state' = '`+inboundStatePendingDispatch+`'`, 1)

	var dispatchAfter int64
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT COALESCE((metadata_json->>'dispatch_after_unix_ms')::bigint, 0)
		   FROM channel_message_ledger
		  WHERE channel_id = $1
		    AND direction = 'inbound'
		    AND platform_conversation_id = 'dm-1'
		    AND platform_message_id = 'm-1'`,
		channel.ID,
	).Scan(&dispatchAfter); err != nil {
		t.Fatalf("query dispatch_after_unix_ms: %v", err)
	}
	if dispatchAfter <= time.Now().UTC().UnixMilli() {
		t.Fatalf("expected dispatch_after_unix_ms in future, got %d", dispatchAfter)
	}
}

func TestDiscordIngressLocalizesInboundTimeForAgent(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	if _, err := env.pool.Exec(context.Background(), `UPDATE users SET timezone = 'Asia/Shanghai' WHERE id = $1`, env.userID); err != nil {
		t.Fatalf("seed user timezone: %v", err)
	}
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})
	mustLinkDiscordIdentity(t, env, channel.ID, "u-time", "alice")

	event := newDiscordMessageCreate("m-time", "dm-time", "u-time", "alice", "hello")
	event.Timestamp = time.Date(2024, time.March, 8, 16, 0, 0, 0, time.UTC)
	if err := env.connector().HandleMessageCreate(context.Background(), observability.NewTraceID(), channel.ID, "", event); err != nil {
		t.Fatalf("handle message create: %v", err)
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
		t.Fatalf("decode content_json: %v", err)
	}
	if len(content.Parts) != 1 || !strings.Contains(content.Parts[0].Text, `time: "2024-03-09 00:00:00 [UTC+8]"`) {
		t.Fatalf("unexpected localized content: %s", string(contentJSON))
	}
	if !strings.Contains(content.Parts[0].Text, `time_utc: "2024-03-08T16:00:00Z"`) {
		t.Fatalf("expected utc field in content, got %s", content.Parts[0].Text)
	}

	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode metadata_json: %v", err)
	}
	if got := asString(metadata["time_local"]); got != "2024-03-09 00:00:00 [UTC+8]" {
		t.Fatalf("unexpected time_local: %q", got)
	}
	if got := asString(metadata["time_utc"]); got != "2024-03-08T16:00:00Z" {
		t.Fatalf("unexpected time_utc: %q", got)
	}
}

func TestDiscordIngressLocalizesInboundTimeWithOwnerTimezoneDST(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	if _, err := env.pool.Exec(context.Background(), `UPDATE users SET timezone = 'America/Los_Angeles' WHERE id = $1`, env.userID); err != nil {
		t.Fatalf("seed user timezone: %v", err)
	}
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})
	_ = mustLinkDiscordIdentity(t, env, channel.ID, "u-owner", "owner-user")

	event := newDiscordMessageCreate("m-dst", "dm-dst", "u-owner", "owner-user", "hello")
	event.Timestamp = time.Date(2024, time.July, 4, 12, 0, 0, 0, time.UTC)
	if err := env.connector().HandleMessageCreate(context.Background(), observability.NewTraceID(), channel.ID, "", event); err != nil {
		t.Fatalf("handle message create: %v", err)
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
		t.Fatalf("decode content_json: %v", err)
	}
	if len(content.Parts) != 1 || !strings.Contains(content.Parts[0].Text, `time: "2024-07-04 05:00:00 [UTC-7]"`) {
		t.Fatalf("unexpected localized content: %s", string(contentJSON))
	}
	if !strings.Contains(content.Parts[0].Text, `time_utc: "2024-07-04T12:00:00Z"`) {
		t.Fatalf("expected utc field in content, got %s", content.Parts[0].Text)
	}

	var metadata map[string]any
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		t.Fatalf("decode metadata_json: %v", err)
	}
	if got := asString(metadata["time_local"]); got != "2024-07-04 05:00:00 [UTC-7]" {
		t.Fatalf("unexpected time_local: %q", got)
	}
	if got := asString(metadata["time_utc"]); got != "2024-07-04T12:00:00Z" {
		t.Fatalf("unexpected time_utc: %q", got)
	}
}

func TestDiscordIngressActiveRunKeepsPendingAndDoesNotInject(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})

	identity := mustLinkDiscordIdentity(t, env, channel.ID, "u-append", "append-user")
	var err error
	thread, err := env.threadRepo.Create(context.Background(), env.accountID, identity.UserID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := env.channelDMThreadsRepo.Create(context.Background(), channel.ID, identity.ID, env.personaID, thread.ID); err != nil {
		t.Fatalf("create dm thread binding: %v", err)
	}
	run, _, err := env.runEventRepo.CreateRunWithStartedEvent(
		context.Background(),
		env.accountID,
		thread.ID,
		identity.UserID,
		"run.started",
		map[string]any{"persona_id": "discord-persona@1"},
	)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	err = env.connector().HandleMessageCreate(
		context.Background(),
		"trace-discord-active",
		channel.ID,
		"",
		newDiscordMessageCreate("m-2", "dm-2", "u-append", "append-user", "follow-up"),
	)
	if err != nil {
		t.Fatalf("handle message create: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM run_events WHERE run_id = '`+run.ID.String()+`' AND type = 'run.input_provided'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND metadata_json->>'ingress_state' = '`+inboundStatePendingDispatch+`'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 0)
}

func TestDiscordIngressDuplicateActiveRunMessageDoesNotAppendInputTwice(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})

	identity := mustLinkDiscordIdentity(t, env, channel.ID, "u-repeat", "repeat-user")
	thread, err := env.threadRepo.Create(context.Background(), env.accountID, identity.UserID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := env.channelDMThreadsRepo.Create(context.Background(), channel.ID, identity.ID, env.personaID, thread.ID); err != nil {
		t.Fatalf("create dm thread binding: %v", err)
	}
	run, _, err := env.runEventRepo.CreateRunWithStartedEvent(
		context.Background(),
		env.accountID,
		thread.ID,
		identity.UserID,
		"run.started",
		map[string]any{"persona_id": "discord-persona@1"},
	)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	event := newDiscordMessageCreate("m-repeat", "dm-repeat", "u-repeat", "repeat-user", "follow-up")
	if err := env.connector().HandleMessageCreate(context.Background(), "trace-discord-repeat-1", channel.ID, "", event); err != nil {
		t.Fatalf("first handle message create: %v", err)
	}
	if err := env.connector().HandleMessageCreate(context.Background(), "trace-discord-repeat-2", channel.ID, "", event); err != nil {
		t.Fatalf("second handle message create: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND platform_conversation_id = 'dm-repeat' AND platform_message_id = 'm-repeat'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM run_events WHERE run_id = '`+run.ID.String()+`' AND type = 'run.input_provided'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM messages`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 0)
}

func TestDiscordIngressBurstRecoveryCreatesSingleRunForBatch(t *testing.T) {
	setChannelInboundBurstWindowForTest(t, 20*time.Millisecond)

	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})
	mustLinkDiscordIdentity(t, env, channel.ID, "u-recover", "recover-user")

	first := newDiscordMessageCreate("m-recover-1", "dm-recover", "u-recover", "recover-user", "hello from recovery 1")
	second := newDiscordMessageCreate("m-recover-2", "dm-recover", "u-recover", "recover-user", "hello from recovery 2")
	if err := env.connector().HandleMessageCreate(context.Background(), "trace-discord-burst-1", channel.ID, "", first); err != nil {
		t.Fatalf("handle first message: %v", err)
	}
	if err := env.connector().HandleMessageCreate(context.Background(), "trace-discord-burst-2", channel.ID, "", second); err != nil {
		t.Fatalf("handle second message: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND metadata_json->>'ingress_state' = '`+inboundStatePendingDispatch+`'`, 2)

	time.Sleep(30 * time.Millisecond)
	if err := env.connector().recoverPendingDiscordInboundDispatches(context.Background(), channel.ID); err != nil {
		t.Fatalf("recover pending discord dispatches: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND run_id IS NOT NULL`, 2)

	var startedJSON []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT data_json::text::jsonb FROM run_events WHERE type = 'run.started' LIMIT 1`).Scan(&startedJSON); err != nil {
		t.Fatalf("query run.started: %v", err)
	}
	var started map[string]any
	if err := json.Unmarshal(startedJSON, &started); err != nil {
		t.Fatalf("decode run.started: %v", err)
	}
	if got := strings.TrimSpace(asString(started["continuation_source"])); got != "none" {
		t.Fatalf("unexpected continuation_source: %q", got)
	}
	if got, ok := started["continuation_loop"].(bool); !ok || got {
		t.Fatalf("unexpected continuation_loop: %#v", started["continuation_loop"])
	}
	if got := strings.TrimSpace(asString(started["thread_tail_message_id"])); got == "" {
		t.Fatalf("expected thread_tail_message_id in run.started: %#v", started)
	}
	delivery, ok := started["channel_delivery"].(map[string]any)
	if !ok {
		t.Fatalf("expected channel_delivery in run.started: %#v", started)
	}
	conversationRef, _ := delivery["conversation_ref"].(map[string]any)
	if got := asString(conversationRef["target"]); got != "dm-recover" {
		t.Fatalf("unexpected run.started conversation_ref: %#v", delivery)
	}
}

func TestDiscordIngressDeferredDispatchRecoversAfterRateLimitClears(t *testing.T) {
	t.Setenv("ARKLOOP_CHANNEL_RATE_LIMIT_PER_MIN", "1")

	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})
	mustLinkDiscordIdentity(t, env, channel.ID, "u-throttle", "throttle-user")

	channelRunTriggerLog.Lock()
	channelRunTriggerByChannel[channel.ID] = []time.Time{time.Now()}
	channelRunTriggerLog.Unlock()

	event := newDiscordMessageCreate("m-throttle", "dm-throttle", "u-throttle", "throttle-user", "hello later")
	if err := env.connector().HandleMessageCreate(context.Background(), "trace-discord-throttle", channel.ID, "", event); err != nil {
		t.Fatalf("handle throttled discord message: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 0)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND metadata_json->>'ingress_state' = '`+inboundStatePendingDispatch+`'`, 1)

	channelRunTriggerLog.Lock()
	delete(channelRunTriggerByChannel, channel.ID)
	channelRunTriggerLog.Unlock()

	if err := env.connector().recoverPendingDiscordInboundDispatches(context.Background(), channel.ID); err != nil {
		t.Fatalf("recover deferred discord dispatch: %v", err)
	}

	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM runs`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM jobs WHERE job_type = '`+data.RunExecuteJobType+`'`, 1)
	assertCountAccount(t, env.pool, `SELECT COUNT(*) FROM channel_message_ledger WHERE channel_id = '`+channel.ID.String()+`' AND direction = 'inbound' AND run_id IS NOT NULL`, 1)
}

func TestDiscordInteractionBindConsumesCode(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})

	channelType := "discord"
	code, err := env.channelBindCodesRepo.Create(context.Background(), env.userID, &channelType, time.Hour)
	if err != nil {
		t.Fatalf("create bind code: %v", err)
	}

	reply, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-discord-bind",
		channel.ID,
		"",
		newDiscordInteractionCommand("bind", "", "dm-bind", "u-bind", "bind-user", code.Token),
	)
	if err != nil {
		t.Fatalf("handle interaction: %v", err)
	}
	if reply == nil || reply.Content != "绑定成功。" {
		t.Fatalf("unexpected bind reply: %#v", reply)
	}

	identity, err := env.channelIdentitiesRepo.GetByChannelAndSubject(context.Background(), "discord", "u-bind")
	if err != nil {
		t.Fatalf("get identity: %v", err)
	}
	if identity == nil || identity.UserID == nil || *identity.UserID != env.userID {
		t.Fatalf("identity not bound correctly: %#v", identity)
	}

	activeCode, err := env.channelBindCodesRepo.GetActiveByToken(context.Background(), code.Token)
	if err != nil {
		t.Fatalf("get bind code: %v", err)
	}
	if activeCode != nil {
		t.Fatalf("expected bind code consumed, got %#v", activeCode)
	}
}

func TestDiscordInteractionNewRemovesDMThreadBinding(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{})

	identity, err := upsertDiscordIdentity(context.Background(), env.channelIdentitiesRepo, &discordgo.User{ID: "u-new", Username: "new-user"})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	thread, err := env.threadRepo.Create(context.Background(), env.accountID, identity.UserID, env.projectID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := env.channelDMThreadsRepo.Create(context.Background(), channel.ID, identity.ID, env.personaID, thread.ID); err != nil {
		t.Fatalf("create dm thread binding: %v", err)
	}

	reply, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-discord-new",
		channel.ID,
		"",
		newDiscordInteractionCommand("new", "", "dm-new", "u-new", "new-user", ""),
	)
	if err != nil {
		t.Fatalf("handle interaction: %v", err)
	}
	if reply == nil || reply.Content != "已开启新会话。" {
		t.Fatalf("unexpected new reply: %#v", reply)
	}

	binding, err := env.channelDMThreadsRepo.GetByBinding(context.Background(), channel.ID, identity.ID, env.personaID)
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if binding != nil {
		t.Fatalf("expected binding removed, got %#v", binding)
	}
}

func TestDiscordInteractionGuildAllowlistRejectsCommand(t *testing.T) {
	env := setupDiscordChannelsTestEnv(t, nil)
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_server_ids":  []string{"guild-allow"},
		"allowed_channel_ids": []string{"channel-allow"},
	})

	reply, err := env.connector().HandleInteraction(
		context.Background(),
		"trace-discord-allowlist",
		channel.ID,
		"",
		newDiscordInteractionCommand("help", "guild-deny", "channel-deny", "u-guild", "guild-user", ""),
	)
	if err != nil {
		t.Fatalf("handle interaction: %v", err)
	}
	if reply == nil || reply.Content != "当前服务器或频道未被授权。" || !reply.Ephemeral {
		t.Fatalf("unexpected allowlist reply: %#v", reply)
	}

	identity, err := env.channelIdentitiesRepo.GetByChannelAndSubject(context.Background(), "discord", "u-guild")
	if err != nil {
		t.Fatalf("get identity: %v", err)
	}
	if identity != nil {
		t.Fatalf("expected no identity for denied command, got %#v", identity)
	}
}

func TestVerifyDiscordChannelBackfillsMetadata(t *testing.T) {
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.URL.Path {
		case "/users/@me":
			_, _ = w.Write([]byte(`{"id":"bot-user-1","username":"alma-bot","bot":true}`))
		case "/applications/@me":
			_, _ = w.Write([]byte(`{"id":"app-1","name":"Alma Discord"}`))
		default:
			nethttp.NotFound(w, r)
		}
	}))
	defer server.Close()

	env := setupDiscordChannelsTestEnv(t, discordbot.NewClient(server.URL, server.Client()))
	channel := createActiveDiscordChannelWithConfig(t, env, "bot-token", map[string]any{
		"allowed_server_ids":  []string{"guild-1"},
		"allowed_channel_ids": []string{"channel-1"},
	})

	resp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels/"+channel.ID.String()+"/verify",
		nil,
		authHeader(env.accessToken),
	)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("verify channel: %d %s", resp.Code, resp.Body.String())
	}

	var body channelVerifyResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	if !body.OK {
		t.Fatalf("expected verify ok, got %#v", body)
	}
	if body.BotUsername != "alma-bot" || body.BotUserID != "bot-user-1" {
		t.Fatalf("unexpected bot metadata: %#v", body)
	}
	if body.ApplicationID != "app-1" || body.ApplicationName != "Alma Discord" {
		t.Fatalf("unexpected app metadata: %#v", body)
	}

	updated, err := env.channelsRepo.GetByID(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get updated channel: %v", err)
	}
	if updated == nil {
		t.Fatal("updated channel missing")
	}
	cfg, err := resolveDiscordConfig(updated.ChannelType, updated.ConfigJSON)
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if cfg.DiscordApplicationID != "app-1" || cfg.DiscordBotUserID != "bot-user-1" {
		t.Fatalf("verify metadata not backfilled: %#v", cfg)
	}
	if len(cfg.AllowedServerIDs) != 1 || cfg.AllowedServerIDs[0] != "guild-1" {
		t.Fatalf("allowed_server_ids changed unexpectedly: %#v", cfg.AllowedServerIDs)
	}
	if len(cfg.AllowedChannelIDs) != 1 || cfg.AllowedChannelIDs[0] != "channel-1" {
		t.Fatalf("allowed_channel_ids changed unexpectedly: %#v", cfg.AllowedChannelIDs)
	}
}
