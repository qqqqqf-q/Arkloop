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

	"github.com/google/uuid"
)

func TestRunsCreateListGetCancelAndEnqueue(t *testing.T) {
	db := setupTestDatabase(t, "api_go_runs")

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
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 7776000)
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
	runRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo)
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
		map[string]any{"login": "alice", "password": "pwdpwdpwd"},
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

	runWithOutputRouteResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"persona_id":        "search",
			"output_route_id": "final-output-route",
		},
		aliceHeaders,
	)
	if runWithOutputRouteResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run with output_route_id status: %d body=%s", runWithOutputRouteResp.Code, runWithOutputRouteResp.Body.String())
	}
	runWithOutputRoute := decodeJSONBody[createRunResponse](t, runWithOutputRouteResp.Body.Bytes())
	runWithOutputRouteID := uuid.MustParse(runWithOutputRoute.RunID)
	var startedJSONWithOutputRoute []byte
	if err := pool.QueryRow(
		ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		runWithOutputRouteID,
	).Scan(&startedJSONWithOutputRoute); err != nil {
		t.Fatalf("load started event with output_route_id: %v", err)
	}
	var startedDataWithOutputRoute map[string]any
	if err := json.Unmarshal(startedJSONWithOutputRoute, &startedDataWithOutputRoute); err != nil {
		t.Fatalf("decode started json with output_route_id: %v raw=%s", err, string(startedJSONWithOutputRoute))
	}
	if startedDataWithOutputRoute["persona_id"] != "search" {
		t.Fatalf("unexpected started persona_id: %#v", startedDataWithOutputRoute["persona_id"])
	}
	if startedDataWithOutputRoute["output_route_id"] != "final-output-route" {
		t.Fatalf("unexpected started output_route_id: %#v", startedDataWithOutputRoute["output_route_id"])
	}

	threadOrgID := uuid.MustParse(threadPayload.OrgID)
	threadUUID := uuid.MustParse(threadPayload.ID)
	var outputCredentialID uuid.UUID
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO llm_credentials (org_id, provider, name)
		 VALUES ($1, 'anthropic', 'hybrid-output-cred')
		 RETURNING id`,
		threadOrgID,
	).Scan(&outputCredentialID); err != nil {
		t.Fatalf("create output credential: %v", err)
	}
	var outputRouteFromAgent uuid.UUID
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO llm_routes (org_id, credential_id, model, priority, is_default, when_json, multiplier)
		 VALUES ($1, $2, 'claude-3-5-haiku', 120, true, '{}'::jsonb, 1.0)
		 RETURNING id`,
		threadOrgID,
		outputCredentialID,
	).Scan(&outputRouteFromAgent); err != nil {
		t.Fatalf("create output route for output agent: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO agent_configs (org_id, scope, name, model, is_default)
		 VALUES ($1, 'org', 'sub-haiku-4.5', 'hybrid-output-cred', false)`,
		threadOrgID,
	); err != nil {
		t.Fatalf("create output agent config by name: %v", err)
	}

	var agentConfigID uuid.UUID
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO agent_configs (org_id, scope, name, safety_rules_json, is_default)
		 VALUES ($1, 'org', 'search-hybrid', $2::jsonb, false)
		 RETURNING id`,
		threadOrgID,
		`{"search_hybrid_output_routes":{"gpt5":"pg-gpt5-route"},"search_hybrid_output_agents":{"claude4":"sub-haiku-4.5"}}`,
	).Scan(&agentConfigID); err != nil {
		t.Fatalf("create agent config: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`UPDATE threads SET agent_config_id = $1 WHERE id = $2`,
		agentConfigID,
		threadUUID,
	); err != nil {
		t.Fatalf("bind thread agent config: %v", err)
	}

	runWithOutputModelResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"persona_id":         "search",
			"output_model_key": "gpt5",
		},
		aliceHeaders,
	)
	if runWithOutputModelResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run with output_model_key status: %d body=%s", runWithOutputModelResp.Code, runWithOutputModelResp.Body.String())
	}
	runWithOutputModel := decodeJSONBody[createRunResponse](t, runWithOutputModelResp.Body.Bytes())
	runWithOutputModelID := uuid.MustParse(runWithOutputModel.RunID)
	var startedJSONWithOutputModel []byte
	if err := pool.QueryRow(
		ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		runWithOutputModelID,
	).Scan(&startedJSONWithOutputModel); err != nil {
		t.Fatalf("load started event with output_model_key: %v", err)
	}
	var startedDataWithOutputModel map[string]any
	if err := json.Unmarshal(startedJSONWithOutputModel, &startedDataWithOutputModel); err != nil {
		t.Fatalf("decode started json with output_model_key: %v raw=%s", err, string(startedJSONWithOutputModel))
	}
	if startedDataWithOutputModel["output_model_key"] != "gpt5" {
		t.Fatalf("unexpected started output_model_key: %#v", startedDataWithOutputModel["output_model_key"])
	}
	if startedDataWithOutputModel["output_route_id"] != "pg-gpt5-route" {
		t.Fatalf("unexpected started output_route_id from model key: %#v", startedDataWithOutputModel["output_route_id"])
	}

	runWithBothOutputResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"persona_id":         "search",
			"output_model_key": "gpt5",
			"output_route_id":  "manual-final-route",
		},
		aliceHeaders,
	)
	if runWithBothOutputResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run with output_model_key + output_route_id status: %d body=%s", runWithBothOutputResp.Code, runWithBothOutputResp.Body.String())
	}
	runWithBothOutput := decodeJSONBody[createRunResponse](t, runWithBothOutputResp.Body.Bytes())
	runWithBothOutputID := uuid.MustParse(runWithBothOutput.RunID)
	var startedJSONWithBothOutput []byte
	if err := pool.QueryRow(
		ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		runWithBothOutputID,
	).Scan(&startedJSONWithBothOutput); err != nil {
		t.Fatalf("load started event with output_model_key + output_route_id: %v", err)
	}
	var startedDataWithBothOutput map[string]any
	if err := json.Unmarshal(startedJSONWithBothOutput, &startedDataWithBothOutput); err != nil {
		t.Fatalf("decode started json with output_model_key + output_route_id: %v raw=%s", err, string(startedJSONWithBothOutput))
	}
	if startedDataWithBothOutput["output_model_key"] != "gpt5" {
		t.Fatalf("unexpected output_model_key when both fields provided: %#v", startedDataWithBothOutput["output_model_key"])
	}
	if startedDataWithBothOutput["output_route_id"] != "manual-final-route" {
		t.Fatalf("unexpected output_route_id precedence when both fields provided: %#v", startedDataWithBothOutput["output_route_id"])
	}

	runWithOutputModelFromAgentNameResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"persona_id":         "search",
			"output_model_key": "claude4",
		},
		aliceHeaders,
	)
	if runWithOutputModelFromAgentNameResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run with output agent name mapping status: %d body=%s", runWithOutputModelFromAgentNameResp.Code, runWithOutputModelFromAgentNameResp.Body.String())
	}
	runWithOutputModelFromAgentName := decodeJSONBody[createRunResponse](t, runWithOutputModelFromAgentNameResp.Body.Bytes())
	runWithOutputModelFromAgentNameID := uuid.MustParse(runWithOutputModelFromAgentName.RunID)
	var startedJSONWithOutputModelFromAgentName []byte
	if err := pool.QueryRow(
		ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		runWithOutputModelFromAgentNameID,
	).Scan(&startedJSONWithOutputModelFromAgentName); err != nil {
		t.Fatalf("load started event with output agent name mapping: %v", err)
	}
	var startedDataWithOutputModelFromAgentName map[string]any
	if err := json.Unmarshal(startedJSONWithOutputModelFromAgentName, &startedDataWithOutputModelFromAgentName); err != nil {
		t.Fatalf("decode started json with output agent name mapping: %v raw=%s", err, string(startedJSONWithOutputModelFromAgentName))
	}
	if startedDataWithOutputModelFromAgentName["output_model_key"] != "claude4" {
		t.Fatalf("unexpected started output_model_key from output agent name path: %#v", startedDataWithOutputModelFromAgentName["output_model_key"])
	}
	if startedDataWithOutputModelFromAgentName["output_route_id"] != outputRouteFromAgent.String() {
		t.Fatalf("unexpected output_route_id from output agent name mapping: %#v", startedDataWithOutputModelFromAgentName["output_route_id"])
	}

	t.Setenv("ARKLOOP_SEARCH_OUTPUT_ROUTE_GEMINI3", "env-gemini3-route")
	runWithOutputModelEnvResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"persona_id":         "search",
			"output_model_key": "gemini3",
		},
		aliceHeaders,
	)
	if runWithOutputModelEnvResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run with env output model status: %d body=%s", runWithOutputModelEnvResp.Code, runWithOutputModelEnvResp.Body.String())
	}
	runWithOutputModelEnv := decodeJSONBody[createRunResponse](t, runWithOutputModelEnvResp.Body.Bytes())
	runWithOutputModelEnvID := uuid.MustParse(runWithOutputModelEnv.RunID)
	var startedJSONWithOutputModelEnv []byte
	if err := pool.QueryRow(
		ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		runWithOutputModelEnvID,
	).Scan(&startedJSONWithOutputModelEnv); err != nil {
		t.Fatalf("load started event with env output model: %v", err)
	}
	var startedDataWithOutputModelEnv map[string]any
	if err := json.Unmarshal(startedJSONWithOutputModelEnv, &startedDataWithOutputModelEnv); err != nil {
		t.Fatalf("decode started json with env output model: %v raw=%s", err, string(startedJSONWithOutputModelEnv))
	}
	if startedDataWithOutputModelEnv["output_model_key"] != "gemini3" {
		t.Fatalf("unexpected started output_model_key from env path: %#v", startedDataWithOutputModelEnv["output_model_key"])
	}
	if startedDataWithOutputModelEnv["output_route_id"] != "env-gemini3-route" {
		t.Fatalf("unexpected env fallback output_route_id: %#v", startedDataWithOutputModelEnv["output_route_id"])
	}

	t.Setenv("ARKLOOP_SEARCH_OUTPUT_ROUTE_GEMINI3", "")
	runWithOutputModelNoMappingResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"persona_id":         "search",
			"output_model_key": "gemini3",
		},
		aliceHeaders,
	)
	if runWithOutputModelNoMappingResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected create run with no mapping status: %d body=%s", runWithOutputModelNoMappingResp.Code, runWithOutputModelNoMappingResp.Body.String())
	}
	runWithOutputModelNoMapping := decodeJSONBody[createRunResponse](t, runWithOutputModelNoMappingResp.Body.Bytes())
	runWithOutputModelNoMappingID := uuid.MustParse(runWithOutputModelNoMapping.RunID)
	var startedJSONWithOutputModelNoMapping []byte
	if err := pool.QueryRow(
		ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		runWithOutputModelNoMappingID,
	).Scan(&startedJSONWithOutputModelNoMapping); err != nil {
		t.Fatalf("load started event with no mapping output model: %v", err)
	}
	var startedDataWithOutputModelNoMapping map[string]any
	if err := json.Unmarshal(startedJSONWithOutputModelNoMapping, &startedDataWithOutputModelNoMapping); err != nil {
		t.Fatalf("decode started json with no mapping output model: %v raw=%s", err, string(startedJSONWithOutputModelNoMapping))
	}
	if startedDataWithOutputModelNoMapping["output_model_key"] != "gemini3" {
		t.Fatalf("unexpected started output_model_key from no mapping path: %#v", startedDataWithOutputModelNoMapping["output_model_key"])
	}
	if _, ok := startedDataWithOutputModelNoMapping["output_route_id"]; ok {
		t.Fatalf("unexpected output_route_id on no mapping path: %#v", startedDataWithOutputModelNoMapping["output_route_id"])
	}

	invalidOutputRouteResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"output_route_id": "invalid route id with spaces",
		},
		aliceHeaders,
	)
	assertErrorEnvelope(t, invalidOutputRouteResp, nethttp.StatusUnprocessableEntity, "validation.error")

	invalidOutputModelResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+"/runs",
		map[string]any{
			"output_model_key": "invalid_model",
		},
		aliceHeaders,
	)
	assertErrorEnvelope(t, invalidOutputModelResp, nethttp.StatusUnprocessableEntity, "validation.error")

	var (
		jobID      uuid.UUID
		jobType    string
		jobPayload []byte
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT id, job_type, payload_json
		 FROM jobs
		 WHERE payload_json->>'run_id' = $1
		 LIMIT 1`,
		runPayload.RunID,
	).Scan(&jobID, &jobType, &jobPayload); err != nil {
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
		map[string]any{"login": "bob", "password": "pwdpwdpwd"},
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

func TestStreamRunEvents(t *testing.T) {
	db := setupTestDatabase(t, "api_go_sse")

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
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 7776000)
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
	runRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo)
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
		RunEventRepo:        runRepo,
		AuditWriter:         auditWriter,
		SSEConfig: SSEConfig{
			HeartbeatSeconds: 60.0,
			BatchLimit:       100,
		},
	})

	aliceRegister := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice_sse", "password": "pwdpwdpwd"},
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

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false", nil, nil)
		assertErrorEnvelope(t, resp, nethttp.StatusUnauthorized, "auth.missing_token")
	})

	t.Run("run not found returns 404", func(t *testing.T) {
		fakeID := "00000000-0000-0000-0000-000000000099"
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+fakeID+"/events?follow=false", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "runs.not_found")
	})

	t.Run("negative after_seq returns 422", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false&after_seq=-1", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("follow=false fetches existing events and validates SSE format", func(t *testing.T) {
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

	t.Run("after_seq=1 skips existing events returns empty", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false&after_seq=1", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		events := parseSseEvents(t, resp.Body.String())
		if len(events) != 0 {
			t.Fatalf("expected 0 events, got %d", len(events))
		}
	})

	t.Run("other user access returns 403", func(t *testing.T) {
		bobRegister := doJSON(
			handler,
			nethttp.MethodPost,
			"/v1/auth/register",
			map[string]any{"login": "bob_sse", "password": "pwdpwdpwd"},
			nil,
		)
		if bobRegister.Code != nethttp.StatusCreated {
			t.Fatalf("register bob: %d body=%s", bobRegister.Code, bobRegister.Body.String())
		}
		bob := decodeJSONBody[registerResponse](t, bobRegister.Body.Bytes())
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs/"+runPayload.RunID+"/events?follow=false", nil, authHeader(bob.AccessToken))
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
	})

	t.Run("non-GET method returns 405", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/runs/"+runPayload.RunID+"/events", nil, aliceHeaders)
		if resp.Code != nethttp.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d body=%s", resp.Code, resp.Body.String())
		}
	})
}

// parseSseEvents parses SSE stream text, extracting all data lines and deserializing into maps.
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

func TestListGlobalRuns(t *testing.T) {
	db := setupTestDatabase(t, "api_go_global_runs")

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
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 7776000)
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
	runRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo)
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

	// alice 注册并创建 thread + run
	aliceReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice_gr", "password": "pwdpwdpwd"}, nil)
	if aliceReg.Code != nethttp.StatusCreated {
		t.Fatalf("register alice: %d body=%s", aliceReg.Code, aliceReg.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, aliceReg.Body.Bytes())
	aliceHeaders := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "t1"}, aliceHeaders)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d", threadResp.Code)
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())

	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+"/runs", nil, aliceHeaders)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("create run: %d", runResp.Code)
	}
	runPayload := decodeJSONBody[createRunResponse](t, runResp.Body.Bytes())
	runID, err := uuid.Parse(runPayload.RunID)
	if err != nil {
		t.Fatalf("parse run id: %v", err)
	}
	_, err = pool.Exec(ctx, "UPDATE runs SET model = $1, persona_id = $2 WHERE id = $3", "gpt-4o-mini", "ops-trace", runID)
	if err != nil {
		t.Fatalf("seed run model/persona: %v", err)
	}

	// bob 注册（不同 org）
	bobReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "bob_gr", "password": "pwdpwdpwd"}, nil)
	if bobReg.Code != nethttp.StatusCreated {
		t.Fatalf("register bob: %d body=%s", bobReg.Code, bobReg.Body.String())
	}
	bob := decodeJSONBody[registerResponse](t, bobReg.Body.Bytes())
	bobHeaders := authHeader(bob.AccessToken)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs", nil, nil)
		assertErrorEnvelope(t, resp, nethttp.StatusUnauthorized, "auth.missing_token")
	})

	t.Run("org member sees own runs", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v body=%s", err, resp.Body.String())
		}
		if body.Total != 1 {
			t.Fatalf("expected total=1, got %d", body.Total)
		}
		if len(body.Data) != 1 || body.Data[0].RunID != runPayload.RunID {
			t.Fatalf("unexpected data: %#v", body.Data)
		}
		if body.Data[0].OrgID != threadPayload.OrgID {
			t.Fatalf("unexpected org_id: %q", body.Data[0].OrgID)
		}
	})

	t.Run("org member cannot query another org_id", func(t *testing.T) {
		// 用一个随机的合法 UUID 当作其他 org
		fakeOrgID := uuid.New().String()
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?org_id="+fakeOrgID, nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "auth.forbidden")
	})

	t.Run("bob sees own org (empty)", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs", nil, bobHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 0 {
			t.Fatalf("expected total=0, got %d", body.Total)
		}
	})

	t.Run("status filter", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?status=running", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 {
			t.Fatalf("expected total=1 for status=running, got %d", body.Total)
		}

		// 不存在的 status 返回空
		resp2 := doJSON(handler, nethttp.MethodGet, "/v1/runs?status=completed", nil, aliceHeaders)
		if resp2.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d", resp2.Code)
		}
		var body2 struct {
			Total int64 `json:"total"`
		}
		if err := json.Unmarshal(resp2.Body.Bytes(), &body2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body2.Total != 0 {
			t.Fatalf("expected total=0 for status=completed, got %d", body2.Total)
		}
	})

	t.Run("thread_id filter", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?thread_id="+threadPayload.ID, nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ThreadID != threadPayload.ID {
			t.Fatalf("unexpected thread filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("run_id filter", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?run_id="+runPayload.RunID, nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].RunID != runPayload.RunID {
			t.Fatalf("unexpected run_id filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("run_id prefix filter", func(t *testing.T) {
		prefix := runPayload.RunID[:8]
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?run_id="+prefix, nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].RunID != runPayload.RunID {
			t.Fatalf("unexpected run_id prefix result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("thread_id prefix filter", func(t *testing.T) {
		prefix := threadPayload.ID[:8]
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?thread_id="+prefix, nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ThreadID != threadPayload.ID {
			t.Fatalf("unexpected thread_id prefix result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("model and persona filters", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?model=gpt-4o&persona_id=ops", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 || len(body.Data) != 1 {
			t.Fatalf("expected model/persona filter to match one run, got total=%d data=%d", body.Total, len(body.Data))
		}
	})

	t.Run("invalid thread_id returns 422", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?thread_id=invalid", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("since after until returns 422", func(t *testing.T) {
		resp := doJSON(
			handler,
			nethttp.MethodGet,
			"/v1/runs?since=2026-03-01T00:00:00Z&until=2026-02-01T00:00:00Z",
			nil,
			aliceHeaders,
		)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("limit and offset", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?limit=1&offset=0", nil, aliceHeaders)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Total != 1 || len(body.Data) != 1 {
			t.Fatalf("expected 1 item, total=1: data=%d total=%d", len(body.Data), body.Total)
		}

		// offset=1 结果为空，但 total 不变
		resp2 := doJSON(handler, nethttp.MethodGet, "/v1/runs?limit=1&offset=1", nil, aliceHeaders)
		if resp2.Code != nethttp.StatusOK {
			t.Fatalf("unexpected status: %d", resp2.Code)
		}
		var body2 struct {
			Data  []globalRunResponse `json:"data"`
			Total int64               `json:"total"`
		}
		if err := json.Unmarshal(resp2.Body.Bytes(), &body2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body2.Total != 1 || len(body2.Data) != 0 {
			t.Fatalf("expected 0 items total=1: data=%d total=%d", len(body2.Data), body2.Total)
		}
	})

	t.Run("invalid limit returns 422", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?limit=999", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("invalid org_id returns 422", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/runs?org_id=notauuid", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("non-GET returns 405", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/runs", nil, aliceHeaders)
		if resp.Code != nethttp.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d body=%s", resp.Code, resp.Body.String())
		}
	})
}
