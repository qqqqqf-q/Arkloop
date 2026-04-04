//go:build desktop

package app

import (
	"context"
	"strconv"
	"strings"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
)

func resolveDesktopContextCompact(ctx context.Context, db data.DesktopDB) (pipeline.ContextCompactSettings, error) {
	if db == nil {
		return pipeline.ContextCompactSettings{}, nil
	}
	registry := sharedconfig.DefaultRegistry()
	resolver, err := sharedconfig.NewResolver(registry, sharedconfig.NewPGXStoreQuerier(db), nil, 0)
	if err != nil {
		return pipeline.ContextCompactSettings{}, err
	}
	scope := sharedconfig.Scope{}
	persistPct := desktopResolveNonNegInt(ctx, resolver, registry, "context.compact.persist_trigger_context_pct", scope, 0)
	if persistPct > 100 {
		persistPct = 100
	}
	return pipeline.ContextCompactSettings{
		Enabled:                     desktopResolveBool(ctx, resolver, registry, "context.compact.enabled", scope, false),
		MaxMessages:                 desktopResolveNonNegInt(ctx, resolver, registry, "context.compact.max_messages", scope, 0),
		MaxUserMessageTokens:        desktopResolveNonNegInt(ctx, resolver, registry, "context.compact.max_user_message_tokens", scope, 0),
		MaxTotalTextTokens:          desktopResolveNonNegInt(ctx, resolver, registry, "context.compact.max_total_text_tokens", scope, 0),
		MaxUserTextBytes:            desktopResolveNonNegInt(ctx, resolver, registry, "context.compact.max_user_text_bytes", scope, 0),
		MaxTotalTextBytes:           desktopResolveNonNegInt(ctx, resolver, registry, "context.compact.max_total_text_bytes", scope, 0),
		PersistEnabled:              desktopResolveBool(ctx, resolver, registry, "context.compact.persist_enabled", scope, false),
		PersistTriggerApproxTokens:  desktopResolvePositiveInt(ctx, resolver, registry, "context.compact.persist_trigger_approx_tokens", scope, 0),
		PersistTriggerContextPct:    persistPct,
		FallbackContextWindowTokens: desktopResolvePositiveInt(ctx, resolver, registry, "context.compact.fallback_context_window_tokens", scope, 128000),
		PersistKeepLastMessages:     desktopResolvePositiveInt(ctx, resolver, registry, "context.compact.persist_keep_last_messages", scope, 40),
	}, nil
}

func desktopResolveBool(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort bool) bool {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok {
			if v, err := strconv.ParseBool(strings.TrimSpace(entry.Default)); err == nil {
				fallback = v
			}
		}
	}
	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil {
		return fallback
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func desktopResolvePositiveInt(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort int) int {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok {
			if v, err := strconv.Atoi(strings.TrimSpace(entry.Default)); err == nil && v > 0 {
				fallback = v
			}
		}
	}
	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func desktopResolveNonNegInt(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort int) int {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok {
			if v, err := strconv.Atoi(strings.TrimSpace(entry.Default)); err == nil && v >= 0 {
				fallback = v
			}
		}
	}
	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return fallback
	}
	return v
}
