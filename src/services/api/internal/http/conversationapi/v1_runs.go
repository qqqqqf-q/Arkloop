package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runlimit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var (
	routeIDRegex    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	personaIDRegex  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}(?:@[A-Za-z0-9][A-Za-z0-9._:-]{0,63})?$`)
	uuidPrefixRegex = regexp.MustCompile(`^[0-9a-fA-F-]{1,36}$`)
)

const (
	searchOutputModelKeyGPT5    = "gpt5"
	searchOutputModelKeyClaude4 = "claude4"
	searchOutputModelKeyGemini3 = "gemini3"

	searchOutputRouteEnvGPT5    = "ARKLOOP_SEARCH_OUTPUT_ROUTE_GPT5"
	searchOutputRouteEnvClaude4 = "ARKLOOP_SEARCH_OUTPUT_ROUTE_CLAUDE4"
	searchOutputRouteEnvGemini3 = "ARKLOOP_SEARCH_OUTPUT_ROUTE_GEMINI3"

	searchHybridOutputRoutesKey = "search_hybrid_output_routes"
	searchHybridOutputModelsKey = "search_hybrid_output_models"
)

var runTerminalEventTypes = []string{"run.completed", "run.failed", "run.cancelled"}

type createRunRequest struct {
	RouteID        *string `json:"route_id"`
	PersonaID      *string `json:"persona_id"`
	OutputRouteID  *string `json:"output_route_id"`
	OutputModelKey *string `json:"output_model_key"`
}

type createRunResponse struct {
	RunID   string `json:"run_id"`
	TraceID string `json:"trace_id"`
}

type threadRunResponse struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type runResponse struct {
	RunID           string   `json:"run_id"`
	AccountID           string   `json:"account_id"`
	ThreadID        string   `json:"thread_id"`
	CreatedByUserID *string  `json:"created_by_user_id"`
	ParentRunID     *string  `json:"parent_run_id,omitempty"`
	ChildRunIDs     []string `json:"child_run_ids,omitempty"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	TraceID         string   `json:"trace_id"`
}

type cancelRunResponse struct {
	OK bool `json:"ok"`
}

type submitInputResponse struct {
	OK bool `json:"ok"`
}

type globalRunResponse struct {
	RunID             string   `json:"run_id"`
	AccountID             string   `json:"account_id"`
	ThreadID          string   `json:"thread_id"`
	Status            string   `json:"status"`
	Model             *string  `json:"model,omitempty"`
	PersonaID         *string  `json:"persona_id,omitempty"`
	ParentRunID       *string  `json:"parent_run_id,omitempty"`
	TotalInputTokens  *int64   `json:"total_input_tokens,omitempty"`
	TotalOutputTokens *int64   `json:"total_output_tokens,omitempty"`
	TotalCostUSD      *float64 `json:"total_cost_usd,omitempty"`
	DurationMs        *int64   `json:"duration_ms,omitempty"`
	CacheHitRate      *float64 `json:"cache_hit_rate,omitempty"`
	CreditsUsed       *int64   `json:"credits_used,omitempty"`
	CreatedAt         string   `json:"created_at"`
	CompletedAt       *string  `json:"completed_at,omitempty"`
	FailedAt          *string  `json:"failed_at,omitempty"`
	// 创建者信息（LEFT JOIN users）
	CreatedByUserID   *string `json:"created_by_user_id,omitempty"`
	CreatedByUserName *string `json:"created_by_user_name,omitempty"`
	CreatedByEmail    *string `json:"created_by_email,omitempty"`
}

