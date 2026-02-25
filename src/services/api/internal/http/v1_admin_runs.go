package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type adminRunEventsStats struct {
	Total             int `json:"total"`
	LlmTurns          int `json:"llm_turns"`
	ToolCalls         int `json:"tool_calls"`
	ProviderFallbacks int `json:"provider_fallbacks"`
}

type adminRunDetailResponse struct {
	RunID             string   `json:"run_id"`
	OrgID             string   `json:"org_id"`
	ThreadID          string   `json:"thread_id"`
	Status            string   `json:"status"`
	Model             *string  `json:"model,omitempty"`
	SkillID           *string  `json:"skill_id,omitempty"`
	ProviderKind      *string  `json:"provider_kind,omitempty"`
	APIMode           *string  `json:"api_mode,omitempty"`
	DurationMs        *int64   `json:"duration_ms,omitempty"`
	TotalInputTokens  *int64   `json:"total_input_tokens,omitempty"`
	TotalOutputTokens *int64   `json:"total_output_tokens,omitempty"`
	TotalCostUSD      *float64 `json:"total_cost_usd,omitempty"`
	CreatedAt         string   `json:"created_at"`
	CompletedAt       *string  `json:"completed_at,omitempty"`
	FailedAt          *string  `json:"failed_at,omitempty"`
	// 创建者
	CreatedByUserID   *string `json:"created_by_user_id,omitempty"`
	CreatedByUserName *string `json:"created_by_user_name,omitempty"`
	CreatedByEmail    *string `json:"created_by_email,omitempty"`
	// 事件统计
	EventsStats adminRunEventsStats `json:"events_stats"`
}

func adminRunsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	usersRepo *data.UserRepository,
	apiKeysRepo *data.APIKeysRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil || usersRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		// 路径：/v1/admin/runs/{run_id}
		tail := strings.TrimPrefix(r.URL.Path, "/v1/admin/runs/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		runID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid run_id", traceID, nil)
			return
		}

		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		// 取事件流，用于统计和提取 provider 信息
		events, err := runRepo.ListEvents(r.Context(), runID, 0, 2000)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		stats, providerKind, apiMode := summarizeRunEvents(events)

		resp := adminRunDetailResponse{
			RunID:             run.ID.String(),
			OrgID:             run.OrgID.String(),
			ThreadID:          run.ThreadID.String(),
			Status:            run.Status,
			Model:             run.Model,
			SkillID:           run.SkillID,
			ProviderKind:      providerKind,
			APIMode:           apiMode,
			DurationMs:        run.DurationMs,
			TotalInputTokens:  run.TotalInputTokens,
			TotalOutputTokens: run.TotalOutputTokens,
			TotalCostUSD:      run.TotalCostUSD,
			CreatedAt:         run.CreatedAt.UTC().Format(time.RFC3339Nano),
			EventsStats:       stats,
		}

		if run.CompletedAt != nil {
			s := run.CompletedAt.UTC().Format(time.RFC3339Nano)
			resp.CompletedAt = &s
		}
		if run.FailedAt != nil {
			s := run.FailedAt.UTC().Format(time.RFC3339Nano)
			resp.FailedAt = &s
		}
		if run.CreatedByUserID != nil {
			s := run.CreatedByUserID.String()
			resp.CreatedByUserID = &s

			user, err := usersRepo.GetByID(r.Context(), *run.CreatedByUserID)
			if err == nil && user != nil {
				resp.CreatedByUserName = &user.DisplayName
				resp.CreatedByEmail = user.Email
			}
		}

		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

// summarizeRunEvents 遍历事件流，统计各类事件数量，并从首个 llm.request 提取 provider 信息。
func summarizeRunEvents(events []data.RunEvent) (stats adminRunEventsStats, providerKind *string, apiMode *string) {
	stats.Total = len(events)
	for _, ev := range events {
		switch ev.Type {
		case "llm.request":
			stats.LlmTurns++
			if providerKind == nil {
				if pk, ok := stringFromData(ev.DataJSON, "provider_kind"); ok {
					providerKind = &pk
				}
				if am, ok := stringFromData(ev.DataJSON, "api_mode"); ok {
					apiMode = &am
				}
			}
		case "tool.call":
			stats.ToolCalls++
		case "run.provider_fallback":
			stats.ProviderFallbacks++
		}
	}
	return stats, providerKind, apiMode
}

func stringFromData(dataJSON any, key string) (string, bool) {
	m, ok := dataJSON.(map[string]any)
	if !ok {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
