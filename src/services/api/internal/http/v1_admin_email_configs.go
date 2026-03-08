package http

import (
	"strconv"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type emailConfigItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	FromAddr    string `json:"from_addr"`
	SMTPHost    string `json:"smtp_host"`
	SMTPPort    string `json:"smtp_port"`
	SMTPUser    string `json:"smtp_user"`
	SMTPPassSet bool   `json:"smtp_pass_set"`
	SMTPTLSMode string `json:"smtp_tls_mode"`
	IsDefault   bool   `json:"is_default"`
}

type createEmailConfigRequest struct {
	Name        string `json:"name"`
	FromAddr    string `json:"from_addr"`
	SMTPHost    string `json:"smtp_host"`
	SMTPPort    string `json:"smtp_port"`
	SMTPUser    string `json:"smtp_user"`
	SMTPPass    string `json:"smtp_pass"`
	SMTPTLSMode string `json:"smtp_tls_mode"`
	IsDefault   bool   `json:"is_default"`
}

type patchEmailConfigRequest struct {
	Name        *string `json:"name"`
	FromAddr    *string `json:"from_addr"`
	SMTPHost    *string `json:"smtp_host"`
	SMTPPort    *string `json:"smtp_port"`
	SMTPUser    *string `json:"smtp_user"`
	SMTPPass    *string `json:"smtp_pass"` // nil = keep existing
	SMTPTLSMode *string `json:"smtp_tls_mode"`
}

func toEmailConfigItem(c data.EmailConfig) emailConfigItem {
	return emailConfigItem{
		ID:          c.ID.String(),
		Name:        c.Name,
		FromAddr:    c.FromAddr,
		SMTPHost:    c.SMTPHost,
		SMTPPort:    c.SMTPPort,
		SMTPUser:    c.SMTPUser,
		SMTPPassSet: c.SMTPPass != "",
		SMTPTLSMode: c.SMTPTLSMode,
		IsDefault:   c.IsDefault,
	}
}

func validateEmailConfigFields(port, tlsMode string) string {
	if port != "" {
		p, err := strconv.Atoi(strings.TrimSpace(port))
		if err != nil || p <= 0 || p > 65535 {
			return "smtp_port must be 1-65535"
		}
	}
	if tlsMode != "" {
		switch tlsMode {
		case "starttls", "tls", "none":
		default:
			return "smtp_tls_mode must be starttls, tls, or none"
		}
	}
	return ""
}

// adminEmailConfigsEntry 处理 GET/POST /v1/admin/email/configs。
func adminEmailConfigsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	emailConfigsRepo *data.EmailConfigsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if emailConfigsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			configs, err := emailConfigsRepo.List(r.Context())
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			items := make([]emailConfigItem, 0, len(configs))
			for _, c := range configs {
				items = append(items, toEmailConfigItem(c))
			}
			writeJSON(w, traceID, nethttp.StatusOK, items)

		case nethttp.MethodPost:
			var body createEmailConfigRequest
			if err := decodeJSON(r, &body); err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			body.Name = strings.TrimSpace(body.Name)
			if body.Name == "" {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name is required", traceID, nil)
				return
			}
			port := strings.TrimSpace(body.SMTPPort)
			if port == "" {
				port = "587"
			}
			tlsMode := strings.TrimSpace(body.SMTPTLSMode)
			if tlsMode == "" {
				tlsMode = "starttls"
			}
			if msg := validateEmailConfigFields(port, tlsMode); msg != "" {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", msg, traceID, nil)
				return
			}
			created, err := emailConfigsRepo.Create(r.Context(), data.EmailConfig{
				Name:        body.Name,
				FromAddr:    strings.TrimSpace(body.FromAddr),
				SMTPHost:    strings.TrimSpace(body.SMTPHost),
				SMTPPort:    port,
				SMTPUser:    strings.TrimSpace(body.SMTPUser),
				SMTPPass:    body.SMTPPass,
				SMTPTLSMode: tlsMode,
				IsDefault:   body.IsDefault,
			})
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if body.IsDefault {
				// 单独做 set-default，处理 unique index 冲突
				_ = emailConfigsRepo.SetDefault(r.Context(), created.ID)
			}
			writeJSON(w, traceID, nethttp.StatusCreated, toEmailConfigItem(*created))

		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

