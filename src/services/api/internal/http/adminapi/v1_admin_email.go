package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"strconv"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"
)

const (
	settingEmailFrom     = "email.from"
	settingEmailSMTPHost = "email.smtp_host"
	settingEmailSMTPPort = "email.smtp_port"
	settingEmailSMTPUser = "email.smtp_user"
	settingEmailSMTPPass = "email.smtp_pass"
	settingEmailTLSMode  = "email.smtp_tls_mode"
)

type emailStatusResponse struct {
	Configured bool   `json:"configured"`
	From       string `json:"from,omitempty"`
	Source     string `json:"source"` // "db" | "env" | "none"
}

type emailConfigResponse struct {
	From        string `json:"from"`
	SMTPHost    string `json:"smtp_host"`
	SMTPPort    string `json:"smtp_port"`
	SMTPUser    string `json:"smtp_user"`
	SMTPPassSet bool   `json:"smtp_pass_set"`
	SMTPTLSMode string `json:"smtp_tls_mode"`
}

type updateEmailConfigRequest struct {
	From        string `json:"from"`
	SMTPHost    string `json:"smtp_host"`
	SMTPPort    string `json:"smtp_port"`
	SMTPUser    string `json:"smtp_user"`
	SMTPPass    string `json:"smtp_pass"` // empty = keep existing
	SMTPTLSMode string `json:"smtp_tls_mode"`
}

type adminEmailTestRequest struct {
	To string `json:"to"`
}

func loadEmailSettings(ctx context.Context, resolver sharedconfig.Resolver) (map[string]string, error) {
	if resolver == nil {
		return map[string]string{}, nil
	}
	return resolver.ResolvePrefix(ctx, "email.", sharedconfig.Scope{})
}

func adminEmailStatus(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		resp := emailStatusResponse{Source: "none"}
		if resolver != nil {
			var (
				val string
				src string
				err error
			)
			if sr, ok := resolver.(sharedconfig.ResolverWithSource); ok {
				val, src, err = sr.ResolveWithSource(r.Context(), settingEmailFrom, sharedconfig.Scope{})
			} else {
				val, err = resolver.Resolve(r.Context(), settingEmailFrom, sharedconfig.Scope{})
			}
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			val = strings.TrimSpace(val)
			if val != "" {
				resp.Configured = true
				resp.From = val
			}
			switch src {
			case "env":
				resp.Source = "env"
			case "platform_db":
				resp.Source = "db"
			default:
				resp.Source = "none"
			}
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func adminEmailConfig(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
	resolver sharedconfig.Resolver,
	invalidator sharedconfig.Invalidator,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	getHandler := adminEmailGetConfig(authService, membershipRepo, apiKeysRepo, resolver)
	putHandler := adminEmailPutConfig(authService, membershipRepo, apiKeysRepo, settingsRepo, invalidator)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			getHandler(w, r)
		case nethttp.MethodPut:
			putHandler(w, r)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func adminEmailGetConfig(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		m, err := loadEmailSettings(r.Context(), resolver)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		port := strings.TrimSpace(m[settingEmailSMTPPort])
		if port == "" {
			port = "587"
		}
		tlsMode := strings.TrimSpace(m[settingEmailTLSMode])
		if tlsMode == "" {
			tlsMode = "starttls"
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, emailConfigResponse{
			From:        m[settingEmailFrom],
			SMTPHost:    m[settingEmailSMTPHost],
			SMTPPort:    port,
			SMTPUser:    m[settingEmailSMTPUser],
			SMTPPassSet: m[settingEmailSMTPPass] != "",
			SMTPTLSMode: tlsMode,
		})
	}
}

func adminEmailPutConfig(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
	invalidator sharedconfig.Invalidator,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPut {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if settingsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		var body updateEmailConfigRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		// Validate port if provided
		if body.SMTPPort != "" {
			p, err := strconv.Atoi(strings.TrimSpace(body.SMTPPort))
			if err != nil || p <= 0 || p > 65535 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "smtp_port must be 1-65535", traceID, nil)
				return
			}
		}
		// Validate TLS mode if provided
		if body.SMTPTLSMode != "" {
			switch body.SMTPTLSMode {
			case "starttls", "tls", "none":
			default:
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "smtp_tls_mode must be starttls, tls, or none", traceID, nil)
				return
			}
		}

		ctx := r.Context()
		updates := map[string]string{
			settingEmailFrom:     strings.TrimSpace(body.From),
			settingEmailSMTPHost: strings.TrimSpace(body.SMTPHost),
			settingEmailSMTPPort: strings.TrimSpace(body.SMTPPort),
			settingEmailSMTPUser: strings.TrimSpace(body.SMTPUser),
			settingEmailTLSMode:  strings.TrimSpace(body.SMTPTLSMode),
		}
		for k, v := range updates {
			if _, err := settingsRepo.Set(ctx, k, v); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if invalidator != nil {
				_ = invalidator.Invalidate(ctx, k, sharedconfig.Scope{})
			}
		}
		// Password: only update if non-empty (empty = keep existing)
		if body.SMTPPass != "" {
			if _, err := settingsRepo.Set(ctx, settingEmailSMTPPass, body.SMTPPass); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if invalidator != nil {
				_ = invalidator.Invalidate(ctx, settingEmailSMTPPass, sharedconfig.Scope{})
			}
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func adminEmailTest(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	jobRepo *data.JobRepository,
	settingsRepo *data.PlatformSettingsRepository,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		configured := false
		if resolver != nil {
			val, err := resolver.Resolve(r.Context(), settingEmailFrom, sharedconfig.Scope{})
			if err == nil && strings.TrimSpace(val) != "" {
				configured = true
			}
		}
		if !configured {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "email.not_configured", "email not configured", traceID, nil)
			return
		}

		if jobRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		var body adminEmailTestRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.To = strings.TrimSpace(body.To)
		if body.To == "" || !strings.Contains(body.To, "@") {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "to must be a valid email address", traceID, nil)
			return
		}

		html := "<p>This is a test email from Arkloop.</p>"
		text := "This is a test email from Arkloop."
		if _, err := jobRepo.EnqueueEmail(r.Context(), body.To, "Arkloop test email", html, text); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}
