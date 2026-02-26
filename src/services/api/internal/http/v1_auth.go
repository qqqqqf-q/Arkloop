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
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

type logoutResponse struct {
	OK bool `json:"ok"`
}

type registerRequest struct {
	Login      string `json:"login"`
	Password   string `json:"password"`
	Email      string `json:"email"`
	InviteCode string `json:"invite_code"`
}

type registerResponse struct {
	UserID       string  `json:"user_id"`
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	TokenType    string  `json:"token_type"`
	Warning      *string `json:"warning,omitempty"`
}

type registrationModeResponse struct {
	Mode string `json:"mode"`
}

type meResponse struct {
	ID            string   `json:"id"`
	Username      string   `json:"username"`
	Email         *string  `json:"email,omitempty"`
	EmailVerified bool     `json:"email_verified"`
	CreatedAt     string   `json:"created_at"`
	OrgID         string   `json:"org_id,omitempty"`
	OrgName       string   `json:"org_name,omitempty"`
	Role          string   `json:"role,omitempty"`
	Permissions   []string `json:"permissions"`
}

type updateMeRequest struct {
	Username string `json:"username"`
}

type updateMeResponse struct {
	Username string `json:"username"`
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
			var unverified auth.EmailNotVerifiedError
			if errors.As(err, &unverified) {
				WriteError(w, nethttp.StatusForbidden, "auth.email_not_verified", "email not verified", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Login)
		}

		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken:  issued.AccessToken,
			RefreshToken: issued.RefreshToken,
			TokenType:    "bearer",
		})
	}
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
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

		var body refreshTokenRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if strings.TrimSpace(body.RefreshToken) == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "refresh_token is required", traceID, nil)
			return
		}

		issued, err := authService.ConsumeRefreshToken(r.Context(), body.RefreshToken)
		if err != nil {
			switch err.(type) {
			case auth.TokenInvalidError, auth.UserNotFoundError:
				WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
				return
			case auth.SuspendedUserError:
				WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
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
			AccessToken:  issued.AccessToken,
			RefreshToken: issued.RefreshToken,
			TokenType:    "bearer",
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
		body.Email = strings.TrimSpace(body.Email)
		body.InviteCode = strings.TrimSpace(body.InviteCode)
		if body.Login == "" || len(body.Login) > 256 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) < 8 || len(body.Password) > 1024 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Email == "" || len(body.Email) > 256 || !strings.Contains(body.Email, "@") {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "email is required and must be valid", traceID, nil)
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

		created, err := registrationService.Register(r.Context(), body.Login, body.Password, body.Email, body.InviteCode, !openRegistration)
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
			UserID:       created.UserID.String(),
			AccessToken:  created.AccessToken,
			RefreshToken: created.RefreshToken,
			TokenType:    "bearer",
		}
		if created.Warning != "" {
			resp.Warning = &created.Warning
		}
		writeJSON(w, traceID, nethttp.StatusCreated, resp)
	}
}

func me(authService *auth.Service, membershipRepo *data.OrgMembershipRepository, orgRepo *data.OrgRepository, credentialRepo *data.UserCredentialRepository, usersRepo *data.UserRepository) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			var permissions []string
			resp := meResponse{
				ID:            user.ID.String(),
				Email:         user.Email,
				EmailVerified: user.EmailVerifiedAt != nil,
				CreatedAt:     user.CreatedAt.UTC().Format(time.RFC3339Nano),
			}

			if credentialRepo != nil {
				if cred, err := credentialRepo.GetByUserID(r.Context(), user.ID); err == nil && cred != nil {
					resp.Username = cred.Login
				}
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

		case nethttp.MethodPatch:
			if usersRepo == nil {
				WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}

			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			var body updateMeRequest
			if err := decodeJSON(r, &body); err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			body.Username = strings.TrimSpace(body.Username)
			if body.Username == "" || len(body.Username) > 256 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "username is invalid", traceID, nil)
				return
			}

			updated, err := usersRepo.UpdateProfile(r.Context(), user.ID, data.UpdateProfileParams{
				DisplayName: body.Username,
			})
			if err != nil || updated == nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			writeJSON(w, traceID, nethttp.StatusOK, updateMeResponse{Username: updated.DisplayName})

		default:
			writeMethodNotAllowed(w, r)
		}
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
		ID:              user.ID,
		DisplayName:     user.DisplayName,
		Email:           user.Email,
		EmailVerifiedAt: user.EmailVerifiedAt,
		CreatedAt:       user.CreatedAt,
	}, true
}

