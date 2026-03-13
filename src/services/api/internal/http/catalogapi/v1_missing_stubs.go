package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

func writeNotImplementedJSON(w nethttp.ResponseWriter, traceID string) {
	httpkit.WriteError(w, nethttp.StatusNotImplemented, "feature.not_implemented", "feature not implemented", traceID, nil)
}

func githubSkillImportEntry(
	_authService *auth.Service,
	_membershipRepo *data.OrgMembershipRepository,
	_apiKeysRepo *data.APIKeysRepository,
	_auditWriter *audit.Writer,
	_skillPackagesRepo *data.SkillPackagesRepository,
	_skillStore skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, observability.TraceIDFromContext(r.Context()))
			return
		}
		writeNotImplementedJSON(w, observability.TraceIDFromContext(r.Context()))
	}
}

func uploadSkillImportEntry(
	_authService *auth.Service,
	_membershipRepo *data.OrgMembershipRepository,
	_apiKeysRepo *data.APIKeysRepository,
	_auditWriter *audit.Writer,
	_skillPackagesRepo *data.SkillPackagesRepository,
	_skillStore skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, observability.TraceIDFromContext(r.Context()))
			return
		}
		writeNotImplementedJSON(w, observability.TraceIDFromContext(r.Context()))
	}
}

func marketSkillsEntry(
	_authService *auth.Service,
	_membershipRepo *data.OrgMembershipRepository,
	_apiKeysRepo *data.APIKeysRepository,
	_auditWriter *audit.Writer,
	_platformSettingsRepo *data.PlatformSettingsRepository,
	_profileSkillInstallsRepo *data.ProfileSkillInstallsRepository,
	_profileRegistriesRepo *data.ProfileRegistriesRepository,
	_workspaceSkillEnableRepo *data.WorkspaceSkillEnablementsRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowedJSON(w, observability.TraceIDFromContext(r.Context()))
			return
		}
		writeNotImplementedJSON(w, observability.TraceIDFromContext(r.Context()))
	}
}

func marketSkillsImportEntry(
	_authService *auth.Service,
	_membershipRepo *data.OrgMembershipRepository,
	_apiKeysRepo *data.APIKeysRepository,
	_auditWriter *audit.Writer,
	_skillPackagesRepo *data.SkillPackagesRepository,
	_skillStore skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, observability.TraceIDFromContext(r.Context()))
			return
		}
		writeNotImplementedJSON(w, observability.TraceIDFromContext(r.Context()))
	}
}

func profileDefaultSkillsEntry(
	_authService *auth.Service,
	_membershipRepo *data.OrgMembershipRepository,
	_apiKeysRepo *data.APIKeysRepository,
	_auditWriter *audit.Writer,
	_skillPackagesRepo *data.SkillPackagesRepository,
	_profileSkillInstallsRepo *data.ProfileSkillInstallsRepository,
	_workspaceSkillEnableRepo *data.WorkspaceSkillEnablementsRepository,
	_profileRegistriesRepo *data.ProfileRegistriesRepository,
	_workspaceRegistriesRepo *data.WorkspaceRegistriesRepository,
	_pool *pgxpool.Pool,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet, nethttp.MethodPut:
			writeNotImplementedJSON(w, observability.TraceIDFromContext(r.Context()))
		default:
			writeMethodNotAllowedJSON(w, observability.TraceIDFromContext(r.Context()))
		}
	}
}
