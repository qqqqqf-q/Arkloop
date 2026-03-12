//go:build desktop

package catalogapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

func toolProvidersEntry(
	_ *auth.Service,
	_ *data.OrgMembershipRepository,
	_ *data.ToolProviderConfigsRepository,
	_ *data.SecretsRepository,
	_ database.DB,
	_ *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNotImplemented)
	}
}

func toolProviderEntry(
	_ *auth.Service,
	_ *data.OrgMembershipRepository,
	_ *data.ToolProviderConfigsRepository,
	_ *data.SecretsRepository,
	_ database.DB,
	_ *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNotImplemented)
	}
}