type authUser struct {
	ID              uuid.UUID
	DisplayName     string
	Email           *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

func writeMethodNotAllowed(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "Method Not Allowed", traceID, nil)
}

func writeAuthNotConfigured(w nethttp.ResponseWriter, traceID string) {
	WriteError(w, nethttp.StatusServiceUnavailable, "auth.not_configured", "auth not configured", traceID, nil)
}

type emailVerifyConfirmRequest struct {
	Token string `json:"token"`
}

func emailVerifySend(authService *auth.Service, emailVerifyService *auth.EmailVerifyService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil || emailVerifyService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		if err := emailVerifyService.SendVerification(r.Context(), user.ID, user.DisplayName); err != nil {
			if err.Error() == "user has no email address" {
				WriteError(w, nethttp.StatusUnprocessableEntity, "email.no_address", "user has no email address", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func emailVerifyConfirm(emailVerifyService *auth.EmailVerifyService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if emailVerifyService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body emailVerifyConfirmRequest
		if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Token) == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "token is required", traceID, nil)
			return
		}

		if err := emailVerifyService.ConfirmVerification(r.Context(), strings.TrimSpace(body.Token)); err != nil {
			var expired auth.TokenAlreadyUsedOrExpiredError
			if errors.As(err, &expired) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "email.token_invalid", "token invalid or expired", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	}
}

type emailOTPSendRequest struct {
	Email string `json:"email"`
}

type emailOTPVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func emailOTPSend(otpLoginService *auth.EmailOTPLoginService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if otpLoginService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body emailOTPSendRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		if body.Email == "" || !strings.Contains(body.Email, "@") {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "valid email is required", traceID, nil)
			return
		}

		// 静默处理：无论邮箱是否存在都返回 204
		_ = otpLoginService.SendLoginOTP(r.Context(), body.Email)
		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func emailOTPVerify(otpLoginService *auth.EmailOTPLoginService, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if otpLoginService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body emailOTPVerifyRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		body.Code = strings.TrimSpace(body.Code)
		if body.Email == "" || body.Code == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "email and code are required", traceID, nil)
			return
		}

		issued, err := otpLoginService.VerifyLoginOTP(r.Context(), body.Email, body.Code)
		if err != nil {
			var expired auth.OTPExpiredOrUsedError
			if errors.As(err, &expired) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "auth.otp_invalid", "code invalid or expired", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Email)
		}

		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken:  issued.AccessToken,
			RefreshToken: issued.RefreshToken,
			TokenType:    "bearer",
		})
	}
}

type checkUserRequest struct {
	Login string `json:"login"`
}

type checkUserResponse struct {
	Exists      bool   `json:"exists"`
	MaskedEmail string `json:"masked_email,omitempty"`
}

// maskEmail 脱敏：john.doe@example.com → j***e@example.com
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	local, domain := parts[0], parts[1]
	if len(local) <= 2 {
		return local[:1] + "***@" + domain
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + domain
}

func checkUser(credentialRepo *data.UserCredentialRepository, usersRepo *data.UserRepository) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if credentialRepo == nil || usersRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "service_unavailable", "service unavailable", traceID, nil)
			return
		}
		var body checkUserRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusBadRequest, "bad_request", "invalid request body", traceID, nil)
			return
		}
		body.Login = strings.TrimSpace(body.Login)

		var maskedEmail string
		exists := false

		if cred, err := credentialRepo.GetByLogin(r.Context(), body.Login); err == nil && cred != nil {
			exists = true
			if user, err := usersRepo.GetByID(r.Context(), cred.UserID); err == nil && user != nil && user.Email != nil && *user.Email != "" {
				maskedEmail = maskEmail(*user.Email)
			}
		}
		if !exists {
			if cred, err := credentialRepo.GetByUserEmail(r.Context(), body.Login); err == nil && cred != nil {
				exists = true
				maskedEmail = maskEmail(body.Login)
			}
		}

		writeJSON(w, traceID, nethttp.StatusOK, checkUserResponse{Exists: exists, MaskedEmail: maskedEmail})
	}
}
