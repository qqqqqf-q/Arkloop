//go:build !desktop

package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"

	"arkloop/services/api/internal/observability"
	"net/http/httptest"
	"testing"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

func TestThreadRetryWithoutAssistantMessageReplaysLatestUserTurn(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_retry_latest_user")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
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
	runRepo, _ := data.NewRunEventRepository(pool)
	messageRepo, _ := data.NewMessageRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		MessageRepo:           messageRepo,
		RunEventRepo:          runRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
		TrustIncomingTraceID:  true,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice_retry_no_assistant", "password": "pwd12345", "email": "alice_retry_no_assistant@test.com"},
		nil,
	)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "retry"}, headers)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	accountID := uuid.MustParse(threadPayload.AccountID)
	threadID := uuid.MustParse(threadPayload.ID)
	userID := uuid.MustParse(alice.UserID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO messages (account_id, thread_id, created_by_user_id, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`,
		accountID,
		threadID,
		userID,
		"hello retry",
	); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	retryResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+":retry", nil, headers)
	if retryResp.Code != nethttp.StatusCreated {
		t.Fatalf("retry run: %d body=%s", retryResp.Code, retryResp.Body.String())
	}
	retryPayload := decodeJSONBody[createRunResponse](t, retryResp.Body.Bytes())

	var startedJSON []byte
	if err := pool.QueryRow(ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		retryPayload.RunID,
	).Scan(&startedJSON); err != nil {
		t.Fatalf("load retry started event: %v", err)
	}
	var startedData map[string]any
	if err := json.Unmarshal(startedJSON, &startedData); err != nil {
		t.Fatalf("decode retry started event: %v", err)
	}
	if got, _ := startedData["source"].(string); got != "retry" {
		t.Fatalf("unexpected retry source: %#v", startedData["source"])
	}
}

func TestThreadRetryWithModelOverrideUsesRequestedModel(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_retry_model")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
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
	runRepo, _ := data.NewRunEventRepository(pool)
	messageRepo, _ := data.NewMessageRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		MessageRepo:           messageRepo,
		RunEventRepo:          runRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
		TrustIncomingTraceID:  true,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice_retry_model", "password": "pwd12345", "email": "alice_retry_model@test.com"},
		nil,
	)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "retry model"}, headers)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	accountID := uuid.MustParse(threadPayload.AccountID)
	threadID := uuid.MustParse(threadPayload.ID)
	userID := uuid.MustParse(alice.UserID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO messages (account_id, thread_id, created_by_user_id, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`,
		accountID,
		threadID,
		userID,
		"hello retry model",
	); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	model := "provider^gpt-5"
	retryResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/threads/"+threadPayload.ID+":retry",
		map[string]any{"model": model},
		headers,
	)
	if retryResp.Code != nethttp.StatusCreated {
		t.Fatalf("retry run: %d body=%s", retryResp.Code, retryResp.Body.String())
	}
	retryPayload := decodeJSONBody[createRunResponse](t, retryResp.Body.Bytes())

	var startedJSON []byte
	if err := pool.QueryRow(ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		retryPayload.RunID,
	).Scan(&startedJSON); err != nil {
		t.Fatalf("load retry started event: %v", err)
	}
	var startedData map[string]any
	if err := json.Unmarshal(startedJSON, &startedData); err != nil {
		t.Fatalf("decode retry started event: %v", err)
	}
	if got, _ := startedData["model"].(string); got != model {
		t.Fatalf("unexpected retry model in started event: %#v", startedData["model"])
	}

	var jobPayload []byte
	if err := pool.QueryRow(ctx,
		`SELECT payload_json FROM jobs WHERE payload_json->>'run_id' = $1 LIMIT 1`,
		retryPayload.RunID,
	).Scan(&jobPayload); err != nil {
		t.Fatalf("load retry job payload: %v", err)
	}
	var jobJSON map[string]any
	if err := json.Unmarshal(jobPayload, &jobJSON); err != nil {
		t.Fatalf("decode retry job payload: %v raw=%s", err, string(jobPayload))
	}
	payloadObj, ok := jobJSON["payload"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected retry job payload shape: %#v", jobJSON["payload"])
	}
	if got, _ := payloadObj["model"].(string); got != model {
		t.Fatalf("unexpected retry model in job payload: %#v", payloadObj["model"])
	}
}

