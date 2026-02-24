package featureflag

import (
	"context"
	"fmt"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const cacheTTL = 5 * time.Minute
const cachePrefix = "arkloop:feat:"

// FlagQuerier 是 Service 所需的最小数据访问接口，方便单测注入 stub。
type FlagQuerier interface {
	GetFlag(ctx context.Context, key string) (*data.FeatureFlag, error)
	GetOrgOverride(ctx context.Context, orgID uuid.UUID, flagKey string) (*data.OrgFeatureOverride, error)
}

type Service struct {
	repo FlagQuerier
	rdb  *redis.Client
}

func NewService(repo FlagQuerier, rdb *redis.Client) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("featureflag: repo must not be nil")
	}
	return &Service{repo: repo, rdb: rdb}, nil
}

// IsEnabled 返回 org 是否启用指定 feature flag。
// 优先级：org override > flag 全局 default_value > 报错（flag 不存在）。
func (s *Service) IsEnabled(ctx context.Context, orgID uuid.UUID, flagKey string) (bool, error) {
	if s.rdb != nil {
		if cached, ok := s.getFromCache(ctx, orgID, flagKey); ok {
			return cached, nil
		}
	}

	enabled, err := s.resolveFromDB(ctx, orgID, flagKey)
	if err != nil {
		return false, err
	}

	if s.rdb != nil {
		s.setCache(ctx, orgID, flagKey, enabled)
	}

	return enabled, nil
}

func (s *Service) resolveFromDB(ctx context.Context, orgID uuid.UUID, flagKey string) (bool, error) {
	// 1. org override
	override, err := s.repo.GetOrgOverride(ctx, orgID, flagKey)
	if err != nil {
		return false, fmt.Errorf("featureflag.IsEnabled override: %w", err)
	}
	if override != nil {
		return override.Enabled, nil
	}

	// 2. flag 全局默认值
	flag, err := s.repo.GetFlag(ctx, flagKey)
	if err != nil {
		return false, fmt.Errorf("featureflag.IsEnabled flag: %w", err)
	}
	if flag == nil {
		return false, fmt.Errorf("featureflag: unknown flag %q", flagKey)
	}

	return flag.DefaultValue, nil
}

func (s *Service) getFromCache(ctx context.Context, orgID uuid.UUID, flagKey string) (bool, bool) {
	cacheKey := cachePrefix + orgID.String() + ":" + flagKey
	val, err := s.rdb.Get(ctx, cacheKey).Result()
	if err != nil {
		return false, false
	}
	return val == "1", true
}

func (s *Service) setCache(ctx context.Context, orgID uuid.UUID, flagKey string, enabled bool) {
	cacheKey := cachePrefix + orgID.String() + ":" + flagKey
	v := "0"
	if enabled {
		v = "1"
	}
	_ = s.rdb.Set(ctx, cacheKey, v, cacheTTL).Err()
}

// IsGloballyEnabled 返回 flag 的全局 default_value，不涉及 org override。
// 用于注册等无 org 上下文的场景。flag 不存在时返回 false + error。
func (s *Service) IsGloballyEnabled(ctx context.Context, flagKey string) (bool, error) {
	if s.rdb != nil {
		if cached, ok := s.getGlobalFromCache(ctx, flagKey); ok {
			return cached, nil
		}
	}

	flag, err := s.repo.GetFlag(ctx, flagKey)
	if err != nil {
		return false, fmt.Errorf("featureflag.IsGloballyEnabled: %w", err)
	}
	if flag == nil {
		return false, fmt.Errorf("featureflag: unknown flag %q", flagKey)
	}

	if s.rdb != nil {
		s.setGlobalCache(ctx, flagKey, flag.DefaultValue)
	}
	return flag.DefaultValue, nil
}

func (s *Service) getGlobalFromCache(ctx context.Context, flagKey string) (bool, bool) {
	cacheKey := cachePrefix + "global:" + flagKey
	val, err := s.rdb.Get(ctx, cacheKey).Result()
	if err != nil {
		return false, false
	}
	return val == "1", true
}

func (s *Service) setGlobalCache(ctx context.Context, flagKey string, enabled bool) {
	cacheKey := cachePrefix + "global:" + flagKey
	v := "0"
	if enabled {
		v = "1"
	}
	_ = s.rdb.Set(ctx, cacheKey, v, cacheTTL).Err()
}

// InvalidateGlobalCache 清除 flag 全局 default_value 的缓存。
func (s *Service) InvalidateGlobalCache(ctx context.Context, flagKey string) {
	if s.rdb == nil {
		return
	}
	_ = s.rdb.Del(ctx, cachePrefix+"global:"+flagKey).Err()
}

// InvalidateCache 清除指定 org + flag 的缓存，用于 override 变更后立即生效。
func (s *Service) InvalidateCache(ctx context.Context, orgID uuid.UUID, flagKey string) {
	if s.rdb == nil {
		return
	}
	cacheKey := cachePrefix + orgID.String() + ":" + flagKey
	_ = s.rdb.Del(ctx, cacheKey).Err()
}