func createThreadRun(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
	pool data.DB,
	apiKeysRepo *data.APIKeysRepository,
	limiter *data.RunLimiter,
	entSvc *entitlement.Service,
	rdb *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsWrite, w, traceID) {
			return
		}

		var body *createRunRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			if errors.Is(err, io.EOF) {
				body = nil
			} else {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
		}

		startedData := map[string]any{}
		outputRouteID := ""
		outputModelKey := ""

		if body != nil && body.RouteID != nil {
			routeID := strings.TrimSpace(*body.RouteID)
			if !routeIDRegex.MatchString(routeID) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			startedData["route_id"] = routeID
		}
		if body != nil && body.PersonaID != nil {
			if !personaIDRegex.MatchString(*body.PersonaID) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			startedData["persona_id"] = strings.TrimSpace(*body.PersonaID)
		}
		if body != nil && body.OutputRouteID != nil {
			outputRouteID = strings.TrimSpace(*body.OutputRouteID)
			if !routeIDRegex.MatchString(outputRouteID) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			startedData["output_route_id"] = outputRouteID
		}
		if body != nil && body.OutputModelKey != nil {
			outputModelKey = normalizeSearchOutputModelKey(*body.OutputModelKey)
			if outputModelKey == "" {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			startedData["output_model_key"] = outputModelKey
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.create", thread, auditWriter) {
			return
		}
		if outputRouteID == "" && outputModelKey != "" {
			resolvedOutputRouteID, err := resolveSearchOutputRouteID(r.Context(), pool, thread, outputModelKey)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if resolvedOutputRouteID != "" {
				startedData["output_route_id"] = resolvedOutputRouteID
			}
		}

		var acquired bool
		if entSvc != nil && rdb != nil {
			// 从权益系统获取该 account 的并发上限，动态解析覆盖全局配置。
			limitVal, err := entSvc.Resolve(r.Context(), thread.AccountID, "limit.concurrent_runs")
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			key := runlimit.Key(thread.AccountID.String())
			if !runlimit.TryAcquire(r.Context(), rdb, key, limitVal.Int()) {
				httpkit.WriteError(w, nethttp.StatusTooManyRequests, "runs.limit_exceeded", "concurrent run limit exceeded", traceID, nil)
				return
			}
			acquired = true
			defer func() {
				if acquired {
					runlimit.Release(r.Context(), rdb, key)
				}
			}()
		} else if limiter != nil {
			if !limiter.TryAcquire(r.Context(), thread.AccountID) {
				httpkit.WriteError(w, nethttp.StatusTooManyRequests, "runs.limit_exceeded", "concurrent run limit exceeded", traceID, nil)
				return
			}
			acquired = true
			defer func() {
				if acquired {
					limiter.Release(r.Context(), thread.AccountID)
				}
			}()
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		runRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		jobRepo, err := data.NewJobRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		run, _, err := runRepo.CreateRunWithStartedEvent(
			r.Context(),
			thread.AccountID,
			thread.ID,
			&actor.UserID,
			"run.started",
			startedData,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		_, err = jobRepo.EnqueueRun(
			r.Context(),
			thread.AccountID,
			run.ID,
			traceID,
			data.RunExecuteJobType,
			map[string]any{"source": "api"},
			nil,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// Commit 成功，计数器由 Worker 在终态时 DECR
		acquired = false

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, createRunResponse{
			RunID:   run.ID.String(),
			TraceID: traceID,
		})
	}
}

func normalizeSearchOutputModelKey(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case searchOutputModelKeyGPT5:
		return searchOutputModelKeyGPT5
	case searchOutputModelKeyClaude4:
		return searchOutputModelKeyClaude4
	case searchOutputModelKeyGemini3:
		return searchOutputModelKeyGemini3
	default:
		return ""
	}
}

func resolveSearchOutputRouteID(
	ctx context.Context,
	pool data.DB,
	thread *data.Thread,
	outputModelKey string,
) (string, error) {
	if pool != nil && thread != nil {
		routeID, err := resolveSearchOutputRouteIDFromPlatformSetting(ctx, pool, thread.AccountID, outputModelKey)
		if err != nil {
			return "", err
		}
		if routeID != "" {
			return routeID, nil
		}
	}
	return resolveSearchOutputRouteIDFromEnv(outputModelKey), nil
}

func resolveSearchOutputRouteIDFromPlatformSetting(
	ctx context.Context,
	pool data.DB,
	accountID uuid.UUID,
	outputModelKey string,
) (string, error) {
	var raw string
	if err := pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		searchHybridOutputModelsKey,
	).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}

	var models map[string]any
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return "", nil
	}
	selector := pickSearchOutputModelSelector(models, outputModelKey)
	if selector == "" {
		return "", nil
	}
	return resolveSearchOutputRouteIDByModelSelector(ctx, pool, accountID, selector)
}

