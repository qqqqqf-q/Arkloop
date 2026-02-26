package http

import (
	"context"
	"strconv"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
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

// loadEmailSettings reads all email.* keys from platform_settings.
func loadEmailSettings(ctx context.Context, repo *data.PlatformSettingsRepository) (map[string]string, error) {
	keys := []string{settingEmailFrom, settingEmailSMTPHost, settingEmailSMTPPort, settingEmailSMTPUser, settingEmailSMTPPass, settingEmailTLSMode}
	result := make(map[string]string, len(keys))
	for _, k := range keys {
		s, err := repo.Get(ctx, k)
		if err != nil {
			return nil, err
		}
		if s != nil {
			result[k] = s.Value
		}
	}
	return result, nil
}

func adminEmailStatus(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
	envFrom string,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		resp := emailStatusResponse{Source: "none"}

		if settingsRepo != nil {
			s, err := settingsRepo.Get(r.Context(), settingEmailFrom)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if s != nil && strings.TrimSpace(s.Value) != "" {
				resp.Configured = true
				resp.From = strings.TrimSpace(s.Value)
				resp.Source = "db"
			}
		}

		if !resp.Configured && strings.TrimSpace(envFrom) != "" {
			resp.Configured = true
			resp.From = strings.TrimSpace(envFrom)
			resp.Source = "env"
		}

		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func adminEmailConfig(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	getHandler := adminEmailGetConfig(authService, membershipRepo, apiKeysRepo, settingsRepo)
	putHandler := adminEmailPutConfig(authService, membershipRepo, apiKeysRepo, settingsRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			getHandler(w, r)
		case nethttp.MethodPut:
			putHandler(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func adminEmailGetConfig(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if settingsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		m, err := loadEmailSettings(r.Context(), settingsRepo)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		port := m[settingEmailSMTPPort]
		if port == "" {
			port = "587"
		}
		tlsMode := m[settingEmailTLSMode]
		if tlsMode == "" {
			tlsMode = "starttls"
		}

		writeJSON(w, traceID, nethttp.StatusOK, emailConfigResponse{
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
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPut {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if settingsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		var body updateEmailConfigRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		// Validate port if provided
		if body.SMTPPort != "" {
			p, err := strconv.Atoi(strings.TrimSpace(body.SMTPPort))
			if err != nil || p <= 0 || p > 65535 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "smtp_port must be 1-65535", traceID, nil)
				return
			}
		}
		// Validate TLS mode if provided
		if body.SMTPTLSMode != "" {
			switch body.SMTPTLSMode {
			case "starttls", "tls", "none":
			default:
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "smtp_tls_mode must be starttls, tls, or none", traceID, nil)
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
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
		// Password: only update if non-empty (empty = keep existing)
		if body.SMTPPass != "" {
			if _, err := settingsRepo.Set(ctx, settingEmailSMTPPass, body.SMTPPass); err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func adminEmailTest(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	jobRepo *data.JobRepository,
	settingsRepo *data.PlatformSettingsRepository,
	envFrom string,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		// Check if email is effectively configured (DB or env)
		configured := strings.TrimSpace(envFrom) != ""
		if !configured && settingsRepo != nil {
			s, err := settingsRepo.Get(r.Context(), settingEmailFrom)
			if err == nil && s != nil && strings.TrimSpace(s.Value) != "" {
				configured = true
			}
		}
		if !configured {
			WriteError(w, nethttp.StatusServiceUnavailable, "email.not_configured", "email not configured", traceID, nil)
			return
		}

		if jobRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		var body adminEmailTestRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.To = strings.TrimSpace(body.To)
		if body.To == "" || !strings.Contains(body.To, "@") {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "to must be a valid email address", traceID, nil)
			return
		}

		html := "<p>This is a test email from Arkloop.</p>"
		text := "This is a test email from Arkloop."
		if _, err := jobRepo.EnqueueEmail(r.Context(), body.To, "Arkloop test email", html, text); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