// adminEmailConfigEntry 处理 GET/PATCH/DELETE /v1/admin/email/configs/{id}。
func adminEmailConfigEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	emailConfigsRepo *data.EmailConfigsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if emailConfigsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
			return
		}

		rawID := strings.TrimPrefix(r.URL.Path, "/v1/admin/email/configs/")
		rawID = strings.TrimSuffix(rawID, "/set-default")
		id, err := uuid.Parse(rawID)
		if err != nil {
			WriteError(w, nethttp.StatusNotFound, "not_found", "config not found", traceID, nil)
			return
		}

		// POST .../set-default
		if strings.HasSuffix(r.URL.Path, "/set-default") {
			if r.Method != nethttp.MethodPost {
				writeMethodNotAllowed(w, r)
				return
			}
			if err := emailConfigsRepo.SetDefault(r.Context(), id); err != nil {
				if strings.Contains(err.Error(), "not found") {
					WriteError(w, nethttp.StatusNotFound, "not_found", "config not found", traceID, nil)
				} else {
					WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				}
				return
			}
			w.WriteHeader(nethttp.StatusNoContent)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			c, err := emailConfigsRepo.Get(r.Context(), id)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if c == nil {
				WriteError(w, nethttp.StatusNotFound, "not_found", "config not found", traceID, nil)
				return
			}
			writeJSON(w, traceID, nethttp.StatusOK, toEmailConfigItem(*c))

		case nethttp.MethodPatch:
			existing, err := emailConfigsRepo.Get(r.Context(), id)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if existing == nil {
				WriteError(w, nethttp.StatusNotFound, "not_found", "config not found", traceID, nil)
				return
			}

			var body patchEmailConfigRequest
			if err := decodeJSON(r, &body); err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}

			// 仅更新非 nil 字段
			updated := *existing
			if body.Name != nil {
				updated.Name = strings.TrimSpace(*body.Name)
			}
			if body.FromAddr != nil {
				updated.FromAddr = strings.TrimSpace(*body.FromAddr)
			}
			if body.SMTPHost != nil {
				updated.SMTPHost = strings.TrimSpace(*body.SMTPHost)
			}
			if body.SMTPPort != nil {
				updated.SMTPPort = strings.TrimSpace(*body.SMTPPort)
			}
			if body.SMTPUser != nil {
				updated.SMTPUser = strings.TrimSpace(*body.SMTPUser)
			}
			if body.SMTPTLSMode != nil {
				updated.SMTPTLSMode = strings.TrimSpace(*body.SMTPTLSMode)
			}

			if msg := validateEmailConfigFields(updated.SMTPPort, updated.SMTPTLSMode); msg != "" {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", msg, traceID, nil)
				return
			}

			result, err := emailConfigsRepo.Update(r.Context(), id, updated)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if result == nil {
				WriteError(w, nethttp.StatusNotFound, "not_found", "config not found", traceID, nil)
				return
			}

			// 密码单独更新（空字符串 = 不改）
			if body.SMTPPass != nil && *body.SMTPPass != "" {
				if err := emailConfigsRepo.UpdatePass(r.Context(), id, *body.SMTPPass); err != nil {
					WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			}

			writeJSON(w, traceID, nethttp.StatusOK, toEmailConfigItem(*result))

		case nethttp.MethodDelete:
			if err := emailConfigsRepo.Delete(r.Context(), id); err != nil {
				if strings.Contains(err.Error(), "not found") {
					WriteError(w, nethttp.StatusNotFound, "not_found", "config not found", traceID, nil)
				} else {
					WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				}
				return
			}
			w.WriteHeader(nethttp.StatusNoContent)

		default:
			writeMethodNotAllowed(w, r)
		}
	}
}
