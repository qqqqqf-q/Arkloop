package accountapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/http/featuregate"
	"arkloop/services/shared/environmentref"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errProjectWorkspaceNotFound = errors.New("project workspace not found")

type projectWorkspaceStatus string

const (
	projectWorkspaceStatusActive      projectWorkspaceStatus = "active"
	projectWorkspaceStatusIdle        projectWorkspaceStatus = "idle"
	projectWorkspaceStatusUnavailable projectWorkspaceStatus = "unavailable"
)

type projectWorkspaceResponse struct {
	ProjectID     string                                 `json:"project_id"`
	WorkspaceRef  string                                 `json:"workspace_ref"`
	OwnerUserID   string                                 `json:"owner_user_id"`
	Status        projectWorkspaceStatus                 `json:"status"`
	LastUsedAt    string                                 `json:"last_used_at"`
	ActiveSession *projectWorkspaceActiveSessionResponse `json:"active_session,omitempty"`
}

type projectWorkspaceActiveSessionResponse struct {
	SessionRef  string `json:"session_ref"`
	SessionType string `json:"session_type"`
	State       string `json:"state"`
	LastUsedAt  string `json:"last_used_at"`
}

type projectWorkspaceFilesResponse struct {
	WorkspaceRef string                         `json:"workspace_ref"`
	Path         string                         `json:"path"`
	Items        []projectWorkspaceFileListItem `json:"items"`
}

type projectWorkspaceFileListItem struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Type        string  `json:"type"`
	Size        *int64  `json:"size,omitempty"`
	MtimeUnixMs *int64  `json:"mtime_unix_ms,omitempty"`
	MimeType    *string `json:"mime_type,omitempty"`
	HasChildren bool    `json:"has_children,omitempty"`
}

type resolvedProjectWorkspace struct {
	Project      data.Project
	ProfileRef   string
	WorkspaceRef string
	Registry     *data.WorkspaceRegistry
	Session      *data.ShellSession
	Status       projectWorkspaceStatus
}

func handleProjectWorkspaceRoute(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	subpath string,
	projectID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	db data.DB,
	store environmentStore,
	flagService *featureflag.Service,
) {
	parts := strings.Split(strings.Trim(subpath, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		httpkit.WriteNotFound(w, r)
		return
	}
	if parts[0] != "workspace" {
		httpkit.WriteNotFound(w, r)
		return
	}

	switch {
	case len(parts) == 1 && r.Method == nethttp.MethodGet:
		getProjectWorkspace(w, r, traceID, projectID, authService, membershipRepo, projectRepo, apiKeysRepo, auditWriter, db, flagService)
	case len(parts) == 2 && parts[1] == "files" && r.Method == nethttp.MethodGet:
		listProjectWorkspaceFiles(w, r, traceID, projectID, authService, membershipRepo, projectRepo, apiKeysRepo, auditWriter, db, store, flagService)
	case len(parts) == 2 && parts[1] == "file" && r.Method == nethttp.MethodGet:
		getProjectWorkspaceFile(w, r, traceID, projectID, authService, membershipRepo, projectRepo, apiKeysRepo, auditWriter, db, store, flagService)
	default:
		httpkit.WriteMethodNotAllowed(w, r)
	}
}

func getProjectWorkspace(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	projectID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	db data.DB,
	flagService *featureflag.Service,
) {
	if !featuregate.EnsureClawEnabled(w, traceID, r.Context(), flagService) {
		return
	}
	actor, ok := resolveProjectWorkspaceActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter, true)
	if !ok {
		return
	}
	resolved, ok := resolveProjectWorkspaceForActor(w, r, traceID, db, projectRepo, projectID, actor)
	if !ok {
		return
	}
	if resolved.Registry == nil || resolved.Registry.OwnerUserID == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "workspaces.not_found", "workspace not found", traceID, nil)
		return
	}

	resp := projectWorkspaceResponse{
		ProjectID:    resolved.Project.ID.String(),
		WorkspaceRef: resolved.WorkspaceRef,
		OwnerUserID:  resolved.Registry.OwnerUserID.String(),
		Status:       resolved.Status,
		LastUsedAt:   resolved.Registry.LastUsedAt.UTC().Format(time.RFC3339Nano),
	}
	if resolved.Session != nil && resolved.Status == projectWorkspaceStatusActive {
		resp.ActiveSession = &projectWorkspaceActiveSessionResponse{
			SessionRef:  resolved.Session.SessionRef,
			SessionType: resolved.Session.SessionType,
			State:       resolved.Session.State,
			LastUsedAt:  resolved.Session.LastUsedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func listProjectWorkspaceFiles(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	projectID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	db data.DB,
	store environmentStore,
	flagService *featureflag.Service,
) {
	if store == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "workspace_files.not_configured", "workspace file storage not configured", traceID, nil)
		return
	}
	if !featuregate.EnsureClawEnabled(w, traceID, r.Context(), flagService) {
		return
	}
	actor, ok := resolveProjectWorkspaceActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter, false)
	if !ok {
		return
	}
	resolved, ok := resolveProjectWorkspaceForActor(w, r, traceID, db, projectRepo, projectID, actor)
	if !ok {
		return
	}
	relativePath, ok := normalizeWorkspaceDirectoryPath(w, traceID, r.URL.Query().Get("path"))
	if !ok {
		return
	}

	items, err := listWorkspaceManifestEntries(r.Context(), db, store, resolved.WorkspaceRef, relativePath)
	if err != nil {
		if errors.Is(err, errWorkspaceFileNotFound) {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, projectWorkspaceFilesResponse{
				WorkspaceRef: resolved.WorkspaceRef,
				Path:         displayWorkspacePath(relativePath),
				Items:        []projectWorkspaceFileListItem{},
			})
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, projectWorkspaceFilesResponse{
		WorkspaceRef: resolved.WorkspaceRef,
		Path:         displayWorkspacePath(relativePath),
		Items:        items,
	})
}

