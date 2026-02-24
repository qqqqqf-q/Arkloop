package http

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type logoutResponse struct {
	OK bool `json:"ok"`
}

type registerRequest struct {
	Login       string `json:"login"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	InviteCode  string `json:"invite_code"`
}

type registerResponse struct {
	UserID      string  `json:"user_id"`
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	Warning     *string `json:"warning,omitempty"`
}

type registrationModeResponse struct {
	Mode string `json:"mode"`
}

type meResponse struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	CreatedAt   string   `json:"created_at"`
	OrgID       string   `json:"org_id,omitempty"`
	OrgName     string   `json:"org_name,omitempty"`
	Role        string   `json:"role,omitempty"`
	Permissions []string `json:"permissions"`
}

func login(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body loginRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		body.Login = strings.TrimSpace(body.Login)
		if body.Login == "" || len(body.Login) > 256 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) > 1024 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		issued, err := authService.IssueAccessToken(r.Context(), body.Login, body.Password)
		if err != nil {
			var invalid auth.InvalidCredentialsError
			if errors.As(err, &invalid) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Login)
		}

		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.Token,
			TokenType:   "bearer",
		})
	}
}

func refreshToken(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		token, ok := parseBearerToken(w, r, traceID)
		if !ok {
			return
		}

		issued, err := authService.RefreshAccessToken(r.Context(), token)
		if err != nil {
			switch err.(type) {
			case auth.TokenExpiredError, auth.TokenInvalidError, auth.UserNotFoundError:
				WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
				return
			default:
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}

		if auditWriter != nil {
			auditWriter.WriteTokenRefreshed(r.Context(), traceID, issued.UserID)
		}

		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.Token,
			TokenType:   "bearer",
		})
	}
}

func logout(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		if err := authService.Logout(r.Context(), user.ID, time.Now().UTC()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLogout(r.Context(), traceID, user.ID)
		}

		writeJSON(w, traceID, nethttp.StatusOK, logoutResponse{OK: true})
	}
}

func registrationMode(flagService *featureflag.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		mode := "invite_only"
		if flagService != nil {
			open, err := flagService.IsGloballyEnabled(r.Context(), "registration.open")
			if err == nil && open {
				mode = "open"
			}
		}

		writeJSON(w, traceID, nethttp.StatusOK, registrationModeResponse{Mode: mode})
	}
}

func register(
	registrationService *auth.RegistrationService,
	flagService *featureflag.Service,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if registrationService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body registerRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		body.Login = strings.TrimSpace(body.Login)
		body.DisplayName = strings.TrimSpace(body.DisplayName)
		body.InviteCode = strings.TrimSpace(body.InviteCode)
		if body.Login == "" || len(body.Login) > 256 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) < 8 || len(body.Password) > 1024 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.DisplayName == "" || len(body.DisplayName) > 256 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		// 注册模式检查
		openRegistration := false
		if flagService != nil {
			open, err := flagService.IsGloballyEnabled(r.Context(), "registration.open")
			if err == nil {
				openRegistration = open
			}
		}

		if !openRegistration && body.InviteCode == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "auth.invite_code_required", "invite code is required", traceID, nil)
			return
		}

		created, err := registrationService.Register(r.Context(), body.Login, body.Password, body.DisplayName, body.InviteCode, !openRegistration)
		if err != nil {
			var loginExists auth.LoginExistsError
			if errors.As(err, &loginExists) {
				WriteError(w, nethttp.StatusConflict, "auth.login_exists", "login already taken", traceID, nil)
				return
			}
			var codeErr auth.InviteCodeInvalidError
			if errors.As(err, &codeErr) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "auth.invite_code_invalid", codeErr.Error(), traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteUserRegistered(r.Context(), traceID, created.UserID, body.Login)
			if created.ReferralID != nil {
				auditWriter.WriteReferralCreated(r.Context(), traceID, created.InviterUserID, created.UserID, created.InviteCodeID, *created.ReferralID)
			}
		}

		resp := registerResponse{
			UserID:      created.UserID.String(),
			AccessToken: created.AccessToken,
			TokenType:   "bearer",
		}
		if created.Warning != "" {
			resp.Warning = &created.Warning
		}
		writeJSON(w, traceID, nethttp.StatusCreated, resp)
	}
}

func me(authService *auth.Service, membershipRepo *data.OrgMembershipRepository, orgRepo *data.OrgRepository) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		var permissions []string
		resp := meResponse{
			ID:          user.ID.String(),
			DisplayName: user.DisplayName,
			CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339Nano),
		}

		if membershipRepo != nil {
			if membership, err := membershipRepo.GetDefaultForUser(r.Context(), user.ID); err == nil && membership != nil {
				permissions = auth.PermissionsForRole(membership.Role)
				resp.OrgID = membership.OrgID.String()
				resp.Role = membership.Role

				if orgRepo != nil {
					if org, err := orgRepo.GetByID(r.Context(), membership.OrgID); err == nil && org != nil {
						resp.OrgName = org.Name
					}
				}
			}
		}
		if permissions == nil {
			permissions = []string{}
		}
		resp.Permissions = permissions

		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func decodeJSON(r *nethttp.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	return decoder.Decode(dst)
}

func writeJSON(w nethttp.ResponseWriter, traceID string, statusCode int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(raw)
}

func parseBearerToken(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if strings.TrimSpace(authorization) == "" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.missing_token", "missing Authorization Bearer token", traceID, nil)
		return "", false
	}

	scheme, rest, ok := strings.Cut(authorization, " ")
	if !ok || strings.TrimSpace(rest) == "" || strings.ToLower(scheme) != "bearer" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_authorization", "Authorization header must be: Bearer <token>", traceID, nil)
		return "", false
	}

	return strings.TrimSpace(rest), true
}

func authenticateUser(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
) (*authUser, bool) {
	token, ok := parseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	user, err := authService.AuthenticateUser(r.Context(), token)
	if err != nil {
		switch typed := err.(type) {
		case auth.TokenExpiredError:
			WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", typed.Error(), traceID, nil)
		case auth.TokenInvalidError:
			WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", typed.Error(), traceID, nil)
		case auth.UserNotFoundError:
			WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
		case auth.SuspendedUserError:
			WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
		default:
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		}
		return nil, false
	}

	return &authUser{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		CreatedAt:   user.CreatedAt,
	}, true
}

type authUser struct {
	ID          uuid.UUID
	DisplayName string
	Email       *string
	CreatedAt   time.Time
}

func writeMethodNotAllowed(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "Method Not Allowed", traceID, nil)
}

func writeAuthNotConfigured(w nethttp.ResponseWriter, traceID string) {
	WriteError(w, nethttp.StatusServiceUnavailable, "auth.not_configured", "auth not configured", traceID, nil)
}
