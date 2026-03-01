package http

import (
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"
)

type configSchemaItem struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
	Scope       string `json:"scope"`
}

func configSchemaEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	registry *sharedconfig.Registry,
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

		if registry == nil {
			registry = sharedconfig.DefaultRegistry()
		}
		entries := registry.List()
		entries = filterSchemaEntries(entries, actor != nil && actor.HasPermission(auth.PermPlatformAdmin))

		out := make([]configSchemaItem, 0, len(entries))
		for _, e := range entries {
			out = append(out, configSchemaItem{
				Key:         e.Key,
				Type:        e.Type,
				Default:     e.Default,
				Description: e.Description,
				Sensitive:   e.Sensitive,
				Scope:       e.Scope,
			})
		}
		writeJSON(w, traceID, nethttp.StatusOK, out)
	}
}

func filterSchemaEntries(entries []sharedconfig.Entry, isPlatformAdmin bool) []sharedconfig.Entry {
	if isPlatformAdmin {
		return entries
	}

	out := make([]sharedconfig.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Scope == sharedconfig.ScopeOrg || e.Scope == sharedconfig.ScopeBoth {
			out = append(out, e)
		}
	}
	return out
}