func getProjectWorkspaceFile(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	projectID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	db data.DB,
	store environmentStore,
	flagService *featureflag.Service,
) {
	if store == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "workspace_files.not_configured", "workspace file storage not configured", traceID, nil)
		return
	}
	if !featuregate.EnsureClawEnabled(w, traceID, r.Context(), flagService) {
		return
	}
	actor, ok := resolveProjectWorkspaceActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter, false)
	if !ok {
		return
	}
	resolved, ok := resolveProjectWorkspaceForActor(w, r, traceID, db, projectRepo, projectID, actor)
	if !ok {
		return
	}
	relativePath, ok := normalizeWorkspaceRelativePath(w, traceID, r.URL.Query().Get("path"))
	if !ok {
		return
	}

	content, contentType, err := readWorkspaceFile(r.Context(), db, store, resolved.WorkspaceRef, relativePath)
	if err != nil {
		if errors.Is(err, errWorkspaceFileNotFound) {
			httpkit.WriteError(w, nethttp.StatusNotFound, "workspace_files.not_found", "workspace file not found", traceID, nil)
			return
		}
		httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write(content)
}

func resolveProjectWorkspaceActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	workspaceOnly bool,
) (*httpkit.Actor, bool) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return nil, false
	}
	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
	if !ok {
		return nil, false
	}
	if !httpkit.RequirePerm(actor, auth.PermDataProjectsRead, w, traceID) {
		return nil, false
	}
	if !workspaceOnly && !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
		return nil, false
	}
	return actor, true
}

func resolveProjectWorkspaceForActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	db data.DB,
	projectRepo *data.ProjectRepository,
	projectID uuid.UUID,
	actor *httpkit.Actor,
) (*resolvedProjectWorkspace, bool) {
	if db == nil || projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return nil, false
	}
	resolved, err := resolveProjectWorkspace(r.Context(), db, projectRepo, projectID, actor)
	if err != nil {
		if errors.Is(err, errProjectWorkspaceNotFound) {
			httpkit.WriteError(w, nethttp.StatusNotFound, "projects.not_found", "project not found", traceID, nil)
			return nil, false
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	return resolved, true
}

func resolveProjectWorkspace(
	ctx context.Context,
	db data.DB,
	projectRepo *data.ProjectRepository,
	projectID uuid.UUID,
	actor *httpkit.Actor,
) (*resolvedProjectWorkspace, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil || projectRepo == nil || actor == nil {
		return nil, fmt.Errorf("project workspace dependencies must not be nil")
	}
	project, err := projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil || project.AccountID != actor.AccountID {
		return nil, errProjectWorkspaceNotFound
	}

	profileRef := environmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	bindingsRepo, err := data.NewDefaultWorkspaceBindingsRepository(tx)
	if err != nil {
		return nil, err
	}
	profileRepo, err := data.NewProfileRegistriesRepository(tx)
	if err != nil {
		return nil, err
	}
	workspaceRepo, err := data.NewWorkspaceRegistriesRepository(tx)
	if err != nil {
		return nil, err
	}

	candidate := environmentref.BuildWorkspaceRef(actor.AccountID, profileRef, data.DefaultWorkspaceBindingScopeProject, projectID)
	workspaceRef, _, err := bindingsRepo.GetOrCreate(ctx, tx, actor.AccountID, &actor.UserID, profileRef, data.DefaultWorkspaceBindingScopeProject, projectID, candidate)
	if err != nil {
		return nil, err
	}
	if err := profileRepo.Ensure(ctx, profileRef, actor.AccountID, actor.UserID); err != nil {
		return nil, err
	}
	if err := workspaceRepo.Ensure(ctx, workspaceRef, actor.AccountID, actor.UserID, &projectID); err != nil {
		return nil, err
	}
	if err := profileRepo.SetDefaultWorkspaceRef(ctx, profileRef, workspaceRef); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	workspaceRepo, err = data.NewWorkspaceRegistriesRepository(db)
	if err != nil {
		return nil, err
	}
	sessionRepo, err := data.NewShellSessionRepository(db)
	if err != nil {
		return nil, err
	}
	registry, err := workspaceRepo.Get(ctx, workspaceRef)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return &resolvedProjectWorkspace{
			Project:      *project,
			ProfileRef:   profileRef,
			WorkspaceRef: workspaceRef,
			Status:       projectWorkspaceStatusUnavailable,
		}, nil
	}

	resolved := &resolvedProjectWorkspace{
		Project:      *project,
		ProfileRef:   profileRef,
		WorkspaceRef: workspaceRef,
		Registry:     registry,
		Status:       projectWorkspaceStatusIdle,
	}
	resolvedSession, err := sessionRepo.GetLatestLiveByWorkspaceRef(ctx, actor.AccountID, workspaceRef)
	if err != nil {
		return nil, err
	}
	if resolvedSession == nil {
		return resolved, nil
	}
	resolved.Session = resolvedSession
	resolved.Status = projectWorkspaceStatusActive
	return resolved, nil
}
