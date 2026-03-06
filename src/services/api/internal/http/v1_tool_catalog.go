package http

import (
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type toolCatalogItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type toolCatalogGroup struct {
	Group string            `json:"group"`
	Tools []toolCatalogItem `json:"tools"`
}

type toolCatalogResponse struct {
	Groups []toolCatalogGroup `json:"groups"`
}

var staticToolCatalog = toolCatalogResponse{
	Groups: []toolCatalogGroup{
		{
			Group: "web_search",
			Tools: []toolCatalogItem{
				{Name: "web_search", Description: "Web search"},
			},
		},
		{
			Group: "web_fetch",
			Tools: []toolCatalogItem{
				{Name: "web_fetch", Description: "Web page fetch"},
			},
		},
		{
			Group: "sandbox",
			Tools: []toolCatalogItem{
				{Name: "python_execute", Description: "Python code execution"},
				{Name: "exec_command", Description: "Persistent shell command execution"},
				{Name: "write_stdin", Description: "Shell stdin and poll"},
			},
		},
	},
}

func toolCatalogEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if _, ok := authenticateActor(w, r, traceID, authService, membershipRepo); !ok {
			return
		}
		writeJSON(w, traceID, nethttp.StatusOK, staticToolCatalog)
	}
}
