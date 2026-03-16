package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

// --- response types ---

type inviteCodeResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Code      string `json:"code"`
	MaxUses   int    `json:"max_uses"`
	UseCount  int    `json:"use_count"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
}

type adminInviteCodeResponse struct {
	inviteCodeResponse
	UserLogin string  `json:"user_login"`
	UserEmail *string `json:"user_email"`
}

type referralResponse struct {
	ID            string `json:"id"`
	InviterUserID string `json:"inviter_user_id"`
	InviteeUserID string `json:"invitee_user_id"`
	InviteCodeID  string `json:"invite_code_id"`
	Credited      bool   `json:"credited"`
	CreatedAt     string `json:"created_at"`
	InviterLogin  string `json:"inviter_login"`
	InviteeLogin  string `json:"invitee_login"`
}

type referralTreeNodeResponse struct {
	UserID    string  `json:"user_id"`
	Login     string  `json:"login"`
	InviterID *string `json:"inviter_id"`
	Depth     int     `json:"depth"`
	CreatedAt string  `json:"created_at"`
}

func toInviteCodeResponse(ic data.InviteCode) inviteCodeResponse {
	return inviteCodeResponse{
		ID:        ic.ID.String(),
		UserID:    ic.UserID.String(),
		Code:      ic.Code,
		MaxUses:   ic.MaxUses,
		UseCount:  ic.UseCount,
		IsActive:  ic.IsActive,
		CreatedAt: ic.CreatedAt.Format(time.RFC3339Nano),
	}
}

// --- /v1/me/invite-code ---

func meInviteCode(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	entitlementSvc *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
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
		if inviteCodesRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		codes, err := inviteCodesRepo.ListActiveByUserID(r.Context(), actor.UserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 用户没有活跃邀请码时自动生成一个
		if len(codes) == 0 {
			maxUses := 0
			if entitlementSvc != nil {
				val, resolveErr := entitlementSvc.Resolve(r.Context(), actor.AccountID, "invite.default_max_uses")
				if resolveErr == nil {
					maxUses = data.NormalizeInviteCodeMaxUses(val.Int())
				}
			}

			code, genErr := data.GenerateCode()
			if genErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			ic, createErr := inviteCodesRepo.Create(r.Context(), actor.UserID, code, maxUses)
			if createErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			if auditWriter != nil {
				auditWriter.WriteInviteCodeCreated(r.Context(), traceID, actor.UserID, ic.ID)
			}

			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toInviteCodeResponse(*ic))
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toInviteCodeResponse(codes[0]))
	}
}

// --- /v1/me/invite-code/reset ---

const inviteCodeResetCooldown = 24 * time.Hour

func meInviteCodeReset(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	entitlementSvc *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
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
		if inviteCodesRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		codes, err := inviteCodesRepo.ListByUserID(r.Context(), actor.UserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 检查冷却时间：最近的邀请码不能在 24 小时内
		now := time.Now().UTC()
		if len(codes) > 0 && now.Sub(codes[0].CreatedAt) < inviteCodeResetCooldown {
			httpkit.WriteError(w, nethttp.StatusTooManyRequests, "invite_codes.reset_cooldown", "reset allowed once per 24 hours", traceID, nil)
			return
		}

		// 停用所有旧的活跃邀请码
		var oldCodeID uuid.UUID
		for _, c := range codes {
			if c.IsActive {
				oldCodeID = c.ID
				if _, err := inviteCodesRepo.SetActive(r.Context(), c.ID, false); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			}
		}

		maxUses := 0
		if entitlementSvc != nil {
			val, resolveErr := entitlementSvc.Resolve(r.Context(), actor.AccountID, "invite.default_max_uses")
			if resolveErr == nil {
				maxUses = data.NormalizeInviteCodeMaxUses(val.Int())
			}
		}

		code, genErr := data.GenerateCode()
		if genErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		ic, createErr := inviteCodesRepo.Create(r.Context(), actor.UserID, code, maxUses)
		if createErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteInviteCodeReset(r.Context(), traceID, actor.UserID, oldCodeID, ic.ID)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toInviteCodeResponse(*ic))
	}
}

// --- /v1/admin/invite-codes ---

func adminInviteCodesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	list := listAdminInviteCodes(authService, membershipRepo, inviteCodesRepo, apiKeysRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			list(w, r)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func listAdminInviteCodes(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformInviteCodesManage, w, traceID) {
			return
		}

		limit, ok := httpkit.ParseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}
		beforeCreatedAt, beforeID, ok := httpkit.ParseThreadCursor(w, traceID, r.URL.Query())
		if !ok {
			return
		}

		query := strings.TrimSpace(r.URL.Query().Get("q"))

		items, err := inviteCodesRepo.List(r.Context(), limit, beforeCreatedAt, beforeID, query)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]adminInviteCodeResponse, 0, len(items))
		for _, item := range items {
			resp = append(resp, adminInviteCodeResponse{
				inviteCodeResponse: toInviteCodeResponse(item.InviteCode),
				UserLogin:          item.UserLogin,
				UserEmail:          item.UserEmail,
			})
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

// --- /v1/admin/invite-codes/{id} ---

func adminInviteCodeEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getAdminInviteCode(authService, membershipRepo, inviteCodesRepo, apiKeysRepo)
	patch := patchAdminInviteCode(authService, membershipRepo, inviteCodesRepo, apiKeysRepo, auditWriter)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		idStr := strings.TrimPrefix(r.URL.Path, "/v1/admin/invite-codes/")
		idStr = strings.TrimRight(idStr, "/")
		id, err := uuid.Parse(idStr)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "http.not_found", "not found", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			get(w, r, id)
		case nethttp.MethodPatch:
			patch(w, r, id)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func getAdminInviteCode(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, id uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformInviteCodesManage, w, traceID) {
			return
		}

		ic, err := inviteCodesRepo.GetByID(r.Context(), id)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if ic == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "invite_codes.not_found", "invite code not found", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toInviteCodeResponse(*ic))
	}
}

func patchAdminInviteCode(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	inviteCodesRepo *data.InviteCodeRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	type patchBody struct {
		MaxUses  *int  `json:"max_uses"`
		IsActive *bool `json:"is_active"`
	}
	return func(w nethttp.ResponseWriter, r *nethttp.Request, id uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformInviteCodesManage, w, traceID) {
			return
		}

		var body patchBody
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid request body", traceID, nil)
			return
		}

		if body.MaxUses == nil && body.IsActive == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "at least one field required", traceID, nil)
			return
		}

		existing, err := inviteCodesRepo.GetByID(r.Context(), id)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if existing == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "invite_codes.not_found", "invite code not found", traceID, nil)
			return
		}

		changes := map[string]any{}
		var result *data.InviteCode

		if body.MaxUses != nil {
			if *body.MaxUses < 0 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "max_uses must not be negative", traceID, nil)
				return
			}
			result, err = inviteCodesRepo.UpdateMaxUses(r.Context(), id, *body.MaxUses)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			changes["max_uses"] = *body.MaxUses
		}

		if body.IsActive != nil {
			result, err = inviteCodesRepo.SetActive(r.Context(), id, *body.IsActive)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			changes["is_active"] = *body.IsActive
		}

		if auditWriter != nil {
			auditWriter.WriteInviteCodeUpdated(r.Context(), traceID, actor.UserID, id, changes)
		}

		if result == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "invite_codes.not_found", "invite code not found", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toInviteCodeResponse(*result))
	}
}

// --- /v1/admin/referrals ---

func adminReferralsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	referralsRepo *data.ReferralRepository,
	apiKeysRepo *data.APIKeysRepository,
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

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformInviteCodesManage, w, traceID) {
			return
		}

		inviterIDStr := strings.TrimSpace(r.URL.Query().Get("inviter_user_id"))
		if inviterIDStr == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "inviter_user_id required", traceID, nil)
			return
		}
		inviterID, err := uuid.Parse(inviterIDStr)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid inviter_user_id", traceID, nil)
			return
		}

		limit, ok := httpkit.ParseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}
		beforeCreatedAt, beforeID, ok := httpkit.ParseThreadCursor(w, traceID, r.URL.Query())
		if !ok {
			return
		}

		items, err := referralsRepo.ListByInviterUserID(r.Context(), inviterID, limit, beforeCreatedAt, beforeID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]referralResponse, 0, len(items))
		for _, item := range items {
			resp = append(resp, referralResponse{
				ID:            item.ID.String(),
				InviterUserID: item.InviterUserID.String(),
				InviteeUserID: item.InviteeUserID.String(),
				InviteCodeID:  item.InviteCodeID.String(),
				Credited:      item.Credited,
				CreatedAt:     item.CreatedAt.Format(time.RFC3339Nano),
				InviterLogin:  item.InviterLogin,
				InviteeLogin:  item.InviteeLogin,
			})
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

// --- /v1/admin/referrals/tree ---

func adminReferralTree(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	referralsRepo *data.ReferralRepository,
	apiKeysRepo *data.APIKeysRepository,
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

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformInviteCodesManage, w, traceID) {
			return
		}

		userIDStr := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userIDStr == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "user_id required", traceID, nil)
			return
		}
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid user_id", traceID, nil)
			return
		}

		nodes, err := referralsRepo.GetReferralTree(r.Context(), userID, 3)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]referralTreeNodeResponse, 0, len(nodes))
		for _, n := range nodes {
			var inviterID *string
			if n.InviterID != nil {
				s := n.InviterID.String()
				inviterID = &s
			}
			resp = append(resp, referralTreeNodeResponse{
				UserID:    n.UserID.String(),
				Login:     n.Login,
				InviterID: inviterID,
				Depth:     n.Depth,
				CreatedAt: n.CreatedAt.Format(time.RFC3339Nano),
			})
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}
