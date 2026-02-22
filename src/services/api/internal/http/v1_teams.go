package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type teamResponse struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type createTeamRequest struct {
	Name string `json:"name"`
}

type addTeamMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type teamMemberResponse struct {
	TeamID    string `json:"team_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func teamsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createTeam(w, r, authService, membershipRepo, teamRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listTeams(w, r, authService, membershipRepo, teamRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func teamEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/teams/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			teamsEntry(authService, membershipRepo, teamRepo, apiKeysRepo)(w, r)
			return
		}

		// {team_id}/members
		parts := strings.SplitN(tail, "/", 2)
		teamID, err := uuid.Parse(parts[0])
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid team id", traceID, nil)
			return
		}

		if len(parts) == 2 && parts[1] == "members" {
			if r.Method != nethttp.MethodPost {
				writeMethodNotAllowed(w, r)
				return
			}
			addTeamMember(w, r, traceID, teamID, authService, membershipRepo, teamRepo, apiKeysRepo)
			return
		}

		writeNotFound(w, r)
	}
}

func createTeam(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if teamRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermOrgTeamsManage, w, traceID) {
		return
	}

	var req createTeamRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must be 1-100 characters", traceID, nil)
		return
	}

	team, err := teamRepo.Create(r.Context(), actor.OrgID, req.Name)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toTeamResponse(team))
}

func listTeams(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if teamRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermOrgTeamsRead, w, traceID) {
		return
	}

	teams, err := teamRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]teamResponse, 0, len(teams))
	for _, t := range teams {
		resp = append(resp, toTeamResponse(t))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func addTeamMember(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	teamID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if teamRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermOrgTeamsManage, w, traceID) {
		return
	}

	team, err := teamRepo.GetByID(r.Context(), teamID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if team == nil || team.OrgID != actor.OrgID {
		WriteError(w, nethttp.StatusNotFound, "teams.not_found", "team not found", traceID, nil)
		return
	}

	var req addTeamMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	userID, err := uuid.Parse(strings.TrimSpace(req.UserID))
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid user_id", traceID, nil)
		return
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "member"
	}

	membership, err := teamRepo.AddMember(r.Context(), teamID, userID, role)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toTeamMemberResponse(membership))
}

func toTeamResponse(t data.Team) teamResponse {
	return teamResponse{
		ID:        t.ID.String(),
		OrgID:     t.OrgID.String(),
		Name:      t.Name,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toTeamMemberResponse(m data.TeamMembership) teamMemberResponse {
	return teamMemberResponse{
		TeamID:    m.TeamID.String(),
		UserID:    m.UserID.String(),
		Role:      m.Role,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
