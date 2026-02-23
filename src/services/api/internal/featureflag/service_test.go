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
	overrides map[string]*data.OrgFeatureOverride // key: orgID+":"+flagKey
}

func newStubQuerier() *stubFlagQuerier {
	return &stubFlagQuerier{
		flags:     make(map[string]*data.FeatureFlag),
		overrides: make(map[string]*data.OrgFeatureOverride),
	}
}

func (s *stubFlagQuerier) GetFlag(_ context.Context, key string) (*data.FeatureFlag, error) {
	return s.flags[key], nil
}

func (s *stubFlagQuerier) GetOrgOverride(_ context.Context, orgID uuid.UUID, flagKey string) (*data.OrgFeatureOverride, error) {
	k := orgID.String() + ":" + flagKey
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

func (s *stubFlagQuerier) addOverride(orgID uuid.UUID, flagKey string, enabled bool) {
	k := orgID.String() + ":" + flagKey
	s.overrides[k] = &data.OrgFeatureOverride{
		OrgID:     orgID,
		FlagKey:   flagKey,
		Enabled:   enabled,
		CreatedAt: time.Now(),
	}
}

func TestIsEnabled_OrgOverrideTakesPrecedence(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, false) // default = false
	stub.addOverride(orgID, flagKey, true) // override = true

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabled, err := svc.IsEnabled(ctx, orgID, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if !enabled {
		t.Error("expected org override (true) to take precedence over flag default (false)")
	}
}

func TestIsEnabled_FlagDefaultUsedWhenNoOverride(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, true) // default = true, no override

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabled, err := svc.IsEnabled(ctx, orgID, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if !enabled {
		t.Error("expected flag default_value (true) when no org override")
	}
}

func TestIsEnabled_UnknownFlagReturnsError(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	stub := newStubQuerier() // 空，flag 不存在

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = svc.IsEnabled(ctx, orgID, "nonexistent.flag")
	if err == nil {
		t.Error("expected error for unknown flag, got nil")
	}
}

func TestIsEnabled_OrgOverrideFalseOverridesDefaultTrue(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, true)           // default = true
	stub.addOverride(orgID, flagKey, false) // override = false

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabled, err := svc.IsEnabled(ctx, orgID, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if enabled {
		t.Error("expected org override (false) to take precedence over flag default (true)")
	}
}

func TestIsEnabled_DifferentOrgGetsOwnOverride(t *testing.T) {
	ctx := context.Background()
	orgA := uuid.New()
	orgB := uuid.New()
	flagKey := "test.feature"

	stub := newStubQuerier()
	stub.addFlag(flagKey, false)
	stub.addOverride(orgA, flagKey, true) // only orgA gets override

	svc, err := featureflag.NewService(stub, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	enabledA, err := svc.IsEnabled(ctx, orgA, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled orgA: %v", err)
	}
	if !enabledA {
		t.Error("orgA should have override enabled=true")
	}

	enabledB, err := svc.IsEnabled(ctx, orgB, flagKey)
	if err != nil {
		t.Fatalf("IsEnabled orgB: %v", err)
	}
	if enabledB {
		t.Error("orgB should fall back to flag default (false)")
	}
}

func TestNewService_NilRepoReturnsError(t *testing.T) {
	_, err := featureflag.NewService(nil, nil)
	if err == nil {
		t.Error("expected error for nil repo")
	}
}

