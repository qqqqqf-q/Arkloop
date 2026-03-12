package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"

	"github.com/redis/go-redis/v9"
)

const (
	settingGatewayIPMode              = "gateway.ip_mode"
	settingGatewayTrustedCIDRs        = "gateway.trusted_cidrs"
	settingGatewayRiskRejectThreshold = "gateway.risk_reject_threshold"
	settingGatewayRateLimitCapacity   = "gateway.ratelimit_capacity"
	settingGatewayRateLimitPerMinute  = "gateway.ratelimit_rate_per_minute"

	// gatewayConfigRedisKey 与 gateway 服务约定的 Redis key
	gatewayConfigRedisKey = "arkloop:gateway:config"

	defaultGatewayRateLimitCapacity  = 600.0
	defaultGatewayRateLimitPerMinute = 300.0
)

var validIPModes = map[string]struct{}{
	"direct":        {},
	"cloudflare":    {},
	"trusted_proxy": {},
}

type gatewayConfigResponse struct {
	IPMode              string   `json:"ip_mode"`
	TrustedCIDRs        []string `json:"trusted_cidrs"`
	RiskRejectThreshold int      `json:"risk_reject_threshold"`
	RateLimitCapacity   float64  `json:"rate_limit_capacity"`
	RateLimitPerMinute  float64  `json:"rate_limit_per_minute"`
}

type updateGatewayConfigRequest struct {
	IPMode              string   `json:"ip_mode"`
	TrustedCIDRs        []string `json:"trusted_cidrs"`
	RiskRejectThreshold *int     `json:"risk_reject_threshold"`
	RateLimitCapacity   *float64 `json:"rate_limit_capacity"`
	RateLimitPerMinute  *float64 `json:"rate_limit_per_minute"`
}

func adminGatewayConfigEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	settingsRepo *data.PlatformSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
	rdb *redis.Client,
	resolver sharedconfig.Resolver,
	invalidator sharedconfig.Invalidator,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			cfg, err := loadGatewayConfig(r.Context(), resolver)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, cfg)

		case nethttp.MethodPut:
			var body updateGatewayConfigRequest
			if err := httpkit.DecodeJSON(r, &body); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}

			if err := validateGatewayConfig(body); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
				return
			}

			if err := saveGatewayConfig(r.Context(), settingsRepo, body, invalidator); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			// 同步写入 Redis，让 Gateway 30s 内热更新
			if rdb != nil {
				_ = pushGatewayConfigToRedis(r.Context(), rdb, body)
			}

			cfg, err := loadGatewayConfig(r.Context(), resolver)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, cfg)

		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func validateGatewayConfig(body updateGatewayConfigRequest) error {
	if body.IPMode != "" {
		if _, ok := validIPModes[body.IPMode]; !ok {
			return fmt.Errorf("ip_mode must be one of: direct, cloudflare, trusted_proxy")
		}
	}

	for _, cidr := range body.TrustedCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid CIDR %q: %s", cidr, err.Error())
		}
	}

	if body.RiskRejectThreshold != nil {
		v := *body.RiskRejectThreshold
		if v < 0 || v > 100 {
			return fmt.Errorf("risk_reject_threshold must be 0-100")
		}
	}
	if body.RateLimitCapacity != nil && *body.RateLimitCapacity <= 0 {
		return fmt.Errorf("rate_limit_capacity must be positive")
	}
	if body.RateLimitPerMinute != nil && *body.RateLimitPerMinute <= 0 {
		return fmt.Errorf("rate_limit_per_minute must be positive")
	}

	return nil
}

