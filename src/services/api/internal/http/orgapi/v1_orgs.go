package orgapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type accountResponse struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
}

type createWorkspaceRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func orgsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	accountRepo *data.AccountRepository,
	accountService *auth.AccountService,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		// GET /v1/orgs/me
		if r.Method == nethttp.MethodGet && strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/me") {
			listMyAccounts(w, r, authService, membershipRepo, accountRepo, apiKeysRepo)
			return
		}

		// POST /v1/orgs
		if r.Method == nethttp.MethodPost && (r.URL.Path == "/v1/orgs" || r.URL.Path == "/v1/orgs/") {
			createWorkspace(w, r, authService, membershipRepo, accountService, apiKeysRepo)
			return
		}

		httpkit.WriteNotFound(w, r)
	}
}

func listMyAccounts(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	accountRepo *data.AccountRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	accounts, err := accountRepo.ListByUser(r.Context(), actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]accountResponse, 0, len(accounts))
	for _, o := range accounts {
		resp = append(resp, toAccountResponse(o))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func createWorkspace(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	accountService *auth.AccountService,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if accountService == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	var req createWorkspaceRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Slug = strings.TrimSpace(req.Slug)
	req.Name = strings.TrimSpace(req.Name)

	if req.Slug == "" || len(req.Slug) > 100 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "slug must be 1-100 characters", traceID, nil)
		return
	}
	if req.Name == "" || len(req.Name) > 200 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must be 1-200 characters", traceID, nil)
		return
	}

	result, err := accountService.CreateWorkspace(r.Context(), req.Slug, req.Name, actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toAccountResponse(result.Account))
}

func toAccountResponse(o data.Account) accountResponse {
	return accountResponse{
		ID:        o.ID.String(),
		Slug:      o.Slug,
		Name:      o.Name,
		Type:      o.Type,
		CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
