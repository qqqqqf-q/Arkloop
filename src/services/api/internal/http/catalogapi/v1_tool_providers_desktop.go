//go:build desktop

package catalogapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"

	"github.com/jackc/pgx/v5/pgxpool"
)

func toolProvidersEntry(
	_ *auth.Service,
	_ *data.AccountMembershipRepository,
	_ *data.ToolProviderConfigsRepository,
	_ *data.SecretsRepository,
	_ data.DB,
	_ *pgxpool.Pool,
	_ *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNotImplemented)
	}
}

func toolProviderEntry(
	_ *auth.Service,
	_ *data.AccountMembershipRepository,
	_ *data.ToolProviderConfigsRepository,
	_ *data.SecretsRepository,
	_ data.DB,
	_ *pgxpool.Pool,
	_ *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNotImplemented)
	}
}