func TestThreadContinueCreatesResumedRun(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_continue")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
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
	runRepo, _ := data.NewRunEventRepository(pool)
	messageRepo, _ := data.NewMessageRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		MessageRepo:           messageRepo,
		RunEventRepo:          runRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
		TrustIncomingTraceID:  true,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice_continue", "password": "pwd12345", "email": "alice_continue@test.com"},
		nil,
	)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "continue"}, headers)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	accountID := uuid.MustParse(threadPayload.AccountID)
	threadID := uuid.MustParse(threadPayload.ID)
	userID := uuid.MustParse(alice.UserID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO messages (id, account_id, thread_id, created_by_user_id, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, $4, 'user', $5, '{}'::jsonb, false)`,
		uuid.New(),
		accountID,
		threadID,
		userID,
		"hello continue",
	); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+"/runs", nil, headers)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("create run: %d body=%s", runResp.Code, runResp.Body.String())
	}
	runPayload := decodeJSONBody[createRunResponse](t, runResp.Body.Bytes())
	runID := uuid.MustParse(runPayload.RunID)
	parentModel := "provider^previous-model"
	parentPersonaID := "resume-persona@1"
	parentRole := "worker"
	parentWorkDir := "/workspace/project"
	parentReasoningMode := "high"
	parentOutputModelKey := "gpt5"
	parentProfileRef := "profile_parent"
	parentWorkspaceRef := "workspace_parent"
	if _, err := pool.Exec(ctx,
		`UPDATE run_events
		    SET data_json = data_json || jsonb_build_object(
		        'model', $2,
		        'persona_id', $3,
		        'role', $4,
		        'work_dir', $5,
		        'reasoning_mode', $6,
		        'output_model_key', $7
		    )
		  WHERE run_id = $1 AND type = 'run.started'`,
		runID,
		parentModel,
		parentPersonaID,
		parentRole,
		parentWorkDir,
		parentReasoningMode,
		parentOutputModelKey,
	); err != nil {
		t.Fatalf("patch parent started event: %v", err)
	}
	cancelledAt := time.Now().UTC()

	if _, err := pool.Exec(ctx,
		`UPDATE runs
		    SET status = 'cancelled',
		        status_updated_at = $2,
		        failed_at = $2,
		        profile_ref = $3,
		        workspace_ref = $4
		  WHERE id = $1`,
		runID,
		cancelledAt,
		parentProfileRef,
		parentWorkspaceRef,
	); err != nil {
		t.Fatalf("mark run cancelled: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json, ts)
		 VALUES ($1, 2, 'message.delta', '{"role":"assistant","content_delta":"partial"}'::jsonb, $2),
		        ($1, 3, 'run.cancelled', '{}'::jsonb, $2)`,
		runID,
		cancelledAt,
	); err != nil {
		t.Fatalf("insert continue events: %v", err)
	}

	continueResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+":continue", map[string]any{"run_id": runID.String()}, headers)
	if continueResp.Code != nethttp.StatusCreated {
		t.Fatalf("continue run: %d body=%s", continueResp.Code, continueResp.Body.String())
	}
	continuePayload := decodeJSONBody[createRunResponse](t, continueResp.Body.Bytes())

	var resumeFromRunID *uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT resume_from_run_id FROM runs WHERE id = $1`,
		continuePayload.RunID,
	).Scan(&resumeFromRunID); err != nil {
		t.Fatalf("load resumed run: %v", err)
	}
	if resumeFromRunID == nil || *resumeFromRunID != runID {
		t.Fatalf("unexpected resume_from_run_id: %#v want %s", resumeFromRunID, runID)
	}
	var resumedCreatedByUserID *uuid.UUID
	var resumedProfileRef *string
	var resumedWorkspaceRef *string
	if err := pool.QueryRow(ctx,
		`SELECT created_by_user_id, profile_ref, workspace_ref FROM runs WHERE id = $1`,
		continuePayload.RunID,
	).Scan(&resumedCreatedByUserID, &resumedProfileRef, &resumedWorkspaceRef); err != nil {
		t.Fatalf("load resumed bindings: %v", err)
	}
	if resumedCreatedByUserID == nil || *resumedCreatedByUserID != userID {
		t.Fatalf("unexpected resumed created_by_user_id: %#v", resumedCreatedByUserID)
	}
	if got := strings.TrimSpace(func() string {
		if resumedProfileRef == nil {
			return ""
		}
		return *resumedProfileRef
	}()); got != parentProfileRef {
		t.Fatalf("unexpected resumed profile_ref: %#v", resumedProfileRef)
	}
	if got := strings.TrimSpace(func() string {
		if resumedWorkspaceRef == nil {
			return ""
		}
		return *resumedWorkspaceRef
	}()); got != parentWorkspaceRef {
		t.Fatalf("unexpected resumed workspace_ref: %#v", resumedWorkspaceRef)
	}

	var startedJSON []byte
	if err := pool.QueryRow(ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		continuePayload.RunID,
	).Scan(&startedJSON); err != nil {
		t.Fatalf("load continue started event: %v", err)
	}
	var startedData map[string]any
	if err := json.Unmarshal(startedJSON, &startedData); err != nil {
		t.Fatalf("decode continue started event: %v", err)
	}
	if got, _ := startedData["source"].(string); got != "continue" {
		t.Fatalf("unexpected continue source: %#v", startedData["source"])
	}
	if got, _ := startedData["continuation_source"].(string); got != "user_followup" {
		t.Fatalf("unexpected continuation_source: %#v", startedData["continuation_source"])
	}
	if got, _ := startedData["model"].(string); got != parentModel {
		t.Fatalf("unexpected continue model in started event: %#v", startedData["model"])
	}
	if got, _ := startedData["persona_id"].(string); got != parentPersonaID {
		t.Fatalf("unexpected continue persona_id in started event: %#v", startedData["persona_id"])
	}
	if got, _ := startedData["role"].(string); got != parentRole {
		t.Fatalf("unexpected continue role in started event: %#v", startedData["role"])
	}
	if got, _ := startedData["work_dir"].(string); got != parentWorkDir {
		t.Fatalf("unexpected continue work_dir in started event: %#v", startedData["work_dir"])
	}
	if got, _ := startedData["reasoning_mode"].(string); got != parentReasoningMode {
		t.Fatalf("unexpected continue reasoning_mode in started event: %#v", startedData["reasoning_mode"])
	}
	if got, _ := startedData["output_model_key"].(string); got != parentOutputModelKey {
		t.Fatalf("unexpected continue output_model_key in started event: %#v", startedData["output_model_key"])
	}

	var jobPayload []byte
	if err := pool.QueryRow(ctx,
		`SELECT payload_json FROM jobs WHERE payload_json->>'run_id' = $1 LIMIT 1`,
		continuePayload.RunID,
	).Scan(&jobPayload); err != nil {
		t.Fatalf("load continue job payload: %v", err)
	}
	var jobJSON map[string]any
	if err := json.Unmarshal(jobPayload, &jobJSON); err != nil {
		t.Fatalf("decode continue job payload: %v raw=%s", err, string(jobPayload))
	}
	payloadObj, ok := jobJSON["payload"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected continue job payload shape: %#v", jobJSON["payload"])
	}
	if got, _ := payloadObj["model"].(string); got != parentModel {
		t.Fatalf("unexpected continue model in job payload: %#v", payloadObj["model"])
	}
	if got, _ := payloadObj["persona_id"].(string); got != parentPersonaID {
		t.Fatalf("unexpected continue persona_id in job payload: %#v", payloadObj["persona_id"])
	}
	if got, _ := payloadObj["role"].(string); got != parentRole {
		t.Fatalf("unexpected continue role in job payload: %#v", payloadObj["role"])
	}
	if got, _ := payloadObj["work_dir"].(string); got != parentWorkDir {
		t.Fatalf("unexpected continue work_dir in job payload: %#v", payloadObj["work_dir"])
	}
	if got, _ := payloadObj["reasoning_mode"].(string); got != parentReasoningMode {
		t.Fatalf("unexpected continue reasoning_mode in job payload: %#v", payloadObj["reasoning_mode"])
	}
	if got, _ := payloadObj["output_model_key"].(string); got != parentOutputModelKey {
		t.Fatalf("unexpected continue output_model_key in job payload: %#v", payloadObj["output_model_key"])
	}
}

func TestThreadContinueCreatesResumedRunFromFailedRunWithOutput(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_continue_failed")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
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
	runRepo, _ := data.NewRunEventRepository(pool)
	messageRepo, _ := data.NewMessageRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		MessageRepo:           messageRepo,
		RunEventRepo:          runRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
		TrustIncomingTraceID:  true,
	})

	registerResp := doJSON(
		handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "alice_continue_failed", "password": "pwd12345", "email": "alice_continue_failed@test.com"},
		nil,
	)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "continue failed"}, headers)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	accountID := uuid.MustParse(threadPayload.AccountID)
	threadID := uuid.MustParse(threadPayload.ID)
	userID := uuid.MustParse(alice.UserID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO messages (id, account_id, thread_id, created_by_user_id, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, $4, 'user', $5, '{}'::jsonb, false)`,
		uuid.New(),
		accountID,
		threadID,
		userID,
		"hello continue failed",
	); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+"/runs", nil, headers)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("create run: %d body=%s", runResp.Code, runResp.Body.String())
	}
	runPayload := decodeJSONBody[createRunResponse](t, runResp.Body.Bytes())
	runID := uuid.MustParse(runPayload.RunID)
	failedAt := time.Now().UTC()
	parentModel := "provider^persisted-model"

	if _, err := pool.Exec(ctx,
		`UPDATE runs
		    SET status = 'failed',
		        status_updated_at = $2,
		        failed_at = $2,
		        model = $3
		  WHERE id = $1`,
		runID,
		failedAt,
		parentModel,
	); err != nil {
		t.Fatalf("mark run failed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json, ts)
		 VALUES ($1, 2, 'message.delta', '{"role":"assistant","content_delta":"partial"}'::jsonb, $2),
		        ($1, 3, 'run.failed', '{"error_class":"provider.non_retryable"}'::jsonb, $2)`,
		runID,
		failedAt,
	); err != nil {
		t.Fatalf("insert failed continue events: %v", err)
	}

	continueResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+":continue", map[string]any{"run_id": runID.String()}, headers)
	if continueResp.Code != nethttp.StatusCreated {
		t.Fatalf("continue failed run: %d body=%s", continueResp.Code, continueResp.Body.String())
	}
	continuePayload := decodeJSONBody[createRunResponse](t, continueResp.Body.Bytes())

	var resumeFromRunID *uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT resume_from_run_id FROM runs WHERE id = $1`,
		continuePayload.RunID,
	).Scan(&resumeFromRunID); err != nil {
		t.Fatalf("load resumed run: %v", err)
	}
	if resumeFromRunID == nil || *resumeFromRunID != runID {
		t.Fatalf("unexpected resume_from_run_id: %#v want %s", resumeFromRunID, runID)
	}

	var startedJSON []byte
	if err := pool.QueryRow(ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		continuePayload.RunID,
	).Scan(&startedJSON); err != nil {
		t.Fatalf("load continue started event: %v", err)
	}
	var startedData map[string]any
	if err := json.Unmarshal(startedJSON, &startedData); err != nil {
		t.Fatalf("decode continue started event: %v", err)
	}
	if got, _ := startedData["model"].(string); got != parentModel {
		t.Fatalf("unexpected continue model in started event: %#v", startedData["model"])
	}
}

