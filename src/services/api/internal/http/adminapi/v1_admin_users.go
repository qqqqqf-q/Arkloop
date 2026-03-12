package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type adminUserResponse struct {
	ID              string  `json:"id"`
	Login           *string `json:"login,omitempty"`
	Username        string  `json:"username"`
	Email           *string `json:"email"`
	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
	Status          string  `json:"status"`
	AvatarURL       *string `json:"avatar_url,omitempty"`
	Locale          *string `json:"locale,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`
	LastLoginAt     *string `json:"last_login_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type adminUserDetailResponse struct {
	adminUserResponse
	Accounts []adminUserAccountResponse `json:"accounts"`
}

type adminUserAccountResponse struct {
	AccountID string `json:"account_id"`
	Role  string `json:"role"`
}

func toAdminUserResponse(u data.User) adminUserResponse {
	resp := adminUserResponse{
		ID:        u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Status:    u.Status,
		AvatarURL: u.AvatarURL,
		Locale:    u.Locale,
		Timezone:  u.Timezone,
		CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"),
	}
	if u.EmailVerifiedAt != nil {
		s := u.EmailVerifiedAt.UTC().Format("2006-01-02T15:04:05.999999999Z")
		resp.EmailVerifiedAt = &s
	}
	if u.LastLoginAt != nil {
		s := u.LastLoginAt.UTC().Format("2006-01-02T15:04:05.999999999Z")
		resp.LastLoginAt = &s
	}
	return resp
}

func adminUsersEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
	credentialRepo *data.UserCredentialRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	list := listAdminUsers(authService, membershipRepo, usersRepo, apiKeysRepo, credentialRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			list(w, r)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func adminUserEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	inviteCodesRepo *data.InviteCodeRepository,
	credentialRepo *data.UserCredentialRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getAdminUser(authService, membershipRepo, usersRepo, apiKeysRepo, credentialRepo)
	patch := patchAdminUser(authService, membershipRepo, usersRepo, apiKeysRepo, auditWriter, inviteCodesRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/admin/users/")
		if tail == "" || strings.Contains(tail, "/") {
			httpkit.WriteError(w, nethttp.StatusNotFound, "http.not_found", "not found", traceID, nil)
			return
		}

		userID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid user_id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			get(w, r, userID)
		case nethttp.MethodPatch:
			patch(w, r, userID)
		case nethttp.MethodDelete:
			deleteUser(authService, membershipRepo, usersRepo, apiKeysRepo, auditWriter)(w, r, userID)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func listAdminUsers(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
	credentialRepo *data.UserCredentialRepository,
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
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
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

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		if status != "" && status != "active" && status != "suspended" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "status must be 'active' or 'suspended'", traceID, nil)
			return
		}

		users, err := usersRepo.List(r.Context(), limit, beforeCreatedAt, beforeID, query, status)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]adminUserResponse, 0, len(users))
		for _, u := range users {
			resp = append(resp, toAdminUserResponse(u))
		}

		// 批量获取 login（用户名）并填充到响应
		if credentialRepo != nil && len(users) > 0 {
			ids := make([]uuid.UUID, len(users))
			for i, u := range users {
				ids[i] = u.ID
			}
			logins, err := credentialRepo.ListLoginsByUserIDs(r.Context(), ids)
			if err == nil {
				for i := range resp {
					userID, _ := uuid.Parse(resp[i].ID)
					if login, ok := logins[userID]; ok {
						resp[i].Login = &login
					}
				}
			}
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func getAdminUser(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
	credentialRepo *data.UserCredentialRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, userID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		user, err := usersRepo.GetByID(r.Context(), userID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if user == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "users.not_found", "user not found", traceID, nil)
			return
		}

		detail := adminUserDetailResponse{
			adminUserResponse: toAdminUserResponse(*user),
			Accounts:              []adminUserAccountResponse{},
		}

		if credentialRepo != nil {
			if cred, err := credentialRepo.GetByUserID(r.Context(), userID); err == nil && cred != nil {
				detail.Login = &cred.Login
			}
		}

		if membershipRepo != nil {
			membership, err := membershipRepo.GetDefaultForUser(r.Context(), userID)
			if err == nil && membership != nil {
				detail.Accounts = append(detail.Accounts, adminUserAccountResponse{
					AccountID: membership.AccountID.String(),
					Role:  membership.Role,
				})
			}
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, detail)
	}
}

func deleteUser(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, userID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if err := usersRepo.SoftDelete(r.Context(), userID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				httpkit.WriteError(w, nethttp.StatusNotFound, "users.not_found", "user not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 删除用户后立即吊销其所有 token，避免旧 token 继续访问。
		if err := authService.BumpTokensInvalidBefore(r.Context(), userID, time.Now().UTC()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteUserStatusChanged(r.Context(), traceID, actor.UserID, userID, "active", "deleted")
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func patchAdminUser(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	inviteCodesRepo *data.InviteCodeRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	type patchBody struct {
		Status        *string `json:"status"`
		Username      *string `json:"username"`
		Email         *string `json:"email"`
		EmailVerified *bool   `json:"email_verified"`
		Locale        *string `json:"locale"`
		Timezone      *string `json:"timezone"`
	}

	return func(w nethttp.ResponseWriter, r *nethttp.Request, userID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		var body patchBody
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid request body", traceID, nil)
			return
		}

		// status 变更走独立分支
		if body.Status != nil {
			newStatus := *body.Status
			if newStatus != "active" && newStatus != "suspended" {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "status must be 'active' or 'suspended'", traceID, nil)
				return
			}

			existing, err := usersRepo.GetByID(r.Context(), userID)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if existing == nil {
				httpkit.WriteError(w, nethttp.StatusNotFound, "users.not_found", "user not found", traceID, nil)
				return
			}

			oldStatus := existing.Status
			if oldStatus == newStatus {
				httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toAdminUserResponse(*existing))
				return
			}

			updated, err := usersRepo.UpdateStatus(r.Context(), userID, newStatus)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if updated == nil {
				httpkit.WriteError(w, nethttp.StatusNotFound, "users.not_found", "user not found", traceID, nil)
				return
			}

			if auditWriter != nil {
				auditWriter.WriteUserStatusChanged(r.Context(), traceID, actor.UserID, userID, oldStatus, newStatus)
			}

			// 封禁后立即吊销其所有 token，保证写后强一致。
			if newStatus == "suspended" {
				if err := authService.BumpTokensInvalidBefore(r.Context(), userID, time.Now().UTC()); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			}

			// 封禁时停用该用户的所有邀请码
			if newStatus == "suspended" && inviteCodesRepo != nil {
				_ = inviteCodesRepo.DeactivateByUserID(r.Context(), userID)
			}

			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toAdminUserResponse(*updated))
			return
		}

		// profile 编辑
		if body.Username == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "username is required", traceID, nil)
			return
		}

		username := strings.TrimSpace(*body.Username)
		if username == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "username must not be empty", traceID, nil)
			return
		}

		existing, err := usersRepo.GetByID(r.Context(), userID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if existing == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "users.not_found", "user not found", traceID, nil)
			return
		}

		params := data.UpdateProfileParams{
			Username:        username,
			Locale:          existing.Locale,
			Timezone:        existing.Timezone,
			Email:           existing.Email,
			EmailVerifiedAt: existing.EmailVerifiedAt,
		}

		if body.Email != nil {
			email := strings.TrimSpace(*body.Email)
			if email == "" {
				params.Email = nil
			} else {
				params.Email = &email
			}
		}

		if body.Locale != nil {
			locale := strings.TrimSpace(*body.Locale)
			if locale == "" {
				params.Locale = nil
			} else {
				params.Locale = &locale
			}
		}

		if body.Timezone != nil {
			tz := strings.TrimSpace(*body.Timezone)
			if tz == "" {
				params.Timezone = nil
			} else {
				params.Timezone = &tz
			}
		}

		if body.EmailVerified != nil {
			if *body.EmailVerified {
				now := time.Now().UTC()
				params.EmailVerifiedAt = &now
			} else {
				params.EmailVerifiedAt = nil
			}
		}

		updated, err := usersRepo.UpdateProfile(r.Context(), userID, params)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if updated == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "users.not_found", "user not found", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toAdminUserResponse(*updated))
	}
}
