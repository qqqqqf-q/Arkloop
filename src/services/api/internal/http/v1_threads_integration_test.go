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

	"github.com/google/uuid"
)

func TestThreadsCreateListGetPatchAndAudit(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads")

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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
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
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		ProjectRepo:         projectRepo,
		AuditWriter:         auditWriter,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwd12345", "email": "alice@test.com"},
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
	if threadPayload.Mode != string(data.ThreadModeChat) {
		t.Fatalf("expected default thread mode %q, got %#v", data.ThreadModeChat, threadPayload)
	}
	if threadPayload.ProjectID == nil || *threadPayload.ProjectID == "" {
		t.Fatalf("expected project_id in thread payload: %#v", threadPayload)
	}

	getResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+threadPayload.ID, nil, headers)
	if getResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected get thread status: %d body=%s", getResp.Code, getResp.Body.String())
	}
	getPayload := decodeJSONBody[threadResponse](t, getResp.Body.Bytes())
	if getPayload.Mode != string(data.ThreadModeChat) {
		t.Fatalf("unexpected get payload: %#v", getPayload)
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
		map[string]any{"login": "bob", "password": "pwd12345", "email": "bob@test.com"},
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
	if updatedPayload.Mode != string(data.ThreadModeChat) {
		t.Fatalf("expected patched thread mode %q, got %#v", data.ThreadModeChat, updatedPayload)
	}

	deniedCount, err := countDeniedAudit(ctx, pool, "threads.get", "org_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedCount != 1 {
		t.Fatalf("unexpected denied audit count: %d", deniedCount)
	}
}

