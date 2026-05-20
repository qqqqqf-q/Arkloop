package conversationapi

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"

	"github.com/jackc/pgx/v5"
)

func suggestionsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		mode := strings.TrimSpace(r.URL.Query().Get("mode"))
		if mode == "" {
			mode = "chat"
		}
		if mode != "chat" && mode != "work" {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "mode must be chat or work", traceID, nil)
			return
		}

		agentID := "user_" + actor.UserID.String()

		var suggestionsJSON string
		var expiresAt *time.Time
		var updatedAt time.Time
		err := pool.QueryRow(r.Context(),
			`SELECT suggestions_json, expires_at, updated_at FROM user_suggestion_snapshots
			 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3 AND mode = $4`,
			actor.AccountID, actor.UserID, agentID, mode,
		).Scan(&suggestionsJSON, &expiresAt, &updatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"suggestions": []any{}})
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if expiresAt != nil && time.Now().After(*expiresAt) {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"suggestions": []any{}})
			return
		}

		var suggestions []any
		if json.Unmarshal([]byte(suggestionsJSON), &suggestions) != nil {
			suggestions = []any{}
		}
		resp := map[string]any{
			"suggestions": suggestions,
			"updated_at":  updatedAt.UTC().Format(time.RFC3339Nano),
		}
		if expiresAt != nil {
			resp["expires_at"] = expiresAt.UTC().Format(time.RFC3339Nano)
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}
