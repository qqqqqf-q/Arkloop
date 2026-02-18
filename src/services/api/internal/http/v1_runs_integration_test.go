package http

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestRunsCreateListGetCancelAndEnqueue(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_runs")

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
	if err := setupRunsSchema(ctx, pool); err != nil {
		t.Fatalf("setup runs schema: %v", err)
	}
	if err := setupJobsSchema(ctx, pool); err != nil {
		t.Fatalf("setup jobs schema: %v", err)
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
	runRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
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

	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+"/runs", nil, aliceHeaders)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run status: %d body=%s", runResp.Code, runResp.Body.String())
	}
	runHeaderTrace := runResp.Header().Get(observability.TraceIDHeader)
	runPayload := decodeJSONBody[createRunResponse](t, runResp.Body.Bytes())
	if runPayload.RunID == "" || runPayload.TraceID == "" {
		t.Fatalf("unexpected create run payload: %#v", runPayload)
	}
	if runPayload.TraceID != runHeaderTrace {
		t.Fatalf("trace mismatch: header=%q body=%q", runHeaderTrace, runPayload.TraceID)
	}

	createdRunID := uuid.MustParse(runPayload.RunID)

	var startedCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM run_events WHERE run_id = $1 AND type = 'run.started'`,
		createdRunID,
	).Scan(&startedCount); err != nil {
		t.Fatalf("count started: %v", err)
	}
	if startedCount != 1 {
		t.Fatalf("unexpected started count: %d", startedCount)
	}

	var (
		startedSeq  int64
		startedJSON []byte
	)
	if err := pool.QueryRow(ctx,
		`SELECT seq, data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		createdRunID,
	).Scan(&startedSeq, &startedJSON); err != nil {
		t.Fatalf("load started event: %v", err)
	}
	if startedSeq != 1 {
		t.Fatalf("unexpected started seq: %d", startedSeq)
	}
	var startedData map[string]any
	if err := json.Unmarshal(startedJSON, &startedData); err != nil {
		t.Fatalf("decode started json: %v raw=%s", err, string(startedJSON))
	}
	if len(startedData) != 0 {
		t.Fatalf("unexpected started data: %#v", startedData)
	}

	var (
		jobID      uuid.UUID
		jobType    string
		jobPayload []byte
	)
	if err := pool.QueryRow(ctx, `SELECT id, job_type, payload_json FROM jobs LIMIT 1`).Scan(&jobID, &jobType, &jobPayload); err != nil {
		t.Fatalf("load job: %v", err)
	}
	if jobType != data.RunExecuteJobType {
		t.Fatalf("unexpected job_type: %q", jobType)
	}
	var jobJSON map[string]any
	if err := json.Unmarshal(jobPayload, &jobJSON); err != nil {
		t.Fatalf("decode job json: %v raw=%s", err, string(jobPayload))
	}
	if jobJSON["job_id"] != jobID.String() {
		t.Fatalf("unexpected payload job_id: %#v", jobJSON["job_id"])
	}
	if jobJSON["type"] != data.RunExecuteJobType {
		t.Fatalf("unexpected payload type: %#v", jobJSON["type"])
	}
	if jobJSON["trace_id"] != runPayload.TraceID {
		t.Fatalf("unexpected payload trace_id: %#v", jobJSON["trace_id"])
	}
	if jobJSON["org_id"] != threadPayload.OrgID {
		t.Fatalf("unexpected payload org_id: %#v", jobJSON["org_id"])
	}
	if jobJSON["run_id"] != runPayload.RunID {
		t.Fatalf("unexpected payload run_id: %#v", jobJSON["run_id"])
	}
	payloadObj, ok := jobJSON["payload"].(map[string]any)
	if !ok || payloadObj["source"] != "api" {
		t.Fatalf("unexpected payload: %#v", jobJSON["payload"])
	}

	listRuns := doJSON(handler, nethttp.MethodGet, "/v1/threads/"+threadPayload.ID+"/runs", nil, aliceHeaders)
	if listRuns.Code != nethttp.StatusOK {
		t.Fatalf("unexpected list runs status: %d body=%s", listRuns.Code, listRuns.Body.String())
	}
	listPayload := decodeJSONBody[[]threadRunResponse](t, listRuns.Body.Bytes())
	if len(listPayload) != 1 || listPayload[0].RunID != runPayload.RunID || listPayload[0].Status != "running" {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}

	getRunResp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID, nil, aliceHeaders)
	if getRunResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected get run status: %d body=%s", getRunResp.Code, getRunResp.Body.String())
	}
	getRunPayload := decodeJSONBody[runResponse](t, getRunResp.Body.Bytes())
	if getRunPayload.RunID != runPayload.RunID || getRunPayload.ThreadID != threadPayload.ID || getRunPayload.OrgID != threadPayload.OrgID {
		t.Fatalf("unexpected get run payload: %#v", getRunPayload)
	}
	if getRunPayload.CreatedByUserID == nil || *getRunPayload.CreatedByUserID != alice.UserID {
		t.Fatalf("unexpected created_by_user_id: %#v", getRunPayload.CreatedByUserID)
	}
	if getRunPayload.TraceID == "" {
		t.Fatalf("missing trace_id in payload: %#v", getRunPayload)
	}

	cancelResp := doJSON(handler, nethttp.MethodPost, "/v1/runs/"+runPayload.RunID+":cancel", nil, aliceHeaders)
	if cancelResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected cancel status: %d body=%s", cancelResp.Code, cancelResp.Body.String())
	}
	cancelPayload := decodeJSONBody[cancelRunResponse](t, cancelResp.Body.Bytes())
	if !cancelPayload.OK {
		t.Fatalf("unexpected cancel payload: %#v", cancelPayload)
	}

	cancelAgain := doJSON(handler, nethttp.MethodPost, "/v1/runs/"+runPayload.RunID+":cancel", nil, aliceHeaders)
	if cancelAgain.Code != nethttp.StatusOK {
		t.Fatalf("unexpected cancel again status: %d body=%s", cancelAgain.Code, cancelAgain.Body.String())
	}

	var cancelRequestedCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM run_events WHERE run_id = $1 AND type = 'run.cancel_requested'`,
		createdRunID,
	).Scan(&cancelRequestedCount); err != nil {
		t.Fatalf("count cancel_requested: %v", err)
	}
	if cancelRequestedCount != 1 {
		t.Fatalf("unexpected cancel_requested count: %d", cancelRequestedCount)
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

	denyGet := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID, nil, authHeader(bob.AccessToken))
	assertErrorEnvelope(t, denyGet, nethttp.StatusForbidden, "policy.denied")

	deniedCount, err := countDeniedAudit(ctx, pool, "runs.get", "org_mismatch")
	if err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedCount != 1 {
		t.Fatalf("unexpected denied audit count: %d", deniedCount)
	}
}

func setupRunsSchema(ctx context.Context, db data.Querier) error {
	if ctx == nil {
		ctx = context.Background()
	}

	statements := []string{
		`CREATE TABLE runs (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   org_id UUID NOT NULL,
		   thread_id UUID NOT NULL,
		   created_by_user_id UUID NULL,
		   status TEXT NOT NULL DEFAULT 'running',
		   next_event_seq BIGINT NOT NULL DEFAULT 1,
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   CONSTRAINT fk_runs_org_id_orgs FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
		   CONSTRAINT fk_runs_thread_id_threads FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
		   CONSTRAINT fk_runs_created_by_user_id_users FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL
		 )`,
		`CREATE TABLE run_events (
		   event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   run_id UUID NOT NULL,
		   seq BIGINT NOT NULL,
		   ts TIMESTAMPTZ NOT NULL DEFAULT now(),
		   type TEXT NOT NULL,
		   data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		   tool_name TEXT NULL,
		   error_class TEXT NULL,
		   CONSTRAINT fk_run_events_run_id_runs FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE,
		   CONSTRAINT uq_run_events_run_id_seq UNIQUE (run_id, seq)
		 )`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func setupJobsSchema(ctx context.Context, db data.Querier) error {
	if ctx == nil {
		ctx = context.Background()
	}

	statements := []string{
		`CREATE TABLE jobs (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   job_type TEXT NOT NULL,
		   payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		   status TEXT NOT NULL DEFAULT 'queued',
		   available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   leased_until TIMESTAMPTZ NULL,
		   lease_token UUID NULL,
		   attempts INTEGER NOT NULL DEFAULT 0,
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		 )`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func TestStreamRunEvents(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_sse")

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
	if err := setupRunsSchema(ctx, pool); err != nil {
		t.Fatalf("setup runs schema: %v", err)
	}
	if err := setupJobsSchema(ctx, pool); err != nil {
		t.Fatalf("setup jobs schema: %v", err)
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
	runRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
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
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		RunEventRepo:        runRepo,
		AuditWriter:         auditWriter,
		SSEConfig: SSEConfig{
			PollSeconds:      0.01,
			HeartbeatSeconds: 60.0,
			BatchLimit:       100,
		},
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice_sse", "password": "pwdpwdpwd", "display_name": "Alice"},
		nil,
	)
	if aliceRegister.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d body=%s", aliceRegister.Code, aliceRegister.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, aliceRegister.Body.Bytes())
	aliceHeaders := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "sse-test"}, aliceHeaders)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())

	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+"/runs", nil, aliceHeaders)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("create run: %d body=%s", runResp.Code, runResp.Body.String())
	}
	runPayload := decodeJSONBody[createRunResponse](t, runResp.Body.Bytes())

	t.Run("未认证返回401", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false", nil, nil)
		assertErrorEnvelope(t, resp, nethttp.StatusUnauthorized, "auth.missing_token")
	})

	t.Run("run不存在返回404", func(t *testing.T) {
		fakeID := "00000000-0000-0000-0000-000000000099"
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+fakeID+"/events?follow=false", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "runs.not_found")
	})

	t.Run("after_seq负数返回422", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false&after_seq=-1", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation_error")
	})

	t.Run("follow=false拉取已有事件并校验SSE格式", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		if ct := resp.Header().Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("unexpected Content-Type: %q", ct)
		}
		if cc := resp.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("unexpected Cache-Control: %q", cc)
		}

		events := parseSseEvents(t, resp.Body.String())
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d body=%s", len(events), resp.Body.String())
		}
		ev := events[0]
		if ev["type"] != "run.started" {
			t.Fatalf("unexpected type: %q", ev["type"])
		}
		if ev["run_id"] != runPayload.RunID {
			t.Fatalf("unexpected run_id: %q", ev["run_id"])
		}
		seqRaw, ok := ev["seq"].(float64)
		if !ok || seqRaw != 1 {
			t.Fatalf("unexpected seq: %v", ev["seq"])
		}
	})

	t.Run("after_seq=1跳过已有事件返回空", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false&after_seq=1", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		events := parseSseEvents(t, resp.Body.String())
		if len(events) != 0 {
			t.Fatalf("expected 0 events, got %d", len(events))
		}
	})

	t.Run("其他用户访问返回403", func(t *testing.T) {
		bobRegister := doJSON(
			handler,
			nethttp.MethodPost,
			"/v1/auth/register",
			map[string]any{"login": "bob_sse", "password": "pwdpwdpwd", "display_name": "Bob"},
			nil,
		)
		if bobRegister.Code != nethttp.StatusCreated {
			t.Fatalf("register bob: %d body=%s", bobRegister.Code, bobRegister.Body.String())
		}
		bob := decodeJSONBody[registerResponse](t, bobRegister.Body.Bytes())
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false", nil, authHeader(bob.AccessToken))
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
	})

	t.Run("非GET方法返回405", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/runs/"+runPayload.RunID+"/events", nil, aliceHeaders)
		if resp.Code != nethttp.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d body=%s", resp.Code, resp.Body.String())
		}
	})
}

// parseSseEvents 解析 SSE 流文本，提取所有 data 行并反序列化为 map。
func parseSseEvents(t *testing.T, body string) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			raw := strings.TrimPrefix(line, "data: ")
			var ev map[string]any
			if err := json.Unmarshal([]byte(raw), &ev); err != nil {
				t.Fatalf("unmarshal sse data: %v raw=%s", err, raw)
			}
			events = append(events, ev)
		}
	}
	return events
}
