package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type orgInvitationResponse struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	InvitedByUserID string  `json:"invited_by_user_id"`
	Email           string  `json:"email"`
	Role            string  `json:"role"`
	ExpiresAt       string  `json:"expires_at"`
	AcceptedAt      *string `json:"accepted_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type createOrgInvitationResponse struct {
	orgInvitationResponse
	Token string `json:"token"`
}

type createOrgInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func orgsInvitationsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	invitationsRepo *data.OrgInvitationsRepository,
	auditWriter *audit.Writer,
	orgRepo *data.OrgRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/orgs/")
		tail = strings.Trim(tail, "/")

		// 期望格式: {org_id}/invitations
		parts := strings.SplitN(tail, "/", 2)
		if len(parts) != 2 || parts[1] != "invitations" {
			writeNotFound(w, r)
			return
		}

		orgID, err := uuid.Parse(parts[0])
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPost:
			createOrgInvitation(w, r, traceID, orgID, authService, membershipRepo, invitationsRepo, auditWriter, orgRepo)
		case nethttp.MethodGet:
			listOrgInvitations(w, r, traceID, orgID, authService, membershipRepo, invitationsRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func orgInvitationEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	invitationsRepo *data.OrgInvitationsRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/org-invitations/")
		tail = strings.Trim(tail, "/")

		if tail == "" {
			writeNotFound(w, r)
			return
		}

		// {token}/accept → POST accept
		if strings.HasSuffix(tail, "/accept") {
			token := strings.TrimSuffix(tail, "/accept")
			token = strings.Trim(token, "/")
			if r.Method != nethttp.MethodPost {
				writeMethodNotAllowed(w, r)
				return
			}
			acceptOrgInvitation(w, r, traceID, token, authService, membershipRepo, invitationsRepo, auditWriter, pool)
			return
		}

		// {uuid} → DELETE revoke
		invitationID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid invitation id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodDelete:
			revokeOrgInvitation(w, r, traceID, invitationID, authService, membershipRepo, invitationsRepo, auditWriter)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createOrgInvitation(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	orgID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	invitationsRepo *data.OrgInvitationsRepository,
	auditWriter *audit.Writer,
	orgRepo *data.OrgRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if invitationsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if actor.OrgID != orgID {
		WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
		return
	}
	if !requirePerm(actor, auth.PermOrgMembersInvite, w, traceID) {
		return
	}

	// personal org 不允许邀请成员
	if orgRepo != nil {
		org, err := orgRepo.GetByID(r.Context(), orgID)
		if err != nil || org == nil {
			WriteError(w, nethttp.StatusNotFound, "orgs.not_found", "org not found", traceID, nil)
			return
		}
		if org.Type != "workspace" {
			WriteError(w, nethttp.StatusBadRequest, "orgs.personal_not_invitable", "cannot invite members to a personal org", traceID, nil)
			return
		}
	}

	var req createOrgInvitationRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || len(req.Email) > 254 {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid email", traceID, nil)
		return
	}
	req.Role = strings.TrimSpace(req.Role)
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "member" && req.Role != "owner" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "role must be member or owner", traceID, nil)
		return
	}

	inv, err := invitationsRepo.Create(r.Context(), orgID, actor.UserID, req.Email, req.Role)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if auditWriter != nil {
		auditWriter.WriteOrgInvitationCreated(r.Context(), traceID, orgID, actor.UserID, inv.ID, inv.Email, inv.Role)
	}

	writeJSON(w, traceID, nethttp.StatusCreated, createOrgInvitationResponse{
		orgInvitationResponse: toOrgInvitationResponse(inv),
		Token:                 inv.Token,
	})
}

func listOrgInvitations(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	orgID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	invitationsRepo *data.OrgInvitationsRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if invitationsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if actor.OrgID != orgID {
		WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
		return
	}
	if !requirePerm(actor, auth.PermOrgMembersList, w, traceID) {
		return
	}

	invitations, err := invitationsRepo.ListActiveByOrg(r.Context(), orgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]orgInvitationResponse, 0, len(invitations))
	for _, inv := range invitations {
		resp = append(resp, toOrgInvitationResponse(inv))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func acceptOrgInvitation(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	token string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	invitationsRepo *data.OrgInvitationsRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if invitationsRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	user, ok := authenticateUser(w, r, traceID, authService)
	if !ok {
		return
	}

	// token 格式: 64位小写 hex（32字节随机数的 hex 编码，生成时全小写）
	if len(token) != 64 {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid token format", traceID, nil)
		return
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid token format", traceID, nil)
			return
		}
	}

	inv, err := invitationsRepo.GetByToken(r.Context(), token)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if inv == nil {
		WriteError(w, nethttp.StatusNotFound, "invitations.not_found", "invitation not found", traceID, nil)
		return
	}
	if inv.AcceptedAt != nil {
		WriteError(w, nethttp.StatusConflict, "invitations.already_accepted", "invitation already accepted", traceID, nil)
		return
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		WriteError(w, nethttp.StatusGone, "invitations.expired", "invitation has expired", traceID, nil)
		return
	}

	if user.Email == nil || !strings.EqualFold(*user.Email, inv.Email) {
		WriteError(w, nethttp.StatusForbidden, "invitations.email_mismatch", "invitation was sent to a different email", traceID, nil)
		return
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txInvitationsRepo, err := data.NewOrgInvitationsRepository(tx)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	txMembershipRepo, err := data.NewOrgMembershipRepository(tx)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 事务内检查成员重复，减小 TOCTOU 窗口
	alreadyMember, err := txMembershipRepo.ExistsForOrgAndUser(r.Context(), inv.OrgID, user.ID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if alreadyMember {
		WriteError(w, nethttp.StatusConflict, "invitations.already_member", "user is already a member of this org", traceID, nil)
		return
	}

	accepted, err := txInvitationsRepo.MarkAccepted(r.Context(), inv.ID, user.ID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !accepted {
		// 并发场景：邀请在检查后被撤销或已被接受
		WriteError(w, nethttp.StatusConflict, "invitations.already_accepted", "invitation no longer available", traceID, nil)
		return
	}
	if _, err := txMembershipRepo.Create(r.Context(), inv.OrgID, user.ID, inv.Role); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	txNotifRepo, err := data.NewNotificationsRepository(tx)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if _, err := txNotifRepo.BackfillBroadcastsForMembership(r.Context(), user.ID, inv.OrgID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if auditWriter != nil {
		auditWriter.WriteOrgInvitationAccepted(r.Context(), traceID, inv.OrgID, user.ID, inv.ID, inv.Email)
	}

	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func revokeOrgInvitation(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	invitationID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	invitationsRepo *data.OrgInvitationsRepository,
	auditWriter *audit.Writer,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if invitationsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermOrgMembersRevoke, w, traceID) {
		return
	}

	deleted, err := invitationsRepo.Delete(r.Context(), invitationID, actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !deleted {
		WriteError(w, nethttp.StatusNotFound, "invitations.not_found", "invitation not found", traceID, nil)
		return
	}

	if auditWriter != nil {
		auditWriter.WriteOrgInvitationRevoked(r.Context(), traceID, actor.OrgID, actor.UserID, invitationID)
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

func toOrgInvitationResponse(inv data.OrgInvitation) orgInvitationResponse {
	resp := orgInvitationResponse{
		ID:              inv.ID.String(),
		OrgID:           inv.OrgID.String(),
		InvitedByUserID: inv.InvitedByUserID.String(),
		Email:           inv.Email,
		Role:            inv.Role,
		ExpiresAt:       inv.ExpiresAt.UTC().Format(time.RFC3339Nano),
		CreatedAt:       inv.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if inv.AcceptedAt != nil {
		s := inv.AcceptedAt.UTC().Format(time.RFC3339Nano)
		resp.AcceptedAt = &s
	}
	return resp
}