func pickSearchOutputModelSelector(models map[string]any, outputModelKey string) string {
	if models == nil {
		return ""
	}
	rawSelector, ok := models[outputModelKey]
	if !ok {
		return ""
	}
	selector, ok := rawSelector.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(selector)
}

func resolveSearchOutputRouteIDByModelSelector(
	ctx context.Context,
	pool data.DB,
	accountID uuid.UUID,
	selector string,
) (string, error) {
	cleanedSelector := strings.TrimSpace(selector)
	if cleanedSelector == "" {
		return "", nil
	}

	parts := strings.SplitN(cleanedSelector, "^", 2)
	var routeID uuid.UUID
	if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
		err := pool.QueryRow(
			ctx,
			`SELECT r.id
			 FROM llm_routes r
			 JOIN llm_credentials c ON c.id = r.credential_id
			 WHERE r.account_id = $1
			   AND c.revoked_at IS NULL
			   AND lower(c.name) = lower($2)
			   AND lower(r.model) = lower($3)
			 ORDER BY r.priority DESC, r.is_default DESC
			 LIMIT 1`,
			accountID,
			strings.TrimSpace(parts[0]),
			strings.TrimSpace(parts[1]),
		).Scan(&routeID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", nil
			}
			return "", err
		}
	} else {
		err := pool.QueryRow(
			ctx,
			`SELECT r.id
			 FROM llm_routes r
			 JOIN llm_credentials c ON c.id = r.credential_id
			 WHERE r.account_id = $1
			   AND c.revoked_at IS NULL
			   AND lower(r.model) = lower($2)
			 ORDER BY r.priority DESC, r.is_default DESC
			 LIMIT 1`,
			accountID,
			cleanedSelector,
		).Scan(&routeID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", nil
			}
			return "", err
		}
	}

	cleanedRouteID := strings.TrimSpace(routeID.String())
	if cleanedRouteID == "" || !routeIDRegex.MatchString(cleanedRouteID) {
		return "", nil
	}
	return cleanedRouteID, nil
}

func resolveSearchOutputRouteIDFromEnv(outputModelKey string) string {
	var envKey string
	switch outputModelKey {
	case searchOutputModelKeyGPT5:
		envKey = searchOutputRouteEnvGPT5
	case searchOutputModelKeyClaude4:
		envKey = searchOutputRouteEnvClaude4
	case searchOutputModelKeyGemini3:
		envKey = searchOutputRouteEnvGemini3
	default:
		return ""
	}

	routeID := strings.TrimSpace(os.Getenv(envKey))
	if routeID == "" || !routeIDRegex.MatchString(routeID) {
		return ""
	}
	return routeID
}

