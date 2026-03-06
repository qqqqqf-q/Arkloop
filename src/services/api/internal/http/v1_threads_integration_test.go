package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func TestThreadsCreateListGetPatchAndAudit(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
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
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
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

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
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
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		AuditWriter:         auditWriter,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwdpwdpwd", "email": "alice@test.com"},
		nil,
	)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "t"}, headers)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create thread status: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	if threadPayload.ID == "" || threadPayload.CreatedAt == "" || threadPayload.OrgID == "" {
		t.Fatalf("unexpected thread payload: %#v", threadPayload)
	}

	cursorIncomplete := doJSON(handler, nethttp.MethodGet, "/v1/threads?before_id="+threadPayload.ID, nil, headers)
	env := assertErrorEnvelopePayload(t, cursorIncomplete, nethttp.StatusUnprocessableEntity, "validation.error")
	details, ok := env.Details.(map[string]any)
	if !ok || details["reason"] != "cursor_incomplete" {
		t.Fatalf("unexpected cursor details: %#v", env.Details)
	}

	missing := doJSON(handler, nethttp.MethodGet, "/v1/threads/00000000-0000-0000-0000-000000000000", nil, headers)
	assertErrorEnvelope(t, missing, nethttp.StatusNotFound, "threads.not_found")

	otherRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "bob", "password": "pwdpwdpwd", "email": "bob@test.com"},
		nil,
	)
	if otherRegister.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", otherRegister.Code, otherRegister.Body.String())
	}
	bob := decodeJSONBody[registerResponse](t, otherRegister.Body.Bytes())

	forbidden := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+threadPayload.ID, nil, authHeader(bob.AccessToken))
	assertErrorEnvelope(t, forbidden, nethttp.StatusForbidden, "policy.denied")

	updated := doJSON(
		handler,
		nethttp.MethodPatch,
		"/v1/threads/"+threadPayload.ID,
		map[string]any{"title": "new"},
		headers,
	)
	if updated.Code != nethttp.StatusOK {
		t.Fatalf("unexpected patch status: %d body=%s", updated.Code, updated.Body.String())
	}
	updatedPayload := decodeJSONBody[threadResponse](t, updated.Body.Bytes())
	if updatedPayload.Title == nil || *updatedPayload.Title != "new" {
		t.Fatalf("unexpected patch payload: %#v", updatedPayload)
	}

	deniedCount, err := countDeniedAudit(ctx, pool, "threads.get", "org_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedCount != 1 {
		t.Fatalf("unexpected denied audit count: %d", deniedCount)
	}
}

func TestThreadListActiveRunID(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_active_run")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
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
	membershipRepo, _ := data.NewOrgMembershipRepository(pool)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(pool)
	auditRepo, _ := data.NewAuditLogRepository(pool)
	threadRepo, _ := data.NewThreadRepository(pool)
	runRepo, _ := data.NewRunEventRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                 pool,
		Logger:               logger,
		AuthService:          authService,
		RegistrationService:  registrationService,
		OrgMembershipRepo:    membershipRepo,
		ThreadRepo:           threadRepo,
		RunEventRepo:         runRepo,
		AuditWriter:          auditWriter,
		TrustIncomingTraceID: true,
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice2", "password": "pwdpwdpwd", "email": "alice2@test.com"},
		nil,
	)
	if aliceRegister.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", aliceRegister.Code, aliceRegister.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, aliceRegister.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	// thread without any run — active_run_id must be null
	idleThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "idle"}, headers)
	if idleThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create idle thread: %d %s", idleThreadResp.Code, idleThreadResp.Body.String())
	}
	idleThread := decodeJSONBody[threadResponse](t, idleThreadResp.Body.Bytes())

	// thread with a running run — active_run_id must be set
	activeThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "active"}, headers)
	if activeThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create active thread: %d %s", activeThreadResp.Code, activeThreadResp.Body.String())
	}
	activeThread := decodeJSONBody[threadResponse](t, activeThreadResp.Body.Bytes())

	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+activeThread.ID+"/runs", nil, headers)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("create run: %d %s", runResp.Code, runResp.Body.String())
	}
	runPayload := decodeJSONBody[createRunResponse](t, runResp.Body.Bytes())

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/threads?limit=50", nil, headers)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list threads: %d %s", listResp.Code, listResp.Body.String())
	}
	var listed []threadResponse
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	byID := make(map[string]threadResponse, len(listed))
	for _, tr := range listed {
		byID[tr.ID] = tr
	}

	if got, ok := byID[idleThread.ID]; !ok {
		t.Fatalf("idle thread missing from list")
	} else if got.ActiveRunID != nil {
		t.Fatalf("idle thread active_run_id should be null, got %q", *got.ActiveRunID)
	}

	if got, ok := byID[activeThread.ID]; !ok {
		t.Fatalf("active thread missing from list")
	} else if got.ActiveRunID == nil {
		t.Fatalf("active thread active_run_id should be set")
	} else if *got.ActiveRunID != runPayload.RunID {
		t.Fatalf("active_run_id mismatch: want %q got %q", runPayload.RunID, *got.ActiveRunID)
	}

	// mark run completed — active_run_id must become null
	if _, err := pool.Exec(ctx,
		`UPDATE runs SET status = 'completed' WHERE id = $1`,
		runPayload.RunID,
	); err != nil {
		t.Fatalf("update run status: %v", err)
	}

	listResp2 := doJSON(handler, nethttp.MethodGet, "/v1/threads?limit=50", nil, headers)
	if listResp2.Code != nethttp.StatusOK {
		t.Fatalf("list threads after completion: %d %s", listResp2.Code, listResp2.Body.String())
	}
	var listed2 []threadResponse
	if err := json.Unmarshal(listResp2.Body.Bytes(), &listed2); err != nil {
		t.Fatalf("decode list2: %v", err)
	}
	byID2 := make(map[string]threadResponse, len(listed2))
	for _, tr := range listed2 {
		byID2[tr.ID] = tr
	}
	if got, ok := byID2[activeThread.ID]; !ok {
		t.Fatalf("active thread missing from list2")
	} else if got.ActiveRunID != nil {
		t.Fatalf("completed thread active_run_id should be null, got %q", *got.ActiveRunID)
	}
}

func assertErrorEnvelopePayload(t *testing.T, recorder *httptest.ResponseRecorder, statusCode int, code string) ErrorEnvelope {
	t.Helper()

	if recorder.Code != statusCode {
		t.Fatalf("unexpected status: %d raw=%s", recorder.Code, recorder.Body.String())
	}
	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v raw=%s", err, recorder.Body.String())
	}
	if payload.Code != code {
		t.Fatalf("unexpected code: %q raw=%s", payload.Code, recorder.Body.String())
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}
	return payload
}

func countDeniedAudit(ctx context.Context, db data.Querier, action string, denyReason string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int
	err := db.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_logs
		 WHERE action = $1
		   AND metadata_json->>'result' = 'denied'
		   AND metadata_json->>'deny_reason' = $2`,
		action,
		denyReason,
	).Scan(&count)
	return count, err
}
