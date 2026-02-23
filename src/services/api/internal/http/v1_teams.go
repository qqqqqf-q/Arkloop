package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type teamResponse struct {
	ID           string `json:"id"`
	OrgID        string `json:"org_id"`
	Name         string `json:"name"`
	MembersCount int64  `json:"members_count"`
	CreatedAt    string `json:"created_at"`
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
	entSvc *entitlement.Service,
	pool *pgxpool.Pool,
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
	entSvc *entitlement.Service,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/teams/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			teamsEntry(authService, membershipRepo, teamRepo, apiKeysRepo, entSvc, pool)(w, r)
			return
		}

		// /{team_id} or /{team_id}/members or /{team_id}/members/{user_id}
		parts := strings.SplitN(tail, "/", 3)
		teamID, err := uuid.Parse(parts[0])
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid team id", traceID, nil)
			return
		}

		// DELETE /v1/teams/{id}
		if len(parts) == 1 {
			if r.Method != nethttp.MethodDelete {
				writeMethodNotAllowed(w, r)
				return
			}
			deleteTeam(w, r, traceID, teamID, authService, membershipRepo, teamRepo, apiKeysRepo)
			return
		}

		if parts[1] != "members" {
			writeNotFound(w, r)
			return
		}

		// GET/POST /v1/teams/{id}/members
		if len(parts) == 2 {
			switch r.Method {
			case nethttp.MethodGet:
				listTeamMembers(w, r, traceID, teamID, authService, membershipRepo, teamRepo, apiKeysRepo)
			case nethttp.MethodPost:
				addTeamMember(w, r, traceID, teamID, authService, membershipRepo, teamRepo, apiKeysRepo, entSvc, pool)
			default:
				writeMethodNotAllowed(w, r)
			}
			return
		}

		// DELETE /v1/teams/{id}/members/{user_id}
		if len(parts) == 3 && r.Method == nethttp.MethodDelete {
			userID, err := uuid.Parse(parts[2])
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid user id", traceID, nil)
				return
			}
			removeTeamMember(w, r, traceID, teamID, userID, authService, membershipRepo, teamRepo, apiKeysRepo)
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

	teams, err := teamRepo.ListByOrgWithCounts(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]teamResponse, 0, len(teams))
	for _, t := range teams {
		resp = append(resp, toTeamWithCountResponse(t))
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
	entSvc *entitlement.Service,
	pool *pgxpool.Pool,
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

	// 确保被添加的用户确实是同一 org 的成员，防止跨 org 数据注入
	if membershipRepo != nil {
		exists, err := membershipRepo.ExistsForOrgAndUser(r.Context(), actor.OrgID, userID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if !exists {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "user is not a member of this org", traceID, nil)
			return
		}
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "member"
	}

	// 在事务内锁定 team 行，避免并发 addTeamMember 导致软配额超额
	if pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	// 行级锁：防止同一 team 的并发添加操作同时通过配额检查
	if _, err := tx.Exec(r.Context(), `SELECT id FROM teams WHERE id = $1 FOR UPDATE`, teamID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	txTeamRepo := teamRepo.WithTx(tx)
	currentCount, err := txTeamRepo.CountMembers(r.Context(), teamID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !requireEntitlementInt(r.Context(), w, traceID, entSvc, actor.OrgID, "limit.team_members", currentCount, "quota.team_members_exceeded", "team member limit reached") {
		return
	}

	membership, err := txTeamRepo.AddMember(r.Context(), teamID, userID, role)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
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

func toTeamWithCountResponse(t data.TeamWithCount) teamResponse {
	return teamResponse{
		ID:           t.ID.String(),
		OrgID:        t.OrgID.String(),
		Name:         t.Name,
		MembersCount: t.MembersCount,
		CreatedAt:    t.CreatedAt.UTC().Format(time.RFC3339Nano),
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

func listTeamMembers(
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
	if !requirePerm(actor, auth.PermOrgTeamsRead, w, traceID) {
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

	members, err := teamRepo.ListMembers(r.Context(), teamID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]teamMemberResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, toTeamMemberResponse(m))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func removeTeamMember(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	teamID, userID uuid.UUID,
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

	if err := teamRepo.RemoveMember(r.Context(), teamID, userID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

func deleteTeam(
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

	if err := teamRepo.Delete(r.Context(), actor.OrgID, teamID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}
