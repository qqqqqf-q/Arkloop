package http

import (
	"context"
	"io"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
)

func TestMessagesCreateListAndAudit(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_messages")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	if err := setupAuthSchema(ctx, pool); err != nil {
		t.Fatalf("setup auth schema: %v", err)
	}
	if err := setupThreadsSchema(ctx, pool); err != nil {
		t.Fatalf("setup threads schema: %v", err)
	}
	if err := setupMessagesSchema(ctx, pool); err != nil {
		t.Fatalf("setup messages schema: %v", err)
	}

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600)
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
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, passwordHasher, tokenService)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		MessageRepo:         messageRepo,
		AuditWriter:         auditWriter,
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwdpwdpwd", "display_name": "Alice"},
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
		map[string]any{"login": "bob", "password": "pwdpwdpwd", "display_name": "Bob"},
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

	deniedCreateCount, err := countDeniedAudit(ctx, pool, "messages.create", "org_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedCreateCount != 1 {
		t.Fatalf("unexpected denied create audit count: %d", deniedCreateCount)
	}

	deniedListCount, err := countDeniedAudit(ctx, pool, "messages.list", "org_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedListCount != 1 {
		t.Fatalf("unexpected denied list audit count: %d", deniedListCount)
	}
}

func setupMessagesSchema(ctx context.Context, db data.Querier) error {
	if ctx == nil {
		ctx = context.Background()
	}

	statements := []string{
		`CREATE TABLE messages (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   org_id UUID NOT NULL,
		   thread_id UUID NOT NULL,
		   created_by_user_id UUID NULL,
		   role TEXT NOT NULL,
		   content TEXT NOT NULL,
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   CONSTRAINT fk_messages_org_id_orgs FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
		   CONSTRAINT fk_messages_created_by_user_id_users FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL,
		   CONSTRAINT fk_messages_thread_org FOREIGN KEY (thread_id, org_id) REFERENCES threads(id, org_id) ON DELETE CASCADE
		 )`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
