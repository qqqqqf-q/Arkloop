package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Scope struct {
	ProjectID *uuid.UUID
}

type Resolver interface {
	Resolve(ctx context.Context, key string, scope Scope) (string, error)
	ResolvePrefix(ctx context.Context, prefix string, scope Scope) (map[string]string, error)
}

type ResolverWithSource interface {
	ResolveWithSource(ctx context.Context, key string, scope Scope) (string, string, error)
}

type Invalidator interface {
	Invalidate(ctx context.Context, key string, scope Scope) error
}

type ResolverImpl struct {
	registry *Registry
	store    Store
	cache    Cache
	cacheTTL time.Duration
}

func NewResolver(registry *Registry, store Store, cache Cache, cacheTTL time.Duration) (*ResolverImpl, error) {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &ResolverImpl{
		registry: registry,
		store:    store,
		cache:    cache,
		cacheTTL: cacheTTL,
	}, nil
}

func (r *ResolverImpl) Resolve(ctx context.Context, key string, scope Scope) (string, error) {
	value, _, err := r.ResolveWithSource(ctx, key, scope)
	return value, err
}

func (r *ResolverImpl) ResolveWithSource(ctx context.Context, key string, scope Scope) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.registry == nil {
		return "", "", fmt.Errorf("config resolver not initialized")
	}

	entry, ok := r.registry.Get(key)
	if !ok {
		return "", "", fmt.Errorf("config key not registered: %s", key)
	}

	if value, ok := resolveFromEnv(entry); ok {
		return value, "env", nil
	}

	if scope.ProjectID != nil && (entry.Scope == ScopeProject || entry.Scope == ScopeBoth) {
		val, found, err := r.getProjectSetting(ctx, *scope.ProjectID, entry.Key)
		if err != nil {
			return "", "", err
		}
		if found {
			return val, "project_db", nil
		}
	}

	if entry.Scope == ScopePlatform || entry.Scope == ScopeBoth {
		val, found, err := r.getPlatformSetting(ctx, entry.Key)
		if err != nil {
			return "", "", err
		}
		if found {
			return val, "platform_db", nil
		}
	}

	return entry.Default, "default", nil
}

func (r *ResolverImpl) ResolvePrefix(ctx context.Context, prefix string, scope Scope) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.registry == nil {
		return nil, fmt.Errorf("config resolver not initialized")
	}

	entries := r.registry.ListByPrefix(prefix)
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		val, err := r.Resolve(ctx, e.Key, scope)
		if err != nil {
			return nil, err
		}
		out[e.Key] = val
	}
	return out, nil
}

func (r *ResolverImpl) Invalidate(ctx context.Context, key string, scope Scope) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.cache == nil || r.cacheTTL <= 0 {
		return nil
	}

	if scope.ProjectID != nil {
		return r.cache.Del(ctx, projectCacheKey(*scope.ProjectID, key))
	}
	return r.cache.Del(ctx, platformCacheKey(key))
}

type cachedSetting struct {
	Found bool   `json:"found"`
	Value string `json:"value,omitempty"`
}

func (r *ResolverImpl) getPlatformSetting(ctx context.Context, key string) (string, bool, error) {
	if r == nil || r.store == nil || r.cache == nil || r.cacheTTL <= 0 {
		if r == nil || r.store == nil {
			return "", false, nil
		}
		return r.store.GetPlatformSetting(ctx, key)
	}

	cacheKey := platformCacheKey(key)
	raw, err := r.cache.Get(ctx, cacheKey)
	if err == nil && len(raw) > 0 {
		var item cachedSetting
		if jsonErr := json.Unmarshal(raw, &item); jsonErr == nil {
			return item.Value, item.Found, nil
		}
	}

	val, found, dbErr := r.store.GetPlatformSetting(ctx, key)
	if dbErr != nil {
		return "", false, dbErr
	}

	payload, _ := json.Marshal(cachedSetting{Found: found, Value: val})
	_ = r.cache.Set(ctx, cacheKey, payload, r.cacheTTL)
	return val, found, nil
}

func (r *ResolverImpl) getProjectSetting(ctx context.Context, projectID uuid.UUID, key string) (string, bool, error) {
	if r == nil || r.store == nil || r.cache == nil || r.cacheTTL <= 0 {
		if r == nil || r.store == nil {
			return "", false, nil
		}
		return r.store.GetProjectSetting(ctx, projectID, key)
	}

	cacheKey := projectCacheKey(projectID, key)
	raw, err := r.cache.Get(ctx, cacheKey)
	if err == nil && len(raw) > 0 {
		var item cachedSetting
		if jsonErr := json.Unmarshal(raw, &item); jsonErr == nil {
			return item.Value, item.Found, nil
		}
	}

	val, found, dbErr := r.store.GetProjectSetting(ctx, projectID, key)
	if dbErr != nil {
		return "", false, dbErr
	}

	payload, _ := json.Marshal(cachedSetting{Found: found, Value: val})
	_ = r.cache.Set(ctx, cacheKey, payload, r.cacheTTL)
	return val, found, nil
}

func resolveFromEnv(e Entry) (string, bool) {
	keys := e.EnvKeys
	if len(keys) == 0 {
		keys = []string{deriveEnvKey(e.Key)}
	}
	for _, k := range keys {
		raw, ok := os.LookupEnv(k)
		if !ok {
			continue
		}
		val := strings.TrimSpace(raw)
		if val == "" {
			continue
		}
		return val, true
	}
	return "", false
}

func deriveEnvKey(key string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(key), ".", "_")
	normalized = strings.ToUpper(normalized)
	return "ARKLOOP_" + normalized
}

func platformCacheKey(key string) string {
	return "arkloop:config:v1:platform:" + key
}

func projectCacheKey(projectID uuid.UUID, key string) string {
	return "arkloop:config:v1:project:" + projectID.String() + ":" + key
}