func TestThreadContinueCreatesResumedRunFromFailedRunWithThinkingOnly(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads_continue_failed_thinking")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
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
	runRepo, _ := data.NewRunEventRepository(pool)
	messageRepo, _ := data.NewMessageRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		MessageRepo:           messageRepo,
		RunEventRepo:          runRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
		TrustIncomingTraceID:  true,
	})

	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{"login": "alice_continue_thinking", "password": "pwd12345", "email": "alice_continue_thinking@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	alice := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	headers := authHeader(alice.AccessToken)

	threadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "continue thinking"}, headers)
	if threadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create thread: %d body=%s", threadResp.Code, threadResp.Body.String())
	}
	threadPayload := decodeJSONBody[threadResponse](t, threadResp.Body.Bytes())
	accountID := uuid.MustParse(threadPayload.AccountID)
	threadID := uuid.MustParse(threadPayload.ID)
	userID := uuid.MustParse(alice.UserID)

	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, created_by_user_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, $4, 'user', $5, '{}'::jsonb, false)`, uuid.New(), accountID, threadID, userID, "hello thinking"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	runResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+"/runs", nil, headers)
	if runResp.Code != nethttp.StatusCreated {
		t.Fatalf("create run: %d body=%s", runResp.Code, runResp.Body.String())
	}
	runID := uuid.MustParse(decodeJSONBody[createRunResponse](t, runResp.Body.Bytes()).RunID)
	failedAt := time.Now().UTC()
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'failed', status_updated_at = $2, failed_at = $2 WHERE id = $1`, runID, failedAt); err != nil {
		t.Fatalf("mark run failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json, ts) VALUES ($1, 2, 'message.delta', '{"role":"assistant","channel":"thinking","content_delta":"ponder"}'::jsonb, $2), ($1, 3, 'run.failed', '{"error_class":"provider.non_retryable"}'::jsonb, $2)`, runID, failedAt); err != nil {
		t.Fatalf("insert thinking events: %v", err)
	}

	continueResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+threadPayload.ID+":continue", map[string]any{"run_id": runID.String()}, headers)
	if continueResp.Code != nethttp.StatusCreated {
		t.Fatalf("continue thinking run: %d body=%s", continueResp.Code, continueResp.Body.String())
	}
}

