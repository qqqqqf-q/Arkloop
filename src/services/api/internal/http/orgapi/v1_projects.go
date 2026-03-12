package orgapi

import (
	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/featureflag"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type projectResponse struct {
	ID          string  `json:"id"`
	OrgID       string  `json:"org_id"`
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
	membershipRepo *data.OrgMembershipRepository,
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
	membershipRepo *data.OrgMembershipRepository,
	projectRepo *data.ProjectRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	store environmentStore,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		projectIDRaw, subpath, _ := strings.Cut(tail, "/")
		projectIDRaw = strings.TrimSpace(projectIDRaw)
		subpath = strings.Trim(strings.TrimSpace(subpath), "/")

		projectID, err := uuid.Parse(projectIDRaw)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid project id", traceID, nil)
			return
		}

		if subpath != "" {
			handleProjectWorkspaceRoute(w, r, traceID, subpath, projectID, authService, membershipRepo, projectRepo, apiKeysRepo, auditWriter, pool, store, flagService)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getProject(w, r, traceID, projectID, authService, membershipRepo, projectRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createProject(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
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

	// 验证 team_id 归属于同一 org
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
			if team == nil || team.OrgID != actor.OrgID {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "team not found in org", traceID, nil)
				return
			}
		}
		teamID = &tid
	}

	project, err := projectRepo.Create(r.Context(), actor.OrgID, teamID, req.Name, req.Description, visibility)
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
	membershipRepo *data.OrgMembershipRepository,
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

	projects, err := projectRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
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
	membershipRepo *data.OrgMembershipRepository,
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
	if project == nil || project.OrgID != actor.OrgID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "projects.not_found", "project not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toProjectResponse(*project))
}

func toProjectResponse(p data.Project) projectResponse {
	resp := projectResponse{
		ID:          p.ID.String(),
		OrgID:       p.OrgID.String(),
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