func listThreadRuns(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.list", thread, auditWriter) {
			return
		}

		runs, err := runRepo.ListRunsByThread(r.Context(), actor.AccountID, threadID, limit)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]threadRunResponse, 0, len(runs))
		for _, run := range runs {
			status := run.Status
			if status == "running" {
				terminal, err := runRepo.GetLatestEventType(r.Context(), run.ID, runTerminalEventTypes)
				if err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
				status = deriveRunStatus(terminal)
			}
			resp = append(resp, threadRunResponse{
				RunID:     run.ID.String(),
				Status:    status,
				CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func getRun(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.get", run, auditWriter) {
			return
		}

		status := run.Status
		if status == "running" {
			terminal, err := runRepo.GetLatestEventType(r.Context(), run.ID, runTerminalEventTypes)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			status = deriveRunStatus(terminal)
		}

		var createdByUserID *string
		if run.CreatedByUserID != nil {
			value := run.CreatedByUserID.String()
			createdByUserID = &value
		}

		var parentRunID *string
		if run.ParentRunID != nil {
			s := run.ParentRunID.String()
			parentRunID = &s
		}

		childIDs, err := runRepo.ListChildRunIDs(r.Context(), run.ID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		var childRunIDs []string
		for _, cid := range childIDs {
			childRunIDs = append(childRunIDs, cid.String())
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, runResponse{
			RunID:           run.ID.String(),
			AccountID:           run.AccountID.String(),
			ThreadID:        run.ThreadID.String(),
			CreatedByUserID: createdByUserID,
			ParentRunID:     parentRunID,
			ChildRunIDs:     childRunIDs,
			Status:          status,
			CreatedAt:       run.CreatedAt.UTC().Format(time.RFC3339Nano),
			TraceID:         traceID,
		})
	}
}

func cancelRun(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool data.DB,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsWrite, w, traceID) {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.cancel", run, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		txRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		_, err = txRepo.RequestCancel(r.Context(), run.ID, &actor.UserID, traceID)
		if err != nil {
			var notFound data.RunNotFoundError
			if errors.As(err, &notFound) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 通知 worker 立即中断，失败可忽略（worker 有 DB 兜底检查）
		_, _ = pool.Exec(r.Context(), "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, run.ID.String())

		if auditWriter != nil {
			auditWriter.WriteRunCancelRequested(r.Context(), traceID, actor.AccountID, actor.UserID, run.ID)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, cancelRunResponse{OK: true})
	}
}

func submitRunInput(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool data.DB,
	apiKeysRepo *data.APIKeysRepository,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsWrite, w, traceID) {
			return
		}

		var body struct {
			Content string `json:"content"`
		}
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "content must not be empty", traceID, nil)
			return
		}

		limitBytes := 32768
		if resolver != nil {
			raw, err := resolver.Resolve(r.Context(), "limit.max_input_content_bytes", sharedconfig.Scope{})
			if err == nil {
				if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
					limitBytes = v
				}
			}
		}

		if len(body.Content) > limitBytes {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "content too large", traceID, nil)
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.input", run, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		txRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if _, err := txRepo.ProvideInput(r.Context(), run.ID, body.Content, traceID); err != nil {
			var notActive data.RunNotActiveError
			if errors.As(err, &notActive) {
				httpkit.WriteError(w, nethttp.StatusConflict, "runs.not_active", "run is not active", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 唤醒 Worker 侧的 WaitForInput LISTEN goroutine
		_, _ = pool.Exec(r.Context(), "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, run.ID.String())

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, submitInputResponse{OK: true})
	}
}