func TestThreadsCreateListGetPatchAndAudit(t *testing.T) {
	db := setupTestDatabase(t, "api_go_threads")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

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
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
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
	if threadPayload.ID == "" || threadPayload.CreatedAt == "" || threadPayload.AccountID == "" {
		t.Fatalf("unexpected thread payload: %#v", threadPayload)
	}
	if threadPayload.ProjectID == nil || *threadPayload.ProjectID == "" {
		t.Fatalf("expected project_id in thread payload: %#v", threadPayload)
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

	deniedCount, err := countDeniedAudit(ctx, pool, "threads.get", "account_mismatch")
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

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

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
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}

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
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		ProjectRepo:           projectRepo,
		AuditWriter:           auditWriter,
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
	aliceAccountID, err := uuid.Parse(patchThread.AccountID)
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
		count, err := countDeniedAudit(ctx, pool, "threads.update", "account_mismatch")
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
	noOwnerPatchProject := mustCreateTestProject(t, ctx, pool, aliceAccountID, nil, "no-owner-patch")
	noOwnerPatchThread, err := threadRepo.Create(ctx, aliceAccountID, nil, noOwnerPatchProject.ID, &noOwnerTitle, false)
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
		count, err := countDeniedAudit(ctx, pool, "threads.delete", "account_mismatch")
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
	noOwnerDeleteProject := mustCreateTestProject(t, ctx, pool, aliceAccountID, nil, "no-owner-delete")
	noOwnerDeleteThread, err := threadRepo.Create(ctx, aliceAccountID, nil, noOwnerDeleteProject.ID, &noOwnerDeleteTitle, false)
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

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

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
	runRepo, _ := data.NewRunEventRepository(pool)

	authService, _ := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	jobRepo, _ := data.NewJobRepository(pool)
	registrationService, _ := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		ProjectRepo:           projectRepo,
		RunEventRepo:          runRepo,
		AuditWriter:           auditWriter,
		TrustIncomingTraceID:  true,
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
