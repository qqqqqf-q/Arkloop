package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/http/featuregate"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type createThreadRequest struct {
	Title     *string      `json:"title"`
	IsPrivate bool         `json:"is_private"`
	Mode      *string      `json:"mode"`
	ProjectID optionalUUID `json:"project_id"`
}

type updateThreadRequest struct {
	Title     optionalString `json:"title"`
	ProjectID optionalUUID   `json:"project_id"`
}

type threadResponse struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	CreatedByUserID *string `json:"created_by_user_id"`
	Mode            string  `json:"mode"`
	Title           *string `json:"title"`
	ProjectID       *string `json:"project_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	ActiveRunID     *string `json:"active_run_id"`
	IsPrivate       bool    `json:"is_private"`
	ParentThreadID  *string `json:"parent_thread_id,omitempty"`
}

type optionalString struct {
	Present bool
	Value   *string
}

func (s *optionalString) UnmarshalJSON(raw []byte) error {
	s.Present = true
	if string(raw) == "null" {
		s.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	s.Value = &value
	return nil
}

type optionalUUID struct {
	Present bool
	Value   *uuid.UUID
}

func isTitleOnlyThreadUpdate(body updateThreadRequest) bool {
	return body.Title.Present && !body.ProjectID.Present
}

func (u *optionalUUID) UnmarshalJSON(raw []byte) error {
	u.Present = true
	if string(raw) == "null" {
		u.Value = nil
		return nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	u.Value = &parsed
	return nil
}

func createThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	projectRepo *data.ProjectRepository,
	pool *pgxpool.Pool,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}
		if projectRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		var body createThreadRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		if body.Title != nil && len(*body.Title) > 200 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		mode, ok := parseThreadMode(w, traceID, body.Mode)
		if !ok {
			return
		}
		if !featuregate.EnsureClawEnabledForMode(w, traceID, r.Context(), mode, flagService) {
			return
		}
		if body.ProjectID.Present && body.ProjectID.Value == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context()) //nolint:errcheck

		txProjectRepo, err := data.NewProjectRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		txThreadRepo, err := data.NewThreadRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		var projectID uuid.UUID
		if body.ProjectID.Present {
			project, err := txProjectRepo.GetByID(r.Context(), *body.ProjectID.Value)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if project == nil || project.OrgID != actor.OrgID {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "project not found in org", traceID, nil)
				return
			}
			projectID = project.ID
		} else {
			project, err := txProjectRepo.GetOrCreateDefaultByOwner(r.Context(), actor.OrgID, actor.UserID)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			projectID = project.ID
		}

		thread, err := txThreadRepo.Create(r.Context(), actor.OrgID, &actor.UserID, projectID, mode, body.Title, body.IsPrivate)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toThreadResponse(thread))
	}
}

func listThreads(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
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
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		beforeCreatedAt, beforeID, ok := parseThreadCursor(w, traceID, r.URL.Query())
		if !ok {
			return
		}

		mode, ok := parseThreadModeQuery(w, traceID, r.URL.Query().Get("mode"))
		if !ok {
			return
		}
		if mode != nil && !featuregate.EnsureClawEnabledForMode(w, traceID, r.Context(), *mode, flagService) {
			return
		}

		threads, err := threadRepo.ListByOwner(
			r.Context(),
			actor.OrgID,
			actor.UserID,
			mode,
			limit,
			beforeCreatedAt,
			beforeID,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]threadResponse, 0, len(threads))
		for _, item := range threads {
			resp = append(resp, toThreadWithActiveRunResponse(item))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func getThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
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

		if !authorizeThreadReadOrAudit(w, r, traceID, actor, "threads.get", thread, projectRepo, teamRepo, auditWriter, flagService) {
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toThreadResponse(*thread))
	}
}

func patchThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	projectRepo *data.ProjectRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		var body updateThreadRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if !body.Title.Present && !body.ProjectID.Present {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Title.Present && body.Title.Value != nil && len(*body.Title.Value) > 200 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.ProjectID.Present && body.ProjectID.Value == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		params := data.ThreadUpdateFields{
			SetTitle:       body.Title.Present,
			Title:          body.Title.Value,
			SetTitleLocked: body.Title.Present,
			TitleLocked:    body.Title.Present,
			SetProjectID:   body.ProjectID.Present,
			ProjectID:      body.ProjectID.Value,
		}

		if isTitleOnlyThreadUpdate(body) {
			updated, err := threadRepo.UpdateFieldsOwned(r.Context(), threadID, actor.OrgID, actor.UserID, params)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if updated != nil {
				httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toThreadResponse(*updated))
				return
			}
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.update", thread, auditWriter, flagService) {
			return
		}

		// 验证新的 project_id 归属于同一 org；projectRepo 必须可用才能做 org 隔离校验
		if body.ProjectID.Present && body.ProjectID.Value != nil {
			if projectRepo == nil {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}
			project, err := projectRepo.GetByID(r.Context(), *body.ProjectID.Value)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if project == nil || project.OrgID != actor.OrgID {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "project not found in org", traceID, nil)
				return
			}
		}

		// 原子更新：单条 SQL 同时写多个字段，避免局部写入
		// 用户手动设置标题时同时锁定，防止 Worker 自动标题覆盖
		updated, err := threadRepo.UpdateFieldsOwned(r.Context(), threadID, actor.OrgID, actor.UserID, params)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if updated == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toThreadResponse(*updated))
	}
}

func deleteThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		deleted, err := threadRepo.DeleteOwnedReturning(r.Context(), threadID, actor.OrgID, actor.UserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if deleted != nil {
			if auditWriter != nil {
				auditWriter.WriteThreadDeleted(r.Context(), traceID, actor.OrgID, actor.UserID, deleted.ID)
			}
			w.WriteHeader(nethttp.StatusNoContent)
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.delete", thread, auditWriter, flagService) {
			return
		}

		deleted, err = threadRepo.DeleteOwnedReturning(r.Context(), threadID, actor.OrgID, actor.UserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if deleted == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteThreadDeleted(r.Context(), traceID, actor.OrgID, actor.UserID, deleted.ID)
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func threadsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	projectRepo *data.ProjectRepository,
	pool *pgxpool.Pool,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	create := createThread(authService, membershipRepo, threadRepo, projectRepo, pool, apiKeysRepo, auditWriter, flagService)
	list := listThreads(authService, membershipRepo, threadRepo, apiKeysRepo, auditWriter, flagService)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			create(w, r)
		case nethttp.MethodGet:
			list(w, r)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func searchThreads(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
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
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "q is required", traceID, nil)
			return
		}
		if len(q) > 200 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "q too long", traceID, nil)
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		mode, ok := parseThreadModeQuery(w, traceID, r.URL.Query().Get("mode"))
		if !ok {
			return
		}
		if mode != nil && !featuregate.EnsureClawEnabledForMode(w, traceID, r.Context(), *mode, flagService) {
			return
		}

		threads, err := threadRepo.SearchByQuery(r.Context(), actor.OrgID, actor.UserID, mode, q, limit)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]threadResponse, 0, len(threads))
		for _, item := range threads {
			resp = append(resp, toThreadWithActiveRunResponse(item))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

type forkThreadRequest struct {
	MessageID string `json:"message_id"`
	IsPrivate *bool  `json:"is_private,omitempty"`
}

type forkThreadResponse struct {
	threadResponse
	IDMapping []idMappingPair `json:"id_mapping,omitempty"`
}

type idMappingPair struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}

func forkThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
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
		if threadRepo == nil || messageRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		var body forkThreadRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		messageID, err := uuid.Parse(body.MessageID)
		if err != nil || messageID == uuid.Nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid message_id", traceID, nil)
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.fork", thread, auditWriter, flagService) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context()) //nolint:errcheck

		txThreadRepo, err := data.NewThreadRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		txMessageRepo, err := data.NewMessageRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		isPrivate := thread.IsPrivate
		if body.IsPrivate != nil {
			isPrivate = *body.IsPrivate
		}

		forked, err := txThreadRepo.Fork(r.Context(), actor.OrgID, &actor.UserID, threadID, messageID, isPrivate)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		copied, err := txMessageRepo.CopyUpTo(r.Context(), actor.OrgID, threadID, forked.ID, messageID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if len(copied) == 0 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "no messages to copy", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		mapping := make([]idMappingPair, len(copied))
		for i, p := range copied {
			mapping[i] = idMappingPair{OldID: p.OldID.String(), NewID: p.NewID.String()}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, forkThreadResponse{
			threadResponse: toThreadResponse(forked),
			IDMapping:      mapping,
		})
	}
}

func threadEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	threadStarRepo *data.ThreadStarRepository,
	threadShareRepo *data.ThreadShareRepository,
	threadReportRepo *data.ThreadReportRepository,
	messageRepo *data.MessageRepository,
	runRepo *data.RunEventRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	apiKeysRepo *data.APIKeysRepository,
	runLimiter *data.RunLimiter,
	entSvc *entitlement.Service,
	rdb *redis.Client,
	attachmentStore messageAttachmentStore,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getThread(authService, membershipRepo, threadRepo, projectRepo, teamRepo, auditWriter, apiKeysRepo, flagService)
	patch := patchThread(authService, membershipRepo, threadRepo, projectRepo, auditWriter, apiKeysRepo, flagService)
	del := deleteThread(authService, membershipRepo, threadRepo, auditWriter, apiKeysRepo, flagService)
	createMessage := createThreadMessage(authService, membershipRepo, threadRepo, messageRepo, auditWriter, apiKeysRepo, flagService)
	listMessages := listThreadMessages(authService, membershipRepo, threadRepo, messageRepo, auditWriter, apiKeysRepo, flagService)
	createRun := createThreadRun(authService, membershipRepo, threadRepo, auditWriter, pool, apiKeysRepo, runLimiter, entSvc, rdb, flagService)
	listRuns := listThreadRuns(authService, membershipRepo, threadRepo, runRepo, auditWriter, apiKeysRepo, flagService)
	retry := retryThread(authService, membershipRepo, threadRepo, messageRepo, auditWriter, pool, apiKeysRepo, flagService)
	editMessage := editThreadMessage(authService, membershipRepo, threadRepo, messageRepo, auditWriter, pool, apiKeysRepo, flagService)
	share := shareEntry(authService, membershipRepo, threadRepo, threadShareRepo, messageRepo, auditWriter, apiKeysRepo, flagService)
	report := reportEntry(authService, membershipRepo, threadRepo, threadReportRepo, auditWriter, apiKeysRepo, flagService)
	fork := forkThread(authService, membershipRepo, threadRepo, messageRepo, auditWriter, pool, apiKeysRepo, flagService)
	uploadAttachment := uploadThreadAttachment(authService, membershipRepo, threadRepo, auditWriter, apiKeysRepo, attachmentStore, flagService)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/v1/threads/" {
			threadsEntry(authService, membershipRepo, threadRepo, projectRepo, pool, apiKeysRepo, auditWriter, flagService)(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/threads/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		// split into at most two segments: {uuid}[:action] and optional sub-resource
		parts := strings.SplitN(tail, "/", 2)
		idPart, actionPart, hasAction := strings.Cut(parts[0], ":")

		threadID, err := uuid.Parse(idPart)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		if hasAction {
			switch actionPart {
			case "retry":
				if r.Method != nethttp.MethodPost {
					httpkit.WriteMethodNotAllowed(w, r)
					return
				}
				retry(w, r, threadID)
			case "star":
				handleThreadStar(w, r, traceID, authService, membershipRepo, threadRepo, threadStarRepo, apiKeysRepo, auditWriter, threadID, flagService)
			case "share":
				share(w, r, threadID)
			case "report":
				report(w, r, threadID)
			case "fork":
				fork(w, r, threadID)
			default:
				httpkit.WriteNotFound(w, r)
			}
			return
		}

		if len(parts) == 1 {
			switch r.Method {
			case nethttp.MethodGet:
				get(w, r, threadID)
			case nethttp.MethodPatch:
				patch(w, r, threadID)
			case nethttp.MethodDelete:
				del(w, r, threadID)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
			return
		}

		// sub-resource dispatch
		subResource, subID, hasSub := strings.Cut(parts[1], "/")
		switch subResource {
		case "messages":
			if hasSub {
				messageID, err := uuid.Parse(subID)
				if err != nil {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
					return
				}
				if r.Method != nethttp.MethodPatch {
					httpkit.WriteMethodNotAllowed(w, r)
					return
				}
				editMessage(w, r, threadID, messageID)
				return
			}
			switch r.Method {
			case nethttp.MethodPost:
				createMessage(w, r, threadID)
			case nethttp.MethodGet:
				listMessages(w, r, threadID)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
		case "runs":
			switch r.Method {
			case nethttp.MethodPost:
				createRun(w, r, threadID)
			case nethttp.MethodGet:
				listRuns(w, r, threadID)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
		case "attachments":
			switch r.Method {
			case nethttp.MethodPost:
				uploadAttachment(w, r, threadID)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
		default:
			httpkit.WriteNotFound(w, r)
		}
	}
}

func parseLimit(w nethttp.ResponseWriter, traceID string, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 50, true
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 || parsed > 200 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return 0, false
	}
	return parsed, true
}

func parseThreadMode(w nethttp.ResponseWriter, traceID string, raw *string) (data.ThreadMode, bool) {
	if raw == nil {
		return data.ThreadModeChat, true
	}
	parsed := data.ThreadMode(strings.TrimSpace(*raw))
	if !parsed.IsValid() {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return "", false
	}
	return parsed, true
}

func parseThreadModeQuery(w nethttp.ResponseWriter, traceID string, raw string) (*data.ThreadMode, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, true
	}
	parsed := data.ThreadMode(trimmed)
	if !parsed.IsValid() {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return nil, false
	}
	return &parsed, true
}

func parseThreadCursor(
	w nethttp.ResponseWriter,
	traceID string,
	values url.Values,
) (*time.Time, *uuid.UUID, bool) {
	beforeCreatedAtRaw := strings.TrimSpace(first(values, "before_created_at"))
	beforeIDRaw := strings.TrimSpace(first(values, "before_id"))

	if (beforeCreatedAtRaw == "") != (beforeIDRaw == "") {
		httpkit.WriteError(
			w,
			nethttp.StatusUnprocessableEntity,
			"validation.error",
			"request validation failed",
			traceID,
			map[string]any{"reason": "cursor_incomplete", "required": []string{"before_created_at", "before_id"}},
		)
		return nil, nil, false
	}
	if beforeCreatedAtRaw == "" {
		return nil, nil, true
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, beforeCreatedAtRaw)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return nil, nil, false
	}
	parsedID, err := uuid.Parse(beforeIDRaw)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return nil, nil, false
	}

	return &parsedTime, &parsedID, true
}

func first(values url.Values, key string) string {
	raw := values[key]
	if len(raw) == 0 {
		return ""
	}
	return raw[0]
}

func authorizeThreadOrAudit(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	action string,
	thread *data.Thread,
	auditWriter *audit.Writer,
	flagService *featureflag.Service,
) bool {
	if actor == nil || thread == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if !featuregate.EnsureClawEnabledForThread(w, traceID, r.Context(), thread, flagService) {
		return false
	}

	denyReason := "owner_mismatch"
	if actor.OrgID != thread.OrgID {
		denyReason = "org_mismatch"
	} else if thread.CreatedByUserID == nil {
		denyReason = "no_owner"
	} else if *thread.CreatedByUserID == actor.UserID {
		return true
	}

	if auditWriter != nil {
		auditWriter.WriteAccessDenied(
			r.Context(),
			traceID,
			actor.OrgID,
			actor.UserID,
			action,
			"thread",
			thread.ID.String(),
			thread.OrgID,
			thread.CreatedByUserID,
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

// authorizeThreadReadOrAudit 用于只读操作，额外检查 project 级别的可见性。
// 优先级：自己是创建者 > project visibility 规则 > 拒绝。
func authorizeThreadReadOrAudit(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	action string,
	thread *data.Thread,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	auditWriter *audit.Writer,
	flagService *featureflag.Service,
) bool {
	if actor == nil || thread == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if !featuregate.EnsureClawEnabledForThread(w, traceID, r.Context(), thread, flagService) {
		return false
	}

	if actor.OrgID != thread.OrgID {
		if auditWriter != nil {
			auditWriter.WriteAccessDenied(r.Context(), traceID, actor.OrgID, actor.UserID,
				action, "thread", thread.ID.String(), thread.OrgID, thread.CreatedByUserID, "org_mismatch")
		}
		httpkit.WriteError(w, nethttp.StatusForbidden, "policy.denied", "access denied", traceID, map[string]any{"action": action})
		return false
	}

	// 创建者直接允许
	if thread.CreatedByUserID != nil && *thread.CreatedByUserID == actor.UserID {
		return true
	}

	// 通过 project visibility 授权
	if thread.ProjectID != nil && projectRepo != nil {
		project, err := projectRepo.GetByID(r.Context(), *thread.ProjectID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return false
		}
		if project != nil {
			switch project.Visibility {
			case "org":
				return true
			case "team":
				if project.TeamID != nil && teamRepo != nil {
					isMember, err := teamRepo.IsMember(r.Context(), *project.TeamID, actor.UserID)
					if err != nil {
						httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
						return false
					}
					if isMember {
						return true
					}
				}
			}
		}
	}

	denyReason := "owner_mismatch"
	if thread.CreatedByUserID == nil {
		denyReason = "no_owner"
	}
	if auditWriter != nil {
		auditWriter.WriteAccessDenied(r.Context(), traceID, actor.OrgID, actor.UserID,
			action, "thread", thread.ID.String(), thread.OrgID, thread.CreatedByUserID, denyReason)
	}
	httpkit.WriteError(w, nethttp.StatusForbidden, "policy.denied", "access denied", traceID, map[string]any{"action": action})
	return false
}

func toThreadResponse(thread data.Thread) threadResponse {
	var createdByUserID *string
	if thread.CreatedByUserID != nil {
		value := thread.CreatedByUserID.String()
		createdByUserID = &value
	}
	var projectID *string
	if thread.ProjectID != nil {
		value := thread.ProjectID.String()
		projectID = &value
	}
	var parentThreadID *string
	if thread.ParentThreadID != nil {
		value := thread.ParentThreadID.String()
		parentThreadID = &value
	}
	return threadResponse{
		ID:              thread.ID.String(),
		OrgID:           thread.OrgID.String(),
		CreatedByUserID: createdByUserID,
		Mode:            string(thread.Mode),
		Title:           thread.Title,
		ProjectID:       projectID,
		CreatedAt:       thread.CreatedAt.UTC().Format(time.RFC3339Nano),
		ActiveRunID:     nil,
		IsPrivate:       thread.IsPrivate,
		ParentThreadID:  parentThreadID,
	}
}

func toThreadWithActiveRunResponse(item data.ThreadWithActiveRun) threadResponse {
	resp := toThreadResponse(item.Thread)
	if item.ActiveRunID != nil {
		value := item.ActiveRunID.String()
		resp.ActiveRunID = &value
	}
	return resp
}

// handleThreadStar 处理 POST /v1/threads/{id}:star（收藏）和 DELETE /v1/threads/{id}:star（取消收藏）。
func handleThreadStar(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	threadStarRepo *data.ThreadStarRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	threadID uuid.UUID,
	flagService *featureflag.Service,
) {
	if r.Method != nethttp.MethodPost && r.Method != nethttp.MethodDelete {
		httpkit.WriteMethodNotAllowed(w, r)
		return
	}
	if threadStarRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
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
	if thread.OrgID != actor.OrgID {
		httpkit.WriteError(w, nethttp.StatusForbidden, "policy.denied", "access denied", traceID, nil)
		return
	}
	if !featuregate.EnsureClawEnabledForThread(w, traceID, r.Context(), thread, flagService) {
		return
	}

	if r.Method == nethttp.MethodPost {
		if err := threadStarRepo.Star(r.Context(), actor.UserID, threadID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	} else {
		if err := threadStarRepo.Unstar(r.Context(), actor.UserID, threadID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

// listStarredThreads 处理 GET /v1/threads/starred，返回当前用户收藏的 thread ID 列表。
func listStarredThreads(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadStarRepo *data.ThreadStarRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if threadStarRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		ids, err := threadStarRepo.ListByUser(r.Context(), actor.UserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 返回字符串 ID 列表，空时返回空数组而非 null
		result := make([]string, 0, len(ids))
		for _, id := range ids {
			result = append(result, id.String())
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, result)
	}
}
