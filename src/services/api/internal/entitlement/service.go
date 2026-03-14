package entitlement

import (
	"context"
	"crypto/hmac"
	"fmt"
	"strconv"
	"time"

	"arkloop/services/api/internal/data"
	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const cacheTTL = 5 * time.Minute
const cachePrefix = "arkloop:entitlement:"

type EntitlementValue struct {
	Raw  string
	Type string
}

func (v EntitlementValue) Int() int64 {
	n, _ := strconv.ParseInt(v.Raw, 10, 64)
	return n
}

func (v EntitlementValue) Bool() bool {
	b, _ := strconv.ParseBool(v.Raw)
	return b
}

func (v EntitlementValue) String() string {
	return v.Raw
}

type Service struct {
	entitlementsRepo *data.EntitlementsRepository
	subscriptionRepo *data.SubscriptionRepository
	planRepo         *data.PlanRepository
	rdb              *redis.Client

	configResolver sharedconfig.Resolver
	registry       *sharedconfig.Registry
}

func NewService(
	entitlementsRepo *data.EntitlementsRepository,
	subscriptionRepo *data.SubscriptionRepository,
	planRepo *data.PlanRepository,
	rdb *redis.Client,
	configResolver sharedconfig.Resolver,
) (*Service, error) {
	if entitlementsRepo == nil {
		return nil, fmt.Errorf("entitlement: entitlements_repo must not be nil")
	}
	if subscriptionRepo == nil {
		return nil, fmt.Errorf("entitlement: subscription_repo must not be nil")
	}
	if planRepo == nil {
		return nil, fmt.Errorf("entitlement: plan_repo must not be nil")
	}

	registry := sharedconfig.DefaultRegistry()
	if configResolver == nil {
		fallback, _ := sharedconfig.NewResolver(registry, nil, nil, 0)
		configResolver = fallback
	}
	return &Service{
		entitlementsRepo: entitlementsRepo,
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		rdb:              rdb,
		configResolver:   configResolver,
		registry:         registry,
	}, nil
}

// Resolve 按优先级返回权益值：account override (未过期) > plan entitlement > 平台默认值。
func (s *Service) Resolve(ctx context.Context, accountID uuid.UUID, key string) (EntitlementValue, error) {
	// 尝试从缓存读取
	if s.rdb != nil {
		cached, err := s.getFromCache(ctx, accountID, key)
		if err == nil && cached != nil {
			return *cached, nil
		}
	}

	resolved, err := s.resolveFromDB(ctx, accountID, key)
	if err != nil {
		return EntitlementValue{}, err
	}

	// 写入缓存
	if s.rdb != nil {
		s.setCache(ctx, accountID, key, resolved)
	}

	return resolved, nil
}

func (s *Service) resolveFromDB(ctx context.Context, accountID uuid.UUID, key string) (EntitlementValue, error) {
	// 1. account override (未过期)
	override, err := s.entitlementsRepo.GetOverride(ctx, accountID, key)
	if err != nil {
		return EntitlementValue{}, fmt.Errorf("entitlement.Resolve override: %w", err)
	}
	if override != nil {
		return EntitlementValue{Raw: override.Value, Type: override.ValueType}, nil
	}

	// 2. plan entitlement (通过 subscription 关联)
	sub, err := s.subscriptionRepo.GetActiveByAccountID(ctx, accountID)
	if err != nil {
		return EntitlementValue{}, fmt.Errorf("entitlement.Resolve subscription: %w", err)
	}
	if sub != nil {
		pe, err := s.entitlementsRepo.GetPlanEntitlement(ctx, sub.PlanID, key)
		if err != nil {
			return EntitlementValue{}, fmt.Errorf("entitlement.Resolve plan_entitlement: %w", err)
		}
		if pe != nil {
			return EntitlementValue{Raw: pe.Value, Type: pe.ValueType}, nil
		}
	}

	// 3. 平台默认值：ENV > platform_settings > registry default
	if s == nil || s.configResolver == nil {
		return EntitlementValue{}, fmt.Errorf("entitlement: config resolver not initialized")
	}
	raw, err := s.configResolver.Resolve(ctx, key, sharedconfig.Scope{})
	if err != nil {
		return EntitlementValue{}, fmt.Errorf("entitlement: resolve platform default %q: %w", key, err)
	}
	return EntitlementValue{Raw: raw, Type: entitlementTypeForKey(key, s.registry)}, nil
}

func (s *Service) getFromCache(ctx context.Context, accountID uuid.UUID, key string) (*EntitlementValue, error) {
	if !sharedent.EntitlementCacheSigningEnabled() {
		return nil, redis.Nil
	}

	cacheKey := cachePrefix + accountID.String() + ":" + key
	sigKey := cacheKey + sharedent.EntitlementCacheSignatureSuffix
	items, err := s.rdb.MGet(ctx, cacheKey, sigKey).Result()
	if err != nil {
		return nil, err
	}
	if len(items) != 2 {
		return nil, fmt.Errorf("invalid cache mget result")
	}

	raw, ok := items[0].(string)
	sig, sigOK := items[1].(string)
	if !ok || !sigOK || raw == "" || sig == "" {
		_ = s.rdb.Del(ctx, cacheKey, sigKey).Err()
		return nil, redis.Nil
	}

	expected, ok := sharedent.ComputeEntitlementCacheSignature(cacheKey, raw)
	if !ok || !hmac.Equal([]byte(sig), []byte(expected)) {
		_ = s.rdb.Del(ctx, cacheKey, sigKey).Err()
		return nil, redis.Nil
	}

	// 缓存格式: "type:value"
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return &EntitlementValue{
				Type: raw[:i],
				Raw:  raw[i+1:],
			}, nil
		}
	}

	_ = s.rdb.Del(ctx, cacheKey, sigKey).Err()
	return nil, fmt.Errorf("invalid cache format")
}

func (s *Service) setCache(ctx context.Context, accountID uuid.UUID, key string, val EntitlementValue) {
	cacheKey := cachePrefix + accountID.String() + ":" + key
	encoded := val.Type + ":" + val.Raw
	if !sharedent.EntitlementCacheSigningEnabled() {
		return
	}
	sig, ok := sharedent.ComputeEntitlementCacheSignature(cacheKey, encoded)
	if !ok {
		return
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, cacheKey, encoded, cacheTTL)
	pipe.Set(ctx, cacheKey+sharedent.EntitlementCacheSignatureSuffix, sig, cacheTTL)
	_, _ = pipe.Exec(ctx)
}

// InvalidateCache 删除指定 account + key 的缓存，用于 override 变更后立即生效。
func (s *Service) InvalidateCache(ctx context.Context, accountID uuid.UUID, key string) {
	if s.rdb == nil {
		return
	}
	if !sharedent.EntitlementCacheSigningEnabled() {
		return
	}
	cacheKey := cachePrefix + accountID.String() + ":" + key
	_ = s.rdb.Del(ctx, cacheKey, cacheKey+sharedent.EntitlementCacheSignatureSuffix).Err()
}

func entitlementTypeForKey(key string, registry *sharedconfig.Registry) string {
	if key == "credit.deduction_policy" {
		return "json"
	}
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}
	if entry, ok := registry.Get(key); ok {
		switch entry.Type {
		case sharedconfig.TypeInt:
			return "int"
		case sharedconfig.TypeBool:
			return "bool"
		default:
			return "string"
		}
	}
	return "string"
}
