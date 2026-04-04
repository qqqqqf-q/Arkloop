package accountapi

import (
	"encoding/json"
	nethttp "net/http"
	"strconv"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type memoryErrorEvent struct {
	EventID  uuid.UUID `json:"event_id"`
	RunID    uuid.UUID `json:"run_id"`
	TS       time.Time `json:"ts"`
	Type     string    `json:"type"`
	DataJSON any       `json:"data"`
}

type memoryErrorsResponse struct {
	Errors []memoryErrorEvent `json:"errors"`
	Total  int                `json:"total"`
}

func memoryErrorsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > 100 {
			limit = 100
		}

		ctx := r.Context()
		rows, err := pool.Query(ctx, `
			SELECT re.event_id, re.run_id, re.ts, re.type, re.data_json
			FROM run_events re
			JOIN runs r ON r.id = re.run_id
			WHERE r.account_id = $1
			  AND re.type IN (
			    'memory.write.failed',
			    'memory.distill.append_failed',
			    'memory.distill.commit_failed'
			  )
			ORDER BY re.ts DESC
			LIMIT $2
		`, actor.AccountID, limit)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "query failed", traceID, nil)
			return
		}
		defer rows.Close()

		events := make([]memoryErrorEvent, 0)
		for rows.Next() {
			var evt memoryErrorEvent
			var rawJSON []byte
			if err := rows.Scan(&evt.EventID, &evt.RunID, &evt.TS, &evt.Type, &rawJSON); err != nil {
				continue
			}
			_ = json.Unmarshal(rawJSON, &evt.DataJSON)
			events = append(events, evt)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, memoryErrorsResponse{
			Errors: events,
			Total:  len(events),
		})
	}
}