func TestThreadsPatchDeleteOwnershipFallbacks(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_patch_delete")

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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
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
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		ProjectRepo:         projectRepo,
		AuditWriter:         auditWriter,
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice-fast", "password": "pwd12345", "email": "alice-fast@test.com"},
		nil,
	)
	if aliceRegister.Code != nethttp.StatusCreated {
		t.Fatalf("register alice: %d body=%s", aliceRegister.Code, aliceRegister.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, aliceRegister.Body.Bytes())
	aliceHeaders := authHeader(alice.AccessToken)

	bobRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "bob-fast", "password": "pwd12345", "email": "bob-fast@test.com"},
		nil,
	)
	if bobRegister.Code != nethttp.StatusCreated {
		t.Fatalf("register bob: %d body=%s", bobRegister.Code, bobRegister.Body.String())
	}
	bob := decodeJSONBody[registerResponse](t, bobRegister.Body.Bytes())
	bobHeaders := authHeader(bob.AccessToken)

	patchThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "patch-me"}, aliceHeaders)
	if patchThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create patch thread: %d body=%s", patchThreadResp.Code, patchThreadResp.Body.String())
	}
	patchThread := decodeJSONBody[threadResponse](t, patchThreadResp.Body.Bytes())
	aliceOrgID, err := uuid.Parse(patchThread.OrgID)
	if err != nil {
		t.Fatalf("parse org id: %v", err)
	}

	t.Run("owner patch title locks thread", func(t *testing.T) {
		resp := doJSON(
			handler,
			nethttp.MethodPatch,
			"/v1/threads/"+patchThread.ID,
			map[string]any{"title": "patch-fast"},
			aliceHeaders,
		)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("patch thread: %d body=%s", resp.Code, resp.Body.String())
		}
		payload := decodeJSONBody[threadResponse](t, resp.Body.Bytes())
		if payload.Title == nil || *payload.Title != "patch-fast" {
			t.Fatalf("unexpected patch response: %#v", payload)
		}
		threadID, err := uuid.Parse(patchThread.ID)
		if err != nil {
			t.Fatalf("parse thread id: %v", err)
		}
		stored, err := threadRepo.GetByID(ctx, threadID)
		if err != nil {
			t.Fatalf("get thread after patch: %v", err)
		}
		if stored == nil || stored.Title == nil || *stored.Title != "patch-fast" || !stored.TitleLocked {
			t.Fatalf("unexpected stored thread after patch: %#v", stored)
		}
	})

	t.Run("non owner patch denied", func(t *testing.T) {
		resp := doJSON(
			handler,
			nethttp.MethodPatch,
			"/v1/threads/"+patchThread.ID,
			map[string]any{"title": "bob-update"},
			bobHeaders,
		)
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
		count, err := countDeniedAudit(ctx, pool, "threads.update", "org_mismatch")
		if err != nil {
			t.Fatalf("count patch denied audit: %v", err)
		}
		if count != 1 {
			t.Fatalf("unexpected patch denied audit count: %d", count)
		}
	})

	t.Run("missing patch stays 404", func(t *testing.T) {
		resp := doJSON(
			handler,
			nethttp.MethodPatch,
			"/v1/threads/"+uuid.NewString(),
			map[string]any{"title": "missing"},
			aliceHeaders,
		)
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "threads.not_found")
	})

	noOwnerTitle := "no-owner"
	noOwnerPatchProject := mustCreateTestProject(t, ctx, pool, aliceOrgID, nil, "no-owner-patch")
	noOwnerPatchThread, err := threadRepo.Create(ctx, aliceOrgID, nil, noOwnerPatchProject.ID, data.ThreadModeChat, &noOwnerTitle, false)
	if err != nil {
		t.Fatalf("create no-owner patch thread: %v", err)
	}

	t.Run("patch no owner keeps denied semantics", func(t *testing.T) {
		resp := doJSON(
			handler,
			nethttp.MethodPatch,
			"/v1/threads/"+noOwnerPatchThread.ID.String(),
			map[string]any{"title": "still-denied"},
			aliceHeaders,
		)
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
		count, err := countDeniedAudit(ctx, pool, "threads.update", "no_owner")
		if err != nil {
			t.Fatalf("count patch no-owner denied audit: %v", err)
		}
		if count != 1 {
			t.Fatalf("unexpected patch no-owner denied audit count: %d", count)
		}
	})

	deleteThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "delete-me"}, aliceHeaders)
	if deleteThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create delete thread: %d body=%s", deleteThreadResp.Code, deleteThreadResp.Body.String())
	}
	deleteThread := decodeJSONBody[threadResponse](t, deleteThreadResp.Body.Bytes())

	t.Run("non owner delete denied", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/threads/"+deleteThread.ID, nil, bobHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
		count, err := countDeniedAudit(ctx, pool, "threads.delete", "org_mismatch")
		if err != nil {
			t.Fatalf("count delete denied audit: %v", err)
		}
		if count != 1 {
			t.Fatalf("unexpected delete denied audit count: %d", count)
		}
	})

	t.Run("owner delete writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/threads/"+deleteThread.ID, nil, aliceHeaders)
		if resp.Code != nethttp.StatusNoContent {
			t.Fatalf("delete thread: %d body=%s", resp.Code, resp.Body.String())
		}
		threadID, err := uuid.Parse(deleteThread.ID)
		if err != nil {
			t.Fatalf("parse delete thread id: %v", err)
		}
		stored, err := threadRepo.GetByID(ctx, threadID)
		if err != nil {
			t.Fatalf("get deleted thread: %v", err)
		}
		if stored != nil {
			t.Fatalf("expected deleted thread to be hidden, got %#v", stored)
		}
		count, err := countAuditResult(ctx, pool, "threads.delete", "deleted", deleteThread.ID)
		if err != nil {
			t.Fatalf("count delete success audit: %v", err)
		}
		if count != 1 {
			t.Fatalf("unexpected delete success audit count: %d", count)
		}
	})

	t.Run("missing delete stays 404", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/threads/"+uuid.NewString(), nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "threads.not_found")
	})

	noOwnerDeleteTitle := "no-owner-delete"
	noOwnerDeleteProject := mustCreateTestProject(t, ctx, pool, aliceOrgID, nil, "no-owner-delete")
	noOwnerDeleteThread, err := threadRepo.Create(ctx, aliceOrgID, nil, noOwnerDeleteProject.ID, data.ThreadModeChat, &noOwnerDeleteTitle, false)
	if err != nil {
		t.Fatalf("create no-owner delete thread: %v", err)
	}

	t.Run("delete no owner keeps denied semantics", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/threads/"+noOwnerDeleteThread.ID.String(), nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
		count, err := countDeniedAudit(ctx, pool, "threads.delete", "no_owner")
		if err != nil {
			t.Fatalf("count delete no-owner denied audit: %v", err)
		}
		if count != 1 {
			t.Fatalf("unexpected delete no-owner denied audit count: %d", count)
		}
	})
}

