package featureflag_test

import (
	"context"
	"testing"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"

	"github.com/google/uuid"
)

// stubFlagQuerier 实现 featureflag.FlagQuerier 接口，不依赖数据库。
type stubFlagQuerier struct {
	flags     map[string]*data.FeatureFlag
	overrides map[string]*data.AccountFeatureOverride // key: accountID+":"+flagKey
}

func newStubQuerier() *stubFlagQuerier {
	return &stubFlagQuerier{
		flags:     make(map[string]*data.FeatureFlag),
		overrides: make(map[string]*data.AccountFeatureOverride),
	}
}

func (s *stubFlagQuerier) GetFlag(_ context.Context, key string) (*data.FeatureFlag, error) {
	return s.flags[key], nil
}

func (s *stubFlagQuerier) GetOrgOverride(_ context.Context, accountID uuid.UUID, flagKey string) (*data.AccountFeatureOverride, error) {
	k := accountID.String() + ":" + flagKey
	return s.overrides[k], nil
}

func (s *stubFlagQuerier) addFlag(key string, defaultValue bool) {
	s.flags[key] = &data.FeatureFlag{
		ID:           uuid.New(),
		Key:          key,
		DefaultValue: defaultValue,
		CreatedAt:    time.Now(),
	}
}

func (s *stubFlagQuerier) addOverride(accountID uuid.UUID, flagKey string, enabled bool) {
	k := accountID.String() + ":" + flagKey
	s.overrides[k] = &data.AccountFeatureOverride{
		AccountID:     accountID,
		FlagKey:   flagKey,
		Enabled:   enabled,
		CreatedAt: time.Now(),
	}
}

func TestIsEnabled_AccountOverrideTakesPrecedence(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, false) // default = false
	stub.addOverride(accountID, flagKey, true) // override = true

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabled, err := svc.IsEnabled(ctx, accountID, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if !enabled {
		t.Error("expected account override (true) to take precedence over flag default (false)")
	}
}

func TestIsEnabled_FlagDefaultUsedWhenNoOverride(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, true) // default = true, no override

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabled, err := svc.IsEnabled(ctx, accountID, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if !enabled {
		t.Error("expected flag default_value (true) when no account override")
	}
}

func TestIsEnabled_UnknownFlagReturnsError(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()

	stub := newStubQuerier() // 空，flag 不存在

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = svc.IsEnabled(ctx, accountID, "nonexistent.flag")
	if err == nil {
		t.Error("expected error for unknown flag, got nil")
	}
}

func TestIsEnabled_AccountOverrideFalseOverridesDefaultTrue(t *testing.T) {
	ctx := context.Background()
	accountID := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, true)           // default = true
	stub.addOverride(accountID, flagKey, false) // override = false

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabled, err := svc.IsEnabled(ctx, accountID, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if enabled {
		t.Error("expected account override (false) to take precedence over flag default (true)")
	}
}

func TestIsEnabled_DifferentAccountGetsOwnOverride(t *testing.T) {
	ctx := context.Background()
	accountA := uuid.New()
	accountB := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, false)
	stub.addOverride(accountA, flagKey, true) // only accountA gets override

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabledA, err := svc.IsEnabled(ctx, accountA, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled accountA: %v", err)
	}
	if !enabledA {
		t.Error("accountA should have override enabled=true")
	}

	enabledB, err := svc.IsEnabled(ctx, accountB, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled accountB: %v", err)
	}
	if enabledB {
		t.Error("accountB should fall back to flag default (false)")
	}
}

func TestNewService_NilRepoReturnsError(t *testing.T) {
	_, err := featureflag.NewService(nil, nil)
	if err == nil {
		t.Error("expected error for nil repo")
	}
}