func streamRunEvents(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	directPool *pgxpool.Pool,
	directPoolAcquireTimeout time.Duration,
	sseConfig SSEConfig,
	apiKeysRepo *data.APIKeysRepository,
	rdb *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.events", run, auditWriter) {
			return
		}

		afterSeq, follow, ok := parseSSEQueryParams(w, traceID, r)
		if !ok {
			return
		}

		batchLimit := sseConfig.BatchLimit
		if batchLimit <= 0 {
			batchLimit = 500
		}
		heartbeatDuration := time.Duration(float64(time.Second) * sseConfig.HeartbeatSeconds)

		flusher, canFlush := w.(nethttp.Flusher)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(nethttp.StatusOK)

		if follow {
			_, _ = fmt.Fprint(w, ": ping\n\n")
			if canFlush {
				flusher.Flush()
			}
		}

		// LISTEN for pg_notify from worker commits
		var notifyCh <-chan struct{}
		if follow && directPool != nil {
			acquireCtx := r.Context()
			var cancelAcquire context.CancelFunc
			if directPoolAcquireTimeout > 0 {
				acquireCtx, cancelAcquire = context.WithTimeout(acquireCtx, directPoolAcquireTimeout)
				defer cancelAcquire()
			}

			listenConn, err := directPool.Acquire(acquireCtx)
			if err == nil {
				channel := fmt.Sprintf(`"run_events:%s"`, runID.String())
				if _, err := listenConn.Exec(r.Context(), "LISTEN "+channel); err == nil {
					ch := make(chan struct{}, 1)
					notifyCh = ch
					go func() {
						defer listenConn.Release()
						for {
							if err := listenConn.Conn().PgConn().WaitForNotification(r.Context()); err != nil {
								return
							}
							select {
							case ch <- struct{}{}:
							default:
							}
						}
					}()
				} else {
					listenConn.Release()
				}
			}
		}

		// Redis Pub/Sub 跨实例广播
		var redisCh <-chan struct{}
		if follow && rdb != nil {
			redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", runID.String())
			sub := rdb.Subscribe(r.Context(), redisChannel)
			msgCh := sub.Channel()
			ch := make(chan struct{}, 1)
			redisCh = ch
			go func() {
				defer sub.Close()
				for {
					select {
					case <-r.Context().Done():
						return
					case _, ok := <-msgCh:
						if !ok {
							return
						}
						select {
						case ch <- struct{}{}:
						default:
						}
					}
				}
			}()
		}

		// 合并 pg_notify + Redis Pub/Sub 为单一持久信号 channel，避免循环内重复创建 goroutine。
		var sigCh <-chan struct{}
		if notifyCh != nil && redisCh != nil {
			merged := make(chan struct{}, 1)
			sigCh = merged
			go func() {
				for {
					select {
					case <-r.Context().Done():
						return
					case <-notifyCh:
					case <-redisCh:
					}
					select {
					case merged <- struct{}{}:
					default:
					}
				}
			}()
		} else if notifyCh != nil {
			sigCh = notifyCh
		} else if redisCh != nil {
			sigCh = redisCh
		}

		cursor := afterSeq
		lastSend := time.Now()

		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}

			events, err := runRepo.ListEvents(r.Context(), runID, cursor, batchLimit)
			if err != nil {
				return
			}

			if len(events) > 0 {
				for _, item := range events {
					cursor = item.Seq
					if err := writeSseEvent(w, item); err != nil {
						return
					}
				}
				lastSend = time.Now()
				if canFlush {
					flusher.Flush()
				}
				continue
			}

			if !follow {
				return
			}

			now := time.Now()
			if heartbeatDuration > 0 && now.Sub(lastSend) >= heartbeatDuration {
				_, _ = fmt.Fprint(w, ": ping\n\n")
				if canFlush {
					flusher.Flush()
				}
				lastSend = now
			}

			if sigCh != nil {
				select {
				case <-r.Context().Done():
					return
				case <-sigCh:
				case <-time.After(heartbeatDuration):
				}
			} else {
				// fallback: poll at heartbeat interval
				select {
				case <-r.Context().Done():
					return
				case <-time.After(heartbeatDuration):
				}
			}
		}
	}
}

// writeSseEvent writes a single RunEvent to the response stream in SSE format.
func writeSseEvent(w nethttp.ResponseWriter, item data.RunEvent) error {
	ts := item.TS.UTC()
	millis := ts.Format("2006-01-02T15:04:05.999Z07:00")

	payload := map[string]any{
		"event_id": item.EventID.String(),
		"run_id":   item.RunID.String(),
		"seq":      item.Seq,
		"ts":       millis,
		"type":     item.Type,
		"data":     item.DataJSON,
	}
	if payload["data"] == nil {
		payload["data"] = map[string]any{}
	}

	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", item.Seq, item.Type, dataBytes)
	return err
}

func parseSSEQueryParams(
	w nethttp.ResponseWriter,
	traceID string,
	r *nethttp.Request,
) (afterSeq int64, follow bool, ok bool) {
	follow = true
	afterSeq = 0

	if raw := strings.TrimSpace(r.URL.Query().Get("after_seq")); raw != "" {
		parsed, err := parseInt64NonNegative(raw)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return 0, false, false
		}
		afterSeq = parsed
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("follow")); raw != "" {
		switch raw {
		case "true", "1":
			follow = true
		case "false", "0":
			follow = false
		default:
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return 0, false, false
		}
	}

	return afterSeq, follow, true
}

func parseInt64NonNegative(raw string) (int64, error) {
	v, err := parseInt64(raw)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return v, nil
}

func parseInt64(raw string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &v)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	return v, nil
}

func runEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool data.DB,
	directPool *pgxpool.Pool,
	directPoolAcquireTimeout time.Duration,
	sseConfig SSEConfig,
	apiKeysRepo *data.APIKeysRepository,
	resolver sharedconfig.Resolver,
	rdb *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getRun(authService, membershipRepo, runRepo, auditWriter, apiKeysRepo)
	cancel := cancelRun(authService, membershipRepo, runRepo, auditWriter, pool, apiKeysRepo)
	submitInput := submitRunInput(authService, membershipRepo, runRepo, auditWriter, pool, apiKeysRepo, resolver)
	streamEvents := streamRunEvents(authService, membershipRepo, runRepo, auditWriter, directPool, directPoolAcquireTimeout, sseConfig, apiKeysRepo, rdb)

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		parts := strings.SplitN(tail, "/", 2)
		idPart, actionPart, hasAction := strings.Cut(parts[0], ":")

		runID, err := uuid.Parse(idPart)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		if hasAction {
			if actionPart != "cancel" {
				httpkit.WriteNotFound(w, r)
				return
			}
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			cancel(w, r, runID)
			return
		}

		if len(parts) == 1 {
			if r.Method != nethttp.MethodGet {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			get(w, r, runID)
			return
		}

		if parts[1] == "events" {
			if r.Method != nethttp.MethodGet {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			streamEvents(w, r, runID)
			return
		}

		if parts[1] == "input" {
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			submitInput(w, r, runID)
			return
		}

		httpkit.WriteNotFound(w, r)
	}
}

func deriveRunStatus(terminalEventType string) string {
	switch terminalEventType {
	case "run.completed":
		return "completed"
	case "run.failed":
		return "failed"
	case "run.cancelled":
		return "cancelled"
	default:
		return "running"
	}
}

func authorizeRunOrAudit(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	action string,
	run *data.Run,
	auditWriter *audit.Writer,
) bool {
	if actor == nil || run == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}

	// platform_admin 可访问所有 run
	if actor.HasPermission(auth.PermPlatformAdmin) {
		return true
	}

	denyReason := "owner_mismatch"
	if actor.AccountID != run.AccountID {
		denyReason = "account_mismatch"
	} else if run.CreatedByUserID == nil {
		denyReason = "no_owner"
	} else if *run.CreatedByUserID == actor.UserID {
		return true
	}

	if auditWriter != nil {
		auditWriter.WriteAccessDenied(
			r.Context(),
			traceID,
			actor.AccountID,
			actor.UserID,
			action,
			"run",
			run.ID.String(),
			run.AccountID,
			run.CreatedByUserID,
			denyReason,
		)
	}

	httpkit.WriteError(
		w,
		nethttp.StatusForbidden,
		"policy.denied",
		"access denied",
		traceID,
		map[string]any{"action": action},
	)
	return false
}

