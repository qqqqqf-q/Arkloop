package http

import (
	"encoding/json"
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

const (
	creditsModeDeductionPolicy = "credit.deduction_policy"
	creditsModeRunsPerMonth    = "quota.runs_per_month"
	creditsModeTokensPerMonth  = "quota.tokens_per_month"

	// multiplier=0 的策略 JSON，表示不扣积分。
	creditsModeDisabledPolicy = `{"tiers":[{"multiplier":0}]}`
)

type creditsModeResponse struct {
	Enabled bool `json:"enabled"`
}

type creditsModeRequest struct {
	Enabled bool `json:"enabled"`
}

// adminCreditsMode 处理 GET/PUT /v1/admin/credits/mode。
func adminCreditsMode(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
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

		switch r.Method {
		case nethttp.MethodGet:
			creditsModeGet(w, r, traceID, settingsRepo)
		case nethttp.MethodPut:
			creditsModeSet(w, r, traceID, settingsRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func creditsModeGet(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	settingsRepo *data.PlatformSettingsRepository,
) {
	if settingsRepo == nil {
		// 无 DB 时默认 enabled=true（不阻断 SaaS 模式）
		writeJSON(w, traceID, nethttp.StatusOK, creditsModeResponse{Enabled: true})
		return
	}

	setting, err := settingsRepo.Get(r.Context(), creditsModeDeductionPolicy)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	enabled := true
	if setting != nil {
		var policy struct {
			Tiers []struct {
				Multiplier float64 `json:"multiplier"`
			} `json:"tiers"`
		}
		if json.Unmarshal([]byte(setting.Value), &policy) == nil &&
			len(policy.Tiers) == 1 && policy.Tiers[0].Multiplier == 0 {
			enabled = false
		}
	}

	writeJSON(w, traceID, nethttp.StatusOK, creditsModeResponse{Enabled: enabled})
}

func creditsModeSet(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	settingsRepo *data.PlatformSettingsRepository,
) {
	if settingsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "db.not_configured", "database not available", traceID, nil)
		return
	}

	var body creditsModeRequest
	if err := decodeJSON(r, &body); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	ctx := r.Context()
	if body.Enabled {
		// 恢复默认：删除三个 key（Lite 自部署预设）
		for _, key := range []string{creditsModeDeductionPolicy, creditsModeRunsPerMonth, creditsModeTokensPerMonth} {
			if err := settingsRepo.Delete(ctx, key); err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
	} else {
		// 关闭积分：zero multiplier + 无限配额（quota=0 表示不限）
		presets := map[string]string{
			creditsModeDeductionPolicy: creditsModeDisabledPolicy,
			creditsModeRunsPerMonth:    "0",
			creditsModeTokensPerMonth:  "0",
		}
		for key, val := range presets {
			if _, err := settingsRepo.Set(ctx, key, val); err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
	}

	w.WriteHeader(nethttp.StatusNoContent)
}


