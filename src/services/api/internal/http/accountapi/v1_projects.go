package accountapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"log/slog"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type projectResponse struct {
	ID          string  `json:"id"`
	AccountID   string  `json:"account_id"`
	TeamID      *string `json:"team_id,omitempty"`
	OwnerUserID *string `json:"owner_user_id,omitempty"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Visibility  string  `json:"visibility"`
	IsDefault   bool    `json:"is_default"`
	CreatedAt   string  `json:"created_at"`
}

type createProjectRequest struct {
	TeamID      *string `json:"team_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Visibility  string  `json:"visibility"`
}

func projectsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createProject(w, r, authService, membershipRepo, projectRepo, teamRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listProjects(w, r, authService, membershipRepo, projectRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func projectEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		projectID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid project id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getProject(w, r, traceID, projectID, authService, membershipRepo, projectRepo, apiKeysRepo)
		case nethttp.MethodDelete:
			deleteProject(w, r, traceID, projectID, authService, membershipRepo, projectRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createProject(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataProjectsManage, w, traceID) {
		return
	}

	var req createProjectRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 200 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must be 1-200 characters", traceID, nil)
		return
	}

	visibility := strings.TrimSpace(req.Visibility)
	if visibility == "" {
		visibility = "private"
	}
	if visibility != "private" && visibility != "team" && visibility != "org" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "visibility must be private, team, or org", traceID, nil)
		return
	}

	// 验证 team_id 归属于同一 account
	var teamID *uuid.UUID
	if req.TeamID != nil {
		tid, err := uuid.Parse(strings.TrimSpace(*req.TeamID))
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid team_id", traceID, nil)
			return
		}
		if teamRepo != nil {
			team, err := teamRepo.GetByID(r.Context(), tid)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if team == nil || team.AccountID != actor.AccountID {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "team not found in org", traceID, nil)
				return
			}
		}
		teamID = &tid
	}

	project, err := projectRepo.Create(r.Context(), actor.AccountID, teamID, req.Name, req.Description, visibility)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toProjectResponse(project))
}

func listProjects(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataProjectsRead, w, traceID) {
		return
	}

	projects, err := projectRepo.ListByOrg(r.Context(), actor.AccountID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 自愈: 旧账号可能缺少默认项目
	if len(projects) == 0 {
		dp, err := projectRepo.GetOrCreateDefaultByOwner(r.Context(), actor.AccountID, actor.UserID)
		if err != nil {
			slog.WarnContext(r.Context(), "projects: failed to self-heal default project", "error", err)
		} else {
			projects = []data.Project{dp}
		}
	}

	resp := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, toProjectResponse(p))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func getProject(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	projectID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataProjectsRead, w, traceID) {
		return
	}

	project, err := projectRepo.GetByID(r.Context(), projectID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if project == nil || project.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "projects.not_found", "project not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toProjectResponse(*project))
}

func deleteProject(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	projectID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataProjectsManage, w, traceID) {
		return
	}

	project, err := projectRepo.GetByID(r.Context(), projectID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if project == nil || project.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "projects.not_found", "project not found", traceID, nil)
		return
	}

	if project.IsDefault {
		httpkit.WriteError(w, nethttp.StatusForbidden, "projects.delete_default", "default project cannot be deleted", traceID, nil)
		return
	}

	if err := projectRepo.SoftDelete(r.Context(), projectID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

func toProjectResponse(p data.Project) projectResponse {
	resp := projectResponse{
		ID:          p.ID.String(),
		AccountID:   p.AccountID.String(),
		Name:        p.Name,
		Description: p.Description,
		Visibility:  p.Visibility,
		IsDefault:   p.IsDefault,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if p.TeamID != nil {
		s := p.TeamID.String()
		resp.TeamID = &s
	}
	if p.OwnerUserID != nil {
		s := p.OwnerUserID.String()
		resp.OwnerUserID = &s
	}
	return resp
}
