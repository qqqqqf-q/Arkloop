//go:build !desktop

package http

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"github.com/google/uuid"
)

func TestMessagesCreateListAndAudit(t *testing.T) {
	db := setupTestDatabase(t, "api_go_messages")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AccountMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		ProjectRepo:         projectRepo,
		MessageRepo:         messageRepo,
		AuditWriter:         auditWriter,
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwd12345", "email": "alice@test.com"},
		nil,
	)
	if aliceRegister.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", aliceRegister.Code, aliceRegister.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, aliceRegister.Body.Bytes())
	aliceHeaders := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "t"}, aliceHeaders)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create thread status: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())

	createResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/messages",
		map[string]any{"content": "hi"},
		aliceHeaders,
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create message status: %d body=%s", createResp.Code, createResp.Body.String())
	}
	messagePayload := decodeJSONBody[messageResponse](t, createResp.Body.Bytes())
	if messagePayload.ID == "" || messagePayload.ThreadID != threadPayload.ID {
		t.Fatalf("unexpected message payload: %#v", messagePayload)
	}
	if messagePayload.Role != "user" || messagePayload.Content != "hi" {
		t.Fatalf("unexpected message payload: %#v", messagePayload)
	}

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+threadPayload.ID+"/messages", nil, aliceHeaders)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected list messages status: %d body=%s", listResp.Code, listResp.Body.String())
	}
	listPayload := decodeJSONBody[[]messageResponse](t, listResp.Body.Bytes())
	if len(listPayload) != 1 || listPayload[0].ID != messagePayload.ID {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}

	bobRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "bob", "password": "pwd12345", "email": "bob@test.com"},
		nil,
	)
	if bobRegister.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", bobRegister.Code, bobRegister.Body.String())
	}
	bob := decodeJSONBody[registerResponse](t, bobRegister.Body.Bytes())
	bobHeaders := authHeader(bob.AccessToken)

	denyCreate := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/messages",
		map[string]any{"content": "nope"},
		bobHeaders,
	)
	assertErrorEnvelope(t, denyCreate, nethttp.StatusForbidden, "policy.denied")

	denyList := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+threadPayload.ID+"/messages", nil, bobHeaders)
	assertErrorEnvelope(t, denyList, nethttp.StatusForbidden, "policy.denied")

	deniedCreateCount, err := countDeniedAudit(ctx, pool, "messages.create", "account_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedCreateCount != 1 {
		t.Fatalf("unexpected denied create audit count: %d", deniedCreateCount)
	}

	deniedListCount, err := countDeniedAudit(ctx, pool, "messages.list", "account_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedListCount != 1 {
		t.Fatalf("unexpected denied list audit count: %d", deniedListCount)
	}
}

func TestMessagesListIncludesAssistantRunID(t *testing.T) {
	db := setupTestDatabase(t, "api_go_messages_run_id")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, _ := data.NewUserRepository(pool)
	credentialRepo, _ := data.NewUserCredentialRepository(pool)
	membershipRepo, _ := data.NewAccountMembershipRepository(pool)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(pool)
	auditRepo, _ := data.NewAuditLogRepository(pool)
	threadRepo, _ := data.NewThreadRepository(pool)
	projectRepo, _ := data.NewProjectRepository(pool)
	messageRepo, _ := data.NewMessageRepository(pool)
	jobRepo, _ := data.NewJobRepository(pool)
	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AccountMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		ProjectRepo:         projectRepo,
		MessageRepo:         messageRepo,
		AuditWriter:         auditWriter,
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice-run-id", "password": "pwd12345", "email": "alice-run-id@test.com"},
		nil,
	)
	if aliceRegister.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", aliceRegister.Code, aliceRegister.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, aliceRegister.Body.Bytes())
	aliceHeaders := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "t"}, aliceHeaders)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create thread status: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	threadID, err := uuid.Parse(threadPayload.ID)
	if err != nil {
		t.Fatalf("parse thread id: %v", err)
	}
	accountID, err := uuid.Parse(threadPayload.AccountID)
	if err != nil {
		t.Fatalf("parse org id: %v", err)
	}
	runID := uuid.New()
	metadataRaw, err := json.Marshal(map[string]any{"run_id": runID.String()})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	_, err = pool.Exec(
		ctx,
		`INSERT INTO messages (account_id, thread_id, created_by_user_id, role, content, metadata_json)
		 VALUES ($1, $2, NULL, 'assistant', $3, $4::jsonb)`,
		accountID,
		threadID,
		"hello from assistant",
		string(metadataRaw),
	)
	if err != nil {
		t.Fatalf("insert assistant: %v", err)
	}

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+threadPayload.ID+"/messages", nil, aliceHeaders)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected list messages status: %d body=%s", listResp.Code, listResp.Body.String())
	}
	listPayload := decodeJSONBody[[]messageResponse](t, listResp.Body.Bytes())
	if len(listPayload) != 1 {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}
	if listPayload[0].RunID == nil || *listPayload[0].RunID != runID.String() {
		t.Fatalf("unexpected assistant run_id: %#v", listPayload[0])
	}
}
