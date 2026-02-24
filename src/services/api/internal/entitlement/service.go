package entitlement

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const cacheTTL = 5 * time.Minute
const cachePrefix = "arkloop:entitlement:"

// 平台默认值，所有层级都无配置时使用。
var platformDefaults = map[string]EntitlementValue{
	"quota.runs_per_month":       {Raw: "100", Type: "int"},
	"quota.tokens_per_month":     {Raw: "1000000", Type: "int"},
	"limit.concurrent_runs":      {Raw: "10", Type: "int"},
	"limit.team_members":         {Raw: "50", Type: "int"},
	"feature.byok_enabled":       {Raw: "false", Type: "bool"},
	"feature.mcp_remote_enabled": {Raw: "false", Type: "bool"},
	"invite.max_codes_per_user":  {Raw: "1", Type: "int"},
	"invite.default_max_uses":    {Raw: "1", Type: "int"},
	"credit.initial_grant":       {Raw: "1000", Type: "int"},
	"credit.invite_reward":       {Raw: "500", Type: "int"},
	"credit.invitee_reward":      {Raw: "200", Type: "int"},
}

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
	entitlementsRepo     *data.EntitlementsRepository
	subscriptionRepo     *data.SubscriptionRepository
	planRepo             *data.PlanRepository
	platformSettingsRepo *data.PlatformSettingsRepository
	rdb                  *redis.Client
}

func NewService(
	entitlementsRepo *data.EntitlementsRepository,
	subscriptionRepo *data.SubscriptionRepository,
	planRepo *data.PlanRepository,
	rdb *redis.Client,
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
	return &Service{
		entitlementsRepo: entitlementsRepo,
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		rdb:              rdb,
	}, nil
}

// SetPlatformSettingsRepo 注入平台设置仓储（可选，未注入时仅使用硬编码默认值）。
func (s *Service) SetPlatformSettingsRepo(repo *data.PlatformSettingsRepository) {
	s.platformSettingsRepo = repo
}

// Resolve 按优先级返回权益值：org override (未过期) > plan entitlement > 平台默认值。
func (s *Service) Resolve(ctx context.Context, orgID uuid.UUID, key string) (EntitlementValue, error) {
	// 尝试从缓存读取
	if s.rdb != nil {
		cached, err := s.getFromCache(ctx, orgID, key)
		if err == nil && cached != nil {
			return *cached, nil
		}
	}

	resolved, err := s.resolveFromDB(ctx, orgID, key)
	if err != nil {
		return EntitlementValue{}, err
	}

	// 写入缓存
	if s.rdb != nil {
		s.setCache(ctx, orgID, key, resolved)
	}

	return resolved, nil
}

func (s *Service) resolveFromDB(ctx context.Context, orgID uuid.UUID, key string) (EntitlementValue, error) {
	// 1. org override (未过期)
	override, err := s.entitlementsRepo.GetOverride(ctx, orgID, key)
	if err != nil {
		return EntitlementValue{}, fmt.Errorf("entitlement.Resolve override: %w", err)
	}
	if override != nil {
		return EntitlementValue{Raw: override.Value, Type: override.ValueType}, nil
	}

	// 2. plan entitlement (通过 subscription 关联)
	sub, err := s.subscriptionRepo.GetActiveByOrgID(ctx, orgID)
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

	// 3. 平台设置（数据库可配置的默认值）
	if s.platformSettingsRepo != nil {
		setting, err := s.platformSettingsRepo.Get(ctx, key)
		if err == nil && setting != nil {
			valType := "string"
			if _, ok := platformDefaults[key]; ok {
				valType = platformDefaults[key].Type
			}
			return EntitlementValue{Raw: setting.Value, Type: valType}, nil
		}
	}

	// 4. 硬编码平台默认值
	if def, ok := platformDefaults[key]; ok {
		return def, nil
	}

	return EntitlementValue{}, fmt.Errorf("entitlement: unknown key %q", key)
}

func (s *Service) getFromCache(ctx context.Context, orgID uuid.UUID, key string) (*EntitlementValue, error) {
	cacheKey := cachePrefix + orgID.String() + ":" + key
	raw, err := s.rdb.Get(ctx, cacheKey).Result()
	if err != nil {
		return nil, err
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
	return nil, fmt.Errorf("invalid cache format")
}

func (s *Service) setCache(ctx context.Context, orgID uuid.UUID, key string, val EntitlementValue) {
	cacheKey := cachePrefix + orgID.String() + ":" + key
	encoded := val.Type + ":" + val.Raw
	_ = s.rdb.Set(ctx, cacheKey, encoded, cacheTTL).Err()
}

// InvalidateCache 删除指定 org + key 的缓存，用于 override 变更后立即生效。
func (s *Service) InvalidateCache(ctx context.Context, orgID uuid.UUID, key string) {
	if s.rdb == nil {
		return
	}
	cacheKey := cachePrefix + orgID.String() + ":" + key
	_ = s.rdb.Del(ctx, cacheKey).Err()
}