func listGlobalRuns(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	runRepo *data.RunEventRepository,
	apiKeysRepo *data.APIKeysRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		isPlatformAdmin := actor.HasPermission(auth.PermPlatformAdmin)
		if !isPlatformAdmin {
			if !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
				return
			}
		}

		q := r.URL.Query()
		params := data.ListRunsParams{}

		if rawRunID := strings.TrimSpace(q.Get("run_id")); rawRunID != "" {
			parsed, err := uuid.Parse(rawRunID)
			if err == nil {
				params.RunID = &parsed
			} else {
				if !uuidPrefixRegex.MatchString(rawRunID) {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid run_id", traceID, nil)
					return
				}
				params.RunIDPrefix = &rawRunID
			}
		}

		if rawAccount := q.Get("account_id"); rawAccount != "" {
			parsed, err := uuid.Parse(rawAccount)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
				return
			}
			if !isPlatformAdmin && parsed != actor.AccountID {
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
				return
			}
			params.AccountID = &parsed
		} else if !isPlatformAdmin {
			params.AccountID = &actor.AccountID
		}

		if rawThreadID := strings.TrimSpace(q.Get("thread_id")); rawThreadID != "" {
			parsed, err := uuid.Parse(rawThreadID)
			if err == nil {
				params.ThreadID = &parsed
			} else {
				if !uuidPrefixRegex.MatchString(rawThreadID) {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid thread_id", traceID, nil)
					return
				}
				params.ThreadIDPrefix = &rawThreadID
			}
		}

		// user_id 筛选：仅 platform_admin 可跨用户过滤
		if rawUser := q.Get("user_id"); rawUser != "" {
			if !isPlatformAdmin {
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
				return
			}
			parsed, err := uuid.Parse(rawUser)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid user_id", traceID, nil)
				return
			}
			params.UserID = &parsed
		}

		if rawPR := q.Get("parent_run_id"); rawPR != "" {
			parsed, err := uuid.Parse(rawPR)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid parent_run_id", traceID, nil)
				return
			}
			params.ParentRunID = &parsed
		}

		if v := q.Get("status"); v != "" {
			v = strings.TrimSpace(v)
			params.Status = &v
		}
		if v := strings.TrimSpace(q.Get("model")); v != "" {
			params.Model = &v
		}
		if v := strings.TrimSpace(q.Get("persona_id")); v != "" {
			params.PersonaID = &v
		}
		if v := q.Get("since"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid since: must be RFC3339", traceID, nil)
				return
			}
			params.Since = &t
		}
		if v := q.Get("until"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid until: must be RFC3339", traceID, nil)
				return
			}
			params.Until = &t
		}
		if params.Since != nil && params.Until != nil && params.Since.After(*params.Until) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "since must be <= until", traceID, nil)
			return
		}
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 200 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "limit must be 1-200", traceID, nil)
				return
			}
			params.Limit = n
		}
		if v := q.Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "offset must be >= 0", traceID, nil)
				return
			}
			params.Offset = n
		}

		runs, total, err := runRepo.ListRuns(r.Context(), params)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]globalRunResponse, 0, len(runs))
		for _, rw := range runs {
			item := globalRunResponse{
				RunID:             rw.ID.String(),
				AccountID:             rw.AccountID.String(),
				ThreadID:          rw.ThreadID.String(),
				Status:            rw.Status,
				Model:             rw.Model,
				PersonaID:         rw.PersonaID,
				TotalInputTokens:  rw.TotalInputTokens,
				TotalOutputTokens: rw.TotalOutputTokens,
				TotalCostUSD:      rw.TotalCostUSD,
				DurationMs:        rw.DurationMs,
				CacheHitRate:      calcCacheHitRate(rw.TotalInputTokens, rw.CacheReadTokens, rw.CacheCreationTokens, rw.CachedTokens),
				CreditsUsed:       rw.CreditsUsed,
				CreatedAt:         rw.CreatedAt.UTC().Format(time.RFC3339Nano),
				CreatedByUserName: rw.UserUsername,
				CreatedByEmail:    rw.UserEmail,
			}
			if rw.CreatedByUserID != nil {
				s := rw.CreatedByUserID.String()
				item.CreatedByUserID = &s
			}
			if rw.ParentRunID != nil {
				s := rw.ParentRunID.String()
				item.ParentRunID = &s
			}
			if rw.CompletedAt != nil {
				s := rw.CompletedAt.UTC().Format(time.RFC3339Nano)
				item.CompletedAt = &s
			}
			if rw.FailedAt != nil {
				s := rw.FailedAt.UTC().Format(time.RFC3339Nano)
				item.FailedAt = &s
			}
			resp = append(resp, item)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{
			"data":  resp,
			"total": total,
		})
	}
}

// calcCacheHitRate 计算命中率（0-1），无 cache 时返回 nil。
// Anthropic: cacheRead / (input + cacheRead + cacheCreation)
// OpenAI: cachedTokens / input（input 含 cached）
// 混合 provider cache 字段并存时返回 nil，避免误导。
func calcCacheHitRate(inputTokens, cacheRead, cacheCreation, cachedTokens *int64) *float64 {
	hasAnthropic := (cacheRead != nil && *cacheRead > 0) || (cacheCreation != nil && *cacheCreation > 0)
	hasOpenAI := cachedTokens != nil && *cachedTokens > 0

	if hasAnthropic && hasOpenAI {
		return nil
	}
	if hasAnthropic {
		total := 0.0
		if inputTokens != nil {
			total += float64(*inputTokens)
		}
		if cacheRead != nil {
			total += float64(*cacheRead)
		}
		if cacheCreation != nil {
			total += float64(*cacheCreation)
		}
		if total <= 0 {
			return nil
		}
		read := 0.0
		if cacheRead != nil {
			read = float64(*cacheRead)
		}
		r := read / total
		return &r
	}
	if hasOpenAI && inputTokens != nil && *inputTokens > 0 {
		r := float64(*cachedTokens) / float64(*inputTokens)
		return &r
	}
	return nil
}
