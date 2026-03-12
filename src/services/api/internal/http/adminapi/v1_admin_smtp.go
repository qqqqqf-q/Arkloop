package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	nethttp "net/http"
	"strings"

	"github.com/google/uuid"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type smtpProviderResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	FromAddr  string `json:"from_addr"`
	SmtpHost  string `json:"smtp_host"`
	SmtpPort  int    `json:"smtp_port"`
	SmtpUser  string `json:"smtp_user"`
	PassSet   bool   `json:"pass_set"`
	TLSMode   string `json:"tls_mode"`
	IsDefault bool   `json:"is_default"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type createSmtpProviderRequest struct {
	Name     string `json:"name"`
	FromAddr string `json:"from_addr"`
	SmtpHost string `json:"smtp_host"`
	SmtpPort int    `json:"smtp_port"`
	SmtpUser string `json:"smtp_user"`
	SmtpPass string `json:"smtp_pass"`
	TLSMode  string `json:"tls_mode"`
}

type updateSmtpProviderRequest struct {
	Name     string `json:"name"`
	FromAddr string `json:"from_addr"`
	SmtpHost string `json:"smtp_host"`
	SmtpPort int    `json:"smtp_port"`
	SmtpUser string `json:"smtp_user"`
	SmtpPass string `json:"smtp_pass"`
	TLSMode  string `json:"tls_mode"`
}

type smtpTestRequest struct {
	To string `json:"to"`
}

func toSmtpProviderResponse(s *data.SmtpProvider) smtpProviderResponse {
	return smtpProviderResponse{
		ID:        s.ID.String(),
		Name:      s.Name,
		FromAddr:  s.FromAddr,
		SmtpHost:  s.SmtpHost,
		SmtpPort:  s.SmtpPort,
		SmtpUser:  s.SmtpUser,
		PassSet:   s.SmtpPass != "",
		TLSMode:   s.TLSMode,
		IsDefault: s.IsDefault,
		CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func adminSmtpProviders(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			adminSmtpList(authService, membershipRepo, apiKeysRepo, smtpRepo)(w, r)
		case nethttp.MethodPost:
			adminSmtpCreate(authService, membershipRepo, apiKeysRepo, smtpRepo)(w, r)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

// adminSmtpProviderEntry 处理 /v1/admin/smtp-providers/{id}[/suffix] 路由。
func adminSmtpProviderEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
	jobRepo *data.JobRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		tail := strings.TrimPrefix(r.URL.Path, "/v1/admin/smtp-providers/")
		parts := strings.SplitN(tail, "/", 2)
		rawID := parts[0]
		suffix := ""
		if len(parts) > 1 {
			suffix = parts[1]
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if rawID == "" {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "id is required", traceID, nil)
			return
		}
		id, err := uuid.Parse(rawID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid id", traceID, nil)
			return
		}

		switch suffix {
		case "":
			switch r.Method {
			case nethttp.MethodPut:
				adminSmtpUpdateByID(authService, membershipRepo, apiKeysRepo, smtpRepo, id)(w, r)
			case nethttp.MethodDelete:
				adminSmtpDeleteByID(authService, membershipRepo, apiKeysRepo, smtpRepo, id)(w, r)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
		case "default":
			if r.Method != nethttp.MethodPut {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			adminSmtpSetDefaultByID(authService, membershipRepo, apiKeysRepo, smtpRepo, id)(w, r)
		case "test":
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			adminSmtpTestByID(authService, membershipRepo, apiKeysRepo, smtpRepo, jobRepo, id)(w, r)
		default:
			nethttp.NotFound(w, r)
		}
	}
}

func adminSmtpList(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		items, err := smtpRepo.List(r.Context())
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]smtpProviderResponse, len(items))
		for i := range items {
			resp[i] = toSmtpProviderResponse(&items[i])
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func adminSmtpCreate(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		var body createSmtpProviderRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
			return
		}
		if err := validateSmtpFields(body.FromAddr, body.SmtpHost, body.SmtpPort, body.TLSMode); err != "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err, traceID, nil)
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name is required", traceID, nil)
			return
		}

		s, dbErr := smtpRepo.Create(r.Context(), data.CreateSmtpProviderParams{
			Name:     body.Name,
			FromAddr: body.FromAddr,
			SmtpHost: body.SmtpHost,
			SmtpPort: body.SmtpPort,
			SmtpUser: body.SmtpUser,
			SmtpPass: body.SmtpPass,
			TLSMode:  body.TLSMode,
		})
		if dbErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSmtpProviderResponse(s))
	}
}

func adminSmtpUpdateByID(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
	id uuid.UUID,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		var body updateSmtpProviderRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
			return
		}
		if err := validateSmtpFields(body.FromAddr, body.SmtpHost, body.SmtpPort, body.TLSMode); err != "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err, traceID, nil)
			return
		}

		s, dbErr := smtpRepo.Update(r.Context(), id, data.UpdateSmtpProviderParams{
			Name:     body.Name,
			FromAddr: body.FromAddr,
			SmtpHost: body.SmtpHost,
			SmtpPort: body.SmtpPort,
			SmtpUser: body.SmtpUser,
			SmtpPass: body.SmtpPass,
			TLSMode:  body.TLSMode,
		})
		if dbErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if s == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "smtp provider not found", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toSmtpProviderResponse(s))
	}
}

func adminSmtpDeleteByID(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
	id uuid.UUID,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		existing, _ := smtpRepo.Get(r.Context(), id)
		if existing == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "smtp provider not found", traceID, nil)
			return
		}
		if existing.IsDefault {
			httpkit.WriteError(w, nethttp.StatusConflict, "conflict", "cannot delete default smtp provider", traceID, nil)
			return
		}

		if err := smtpRepo.Delete(r.Context(), id); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func adminSmtpSetDefaultByID(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
	id uuid.UUID,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if err := smtpRepo.SetDefault(r.Context(), id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "smtp provider not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func adminSmtpTestByID(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	smtpRepo *data.SmtpProviderRepository,
	jobRepo *data.JobRepository,
	id uuid.UUID,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		provider, _ := smtpRepo.Get(r.Context(), id)
		if provider == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "smtp provider not found", traceID, nil)
			return
		}

		if jobRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		var body smtpTestRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
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

func validateSmtpFields(from, host string, port int, tlsMode string) string {
	if strings.TrimSpace(from) == "" {
		return "from_addr is required"
	}
	if !strings.Contains(from, "@") {
		return "from_addr must be a valid email address"
	}
	if strings.TrimSpace(host) == "" {
		return "smtp_host is required"
	}
	if port < 0 || port > 65535 {
		return "smtp_port must be 0-65535"
	}
	if tlsMode != "" {
		switch tlsMode {
		case "starttls", "tls", "none":
		default:
			return "tls_mode must be starttls, tls, or none"
		}
	}
	return ""
}
