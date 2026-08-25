package featureflag

import (
	"context"
	"fmt"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

// FlagQuerier 是 Service 所需的最小数据访问接口，方便单测注入 stub。
type FlagQuerier interface {
	GetFlag(ctx context.Context, key string) (*data.FeatureFlag, error)
	GetOrgOverride(ctx context.Context, accountID uuid.UUID, flagKey string) (*data.AccountFeatureOverride, error)
}

type Service struct {
	repo FlagQuerier
}

func NewService(repo FlagQuerier) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("featureflag: repo must not be nil")
	}
	return &Service{repo: repo}, nil
}

// IsEnabled 返回 account 是否启用指定 feature flag。
// 优先级：account override > flag 全局 default_value > 报错（flag 不存在）。
func (s *Service) IsEnabled(ctx context.Context, accountID uuid.UUID, flagKey string) (bool, error) {
	// 1. account override
	override, err := s.repo.GetOrgOverride(ctx, accountID, flagKey)
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

// IsGloballyEnabled 返回 flag 的全局 default_value，不涉及 account override。
// 用于注册等无 account 上下文的场景。flag 不存在时返回 false + error。
func (s *Service) IsGloballyEnabled(ctx context.Context, flagKey string) (bool, error) {
	flag, err := s.repo.GetFlag(ctx, flagKey)
	if err != nil {
		return false, fmt.Errorf("featureflag.IsGloballyEnabled: %w", err)
	}
	if flag == nil {
		return false, nil
	}

	return flag.DefaultValue, nil
}