func TestThreadListActiveRunID(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_active_run")

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
	membershipRepo, _ := data.NewOrgMembershipRepository(pool)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(pool)
	auditRepo, _ := data.NewAuditLogRepository(pool)
	threadRepo, _ := data.NewThreadRepository(pool)
	projectRepo, _ := data.NewProjectRepository(pool)
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
		ProjectRepo:          projectRepo,
		RunEventRepo:         runRepo,
		AuditWriter:          auditWriter,
		TrustIncomingTraceID: true,
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice2", "password": "pwd12345", "email": "alice2@test.com"},
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

func countAuditResult(ctx context.Context, db data.Querier, action string, result string, targetID string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int
	err := db.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_logs
		 WHERE action = $1
		   AND metadata_json->>'result' = $2
		   AND target_id = $3`,
		action,
		result,
		targetID,
	).Scan(&count)
	return count, err
}

func TestThreadModeCreateListSearchAndFork(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_mode")

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
	membershipRepo, _ := data.NewOrgMembershipRepository(pool)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(pool)
	auditRepo, _ := data.NewAuditLogRepository(pool)
	threadRepo, _ := data.NewThreadRepository(pool)
	projectRepo, _ := data.NewProjectRepository(pool)
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
		ProjectRepo:          projectRepo,
		RunEventRepo:         runRepo,
		AuditWriter:          auditWriter,
		TrustIncomingTraceID: true,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "mode-alice", "password": "pwd12345", "email": "mode-alice@test.com"},
		nil,
	)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	defaultThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "chat-default"}, headers)
	if defaultThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected default create status: %d body=%s", defaultThreadResp.Code, defaultThreadResp.Body.String())
	}
	defaultThread := decodeJSONBody[threadResponse](t, defaultThreadResp.Body.Bytes())
	if defaultThread.Mode != string(data.ThreadModeChat) {
		t.Fatalf("expected default mode %q, got %#v", data.ThreadModeChat, defaultThread)
	}

	clawThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "claw-thread", "mode": "claw"}, headers)
	if clawThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected claw create status: %d body=%s", clawThreadResp.Code, clawThreadResp.Body.String())
	}
	clawThread := decodeJSONBody[threadResponse](t, clawThreadResp.Body.Bytes())
	if clawThread.Mode != string(data.ThreadModeClaw) {
		t.Fatalf("expected claw mode %q, got %#v", data.ThreadModeClaw, clawThread)
	}

	invalidCreateResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "bad", "mode": "weird"}, headers)
	assertErrorEnvelope(t, invalidCreateResp, nethttp.StatusUnprocessableEntity, "validation.error")

	for _, tc := range []struct {
		threadID string
		content  string
	}{
		{threadID: defaultThread.ID, content: "shared-mode-query"},
		{threadID: clawThread.ID, content: "shared-mode-query"},
	} {
		resp := doJSON(
			handler,
			nethttp.MethodPost,
			"/v1/threads/"+tc.threadID+"/messages",
			map[string]any{"content": tc.content},
			headers,
		)
		if resp.Code != nethttp.StatusCreated {
			t.Fatalf("create message for %s: %d %s", tc.threadID, resp.Code, resp.Body.String())
		}
	}

	getClawResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+clawThread.ID, nil, headers)
	if getClawResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected claw get status: %d body=%s", getClawResp.Code, getClawResp.Body.String())
	}
	if payload := decodeJSONBody[threadResponse](t, getClawResp.Body.Bytes()); payload.Mode != string(data.ThreadModeClaw) {
		t.Fatalf("unexpected claw get payload: %#v", payload)
	}

	listChatResp := doJSON(handler, nethttp.MethodGet, "/v1/threads?mode=chat", nil, headers)
	if listChatResp.Code != nethttp.StatusOK {
		t.Fatalf("list chat threads: %d %s", listChatResp.Code, listChatResp.Body.String())
	}
	chatThreads := decodeJSONBody[[]threadResponse](t, listChatResp.Body.Bytes())
	if len(chatThreads) != 1 || chatThreads[0].ID != defaultThread.ID || chatThreads[0].Mode != string(data.ThreadModeChat) {
		t.Fatalf("unexpected chat thread list: %#v", chatThreads)
	}

	listClawResp := doJSON(handler, nethttp.MethodGet, "/v1/threads?mode=claw", nil, headers)
	if listClawResp.Code != nethttp.StatusOK {
		t.Fatalf("list claw threads: %d %s", listClawResp.Code, listClawResp.Body.String())
	}
	clawThreads := decodeJSONBody[[]threadResponse](t, listClawResp.Body.Bytes())
	if len(clawThreads) != 1 || clawThreads[0].ID != clawThread.ID || clawThreads[0].Mode != string(data.ThreadModeClaw) {
		t.Fatalf("unexpected claw thread list: %#v", clawThreads)
	}

	listAllResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, headers)
	if listAllResp.Code != nethttp.StatusOK {
		t.Fatalf("list all threads: %d %s", listAllResp.Code, listAllResp.Body.String())
	}
	allThreads := decodeJSONBody[[]threadResponse](t, listAllResp.Body.Bytes())
	if len(allThreads) != 2 {
		t.Fatalf("expected 2 threads without mode filter, got %#v", allThreads)
	}

	invalidListResp := doJSON(handler, nethttp.MethodGet, "/v1/threads?mode=weird", nil, headers)
	assertErrorEnvelope(t, invalidListResp, nethttp.StatusUnprocessableEntity, "validation.error")

	searchChatResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/search?q=shared-mode-query&mode=chat", nil, headers)
	if searchChatResp.Code != nethttp.StatusOK {
		t.Fatalf("search chat threads: %d %s", searchChatResp.Code, searchChatResp.Body.String())
	}
	searchChat := decodeJSONBody[[]threadResponse](t, searchChatResp.Body.Bytes())
	if len(searchChat) != 1 || searchChat[0].ID != defaultThread.ID || searchChat[0].Mode != string(data.ThreadModeChat) {
		t.Fatalf("unexpected chat search results: %#v", searchChat)
	}

	searchClawResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/search?q=shared-mode-query&mode=claw", nil, headers)
	if searchClawResp.Code != nethttp.StatusOK {
		t.Fatalf("search claw threads: %d %s", searchClawResp.Code, searchClawResp.Body.String())
	}
	searchClaw := decodeJSONBody[[]threadResponse](t, searchClawResp.Body.Bytes())
	if len(searchClaw) != 1 || searchClaw[0].ID != clawThread.ID || searchClaw[0].Mode != string(data.ThreadModeClaw) {
		t.Fatalf("unexpected claw search results: %#v", searchClaw)
	}

	invalidSearchResp := doJSON(handler, nethttp.MethodGet, "/v1/threads/search?q=shared-mode-query&mode=weird", nil, headers)
	assertErrorEnvelope(t, invalidSearchResp, nethttp.StatusUnprocessableEntity, "validation.error")

	forkMessageResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+clawThread.ID+"/messages",
		map[string]any{"content": "fork source"},
		headers,
	)
	if forkMessageResp.Code != nethttp.StatusCreated {
		t.Fatalf("create fork source message: %d %s", forkMessageResp.Code, forkMessageResp.Body.String())
	}
	forkSourceMessage := decodeJSONBody[messageResponse](t, forkMessageResp.Body.Bytes())

	forkResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+clawThread.ID+":fork",
		map[string]any{"message_id": forkSourceMessage.ID},
		headers,
	)
	if forkResp.Code != nethttp.StatusCreated {
		t.Fatalf("fork thread: %d %s", forkResp.Code, forkResp.Body.String())
	}
	forked := decodeJSONBody[forkThreadResponse](t, forkResp.Body.Bytes())
	if forked.Mode != string(data.ThreadModeClaw) {
		t.Fatalf("expected forked thread to inherit claw mode, got %#v", forked)
	}
}