func saveGatewayConfig(ctx context.Context, settingsRepo *data.PlatformSettingsRepository, body updateGatewayConfigRequest, invalidator sharedconfig.Invalidator) error {
	if body.IPMode != "" {
		if _, err := settingsRepo.Set(ctx, settingGatewayIPMode, body.IPMode); err != nil {
			return err
		}
		if invalidator != nil {
			if err := invalidator.Invalidate(ctx, settingGatewayIPMode, sharedconfig.Scope{}); err != nil {
				slog.Warn("cache invalidation failed", "key", settingGatewayIPMode, "error", err)
			}
		}
	}

	cidrs := filterCIDRs(body.TrustedCIDRs)
	encoded, err := json.Marshal(cidrs)
	if err != nil {
		return fmt.Errorf("marshal trusted CIDRs: %w", err)
	}
	if _, err := settingsRepo.Set(ctx, settingGatewayTrustedCIDRs, string(encoded)); err != nil {
		return err
	}
	if invalidator != nil {
		if err := invalidator.Invalidate(ctx, settingGatewayTrustedCIDRs, sharedconfig.Scope{}); err != nil {
			slog.Warn("cache invalidation failed", "key", settingGatewayTrustedCIDRs, "error", err)
		}
	}

	if body.RiskRejectThreshold != nil {
		val := fmt.Sprintf("%d", *body.RiskRejectThreshold)
		if _, err := settingsRepo.Set(ctx, settingGatewayRiskRejectThreshold, val); err != nil {
			return err
		}
		if invalidator != nil {
			if err := invalidator.Invalidate(ctx, settingGatewayRiskRejectThreshold, sharedconfig.Scope{}); err != nil {
				slog.Warn("cache invalidation failed", "key", settingGatewayRiskRejectThreshold, "error", err)
			}
		}
	}
	if body.RateLimitCapacity != nil {
		val := strconv.FormatFloat(*body.RateLimitCapacity, 'f', -1, 64)
		if _, err := settingsRepo.Set(ctx, settingGatewayRateLimitCapacity, val); err != nil {
			return err
		}
		if invalidator != nil {
			if err := invalidator.Invalidate(ctx, settingGatewayRateLimitCapacity, sharedconfig.Scope{}); err != nil {
				slog.Warn("cache invalidation failed", "key", settingGatewayRateLimitCapacity, "error", err)
			}
		}
	}
	if body.RateLimitPerMinute != nil {
		val := strconv.FormatFloat(*body.RateLimitPerMinute, 'f', -1, 64)
		if _, err := settingsRepo.Set(ctx, settingGatewayRateLimitPerMinute, val); err != nil {
			return err
		}
		if invalidator != nil {
			if err := invalidator.Invalidate(ctx, settingGatewayRateLimitPerMinute, sharedconfig.Scope{}); err != nil {
				slog.Warn("cache invalidation failed", "key", settingGatewayRateLimitPerMinute, "error", err)
			}
		}
	}

	return nil
}

func loadGatewayConfig(ctx context.Context, resolver sharedconfig.Resolver) (*gatewayConfigResponse, error) {
	cfg := &gatewayConfigResponse{
		IPMode:             "direct",
		TrustedCIDRs:       []string{},
		RateLimitCapacity:  defaultGatewayRateLimitCapacity,
		RateLimitPerMinute: defaultGatewayRateLimitPerMinute,
	}

	if resolver != nil {
		m, err := resolver.ResolvePrefix(ctx, "gateway.", sharedconfig.Scope{})
		if err != nil {
			return nil, err
		}

		if raw := strings.TrimSpace(m[settingGatewayIPMode]); raw != "" {
			if _, ok := validIPModes[raw]; ok {
				cfg.IPMode = raw
			}
		}

		cfg.TrustedCIDRs = parseTrustedCIDRs(m[settingGatewayTrustedCIDRs])

		if raw := strings.TrimSpace(m[settingGatewayRiskRejectThreshold]); raw != "" {
			var v int
			if _, err := fmt.Sscanf(raw, "%d", &v); err == nil {
				cfg.RiskRejectThreshold = v
			}
		}

		if raw := strings.TrimSpace(m[settingGatewayRateLimitCapacity]); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
				cfg.RateLimitCapacity = v
			}
		}
		if raw := strings.TrimSpace(m[settingGatewayRateLimitPerMinute]); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
				cfg.RateLimitPerMinute = v
			}
		}
	}

	return cfg, nil
}

func parseTrustedCIDRs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}

	var cidrs []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &cidrs); err == nil {
			return filterCIDRs(cidrs)
		}
	}

	parts := strings.Split(raw, ",")
	return filterCIDRs(parts)
}

// gatewayRedisPayload 与 gateway 服务的 gatewayDynamicConfig 结构对应。
type gatewayRedisPayload struct {
	IPMode              string   `json:"ip_mode,omitempty"`
	TrustedCIDRs        []string `json:"trusted_cidrs,omitempty"`
	RiskRejectThreshold *int     `json:"risk_reject_threshold,omitempty"`
	RateLimitCapacity   float64  `json:"rate_limit_capacity,omitempty"`
	RateLimitPerMinute  float64  `json:"rate_limit_per_minute,omitempty"`
}

func pushGatewayConfigToRedis(ctx context.Context, rdb *redis.Client, body updateGatewayConfigRequest) error {
	payload := gatewayRedisPayload{
		IPMode:       body.IPMode,
		TrustedCIDRs: filterCIDRs(body.TrustedCIDRs),
	}
	if body.RiskRejectThreshold != nil {
		payload.RiskRejectThreshold = body.RiskRejectThreshold
	}
	if body.RateLimitCapacity != nil {
		payload.RateLimitCapacity = *body.RateLimitCapacity
	}
	if body.RateLimitPerMinute != nil {
		payload.RateLimitPerMinute = *body.RateLimitPerMinute
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return rdb.Set(ctx, gatewayConfigRedisKey, raw, 0).Err()
}

func filterCIDRs(cidrs []string) []string {
	result := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		if s := strings.TrimSpace(c); s != "" {
			result = append(result, s)
		}
	}
	return result
}
