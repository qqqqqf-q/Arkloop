package entitlement

import (
	"context"
	"testing"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/creditpolicy"

	"github.com/google/uuid"
)

func TestResolverNilPoolFallbackToDefault(t *testing.T) {
	r := NewResolver(nil, nil)
	val, err := r.Resolve(context.Background(), uuid.New(), "quota.runs_per_month")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "999999" {
		t.Fatalf("expected registry default 999999, got %q", val)
	}
}

func TestResolverNilPoolCountsReturnZero(t *testing.T) {
	r := NewResolver(nil, nil)
	accountID := uuid.New()
	now := time.Now().UTC()

	runs, err := r.CountMonthlyRuns(context.Background(), accountID, now.Year(), int(now.Month()))
	if err != nil {
		t.Fatalf("CountMonthlyRuns error: %v", err)
	}
	if runs != 0 {
		t.Fatalf("CountMonthlyRuns = %d, want 0", runs)
	}

	tokens, err := r.SumMonthlyTokens(context.Background(), accountID, now.Year(), int(now.Month()))
	if err != nil {
		t.Fatalf("SumMonthlyTokens error: %v", err)
	}
	if tokens != 0 {
		t.Fatalf("SumMonthlyTokens = %d, want 0", tokens)
	}

	balance, err := r.GetCreditBalance(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetCreditBalance error: %v", err)
	}
	if balance != 0 {
		t.Fatalf("GetCreditBalance = %d, want 0", balance)
	}
}

func TestResolverResolveIntParsesDefault(t *testing.T) {
	r := NewResolver(nil, nil)
	accountID := uuid.New()

	runs, err := r.ResolveInt(context.Background(), accountID, "quota.runs_per_month")
	if err != nil {
		t.Fatalf("ResolveInt runs: %v", err)
	}
	if runs != 999999 {
		t.Fatalf("ResolveInt runs = %d, want 999999", runs)
	}

	tokens, err := r.ResolveInt(context.Background(), accountID, "quota.tokens_per_month")
	if err != nil {
		t.Fatalf("ResolveInt tokens: %v", err)
	}
	if tokens != 1000000 {
		t.Fatalf("ResolveInt tokens = %d, want 1000000", tokens)
	}
}

func TestMonthRange(t *testing.T) {
	tests := []struct {
		year      int
		month     int
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			year:      2025,
			month:     1,
			wantStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			year:      2024,
			month:     12,
			wantStart: time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			year:      2024,
			month:     2,
			wantStart: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		start, end := monthRange(tt.year, tt.month)
		if !start.Equal(tt.wantStart) {
			t.Fatalf("monthRange(%d, %d) start = %v, want %v", tt.year, tt.month, start, tt.wantStart)
		}
		if !end.Equal(tt.wantEnd) {
			t.Fatalf("monthRange(%d, %d) end = %v, want %v", tt.year, tt.month, end, tt.wantEnd)
		}
	}
}

func TestCacheTypeForKey(t *testing.T) {
	registry := sharedconfig.DefaultRegistry()

	tests := []struct {
		name     string
		key      string
		registry *sharedconfig.Registry
		want     string
	}{
		{name: "credit.deduction_policy 固定返回 json", key: "credit.deduction_policy", registry: registry, want: "json"},
		{name: "credit.deduction_policy nil registry 仍返回 json", key: "credit.deduction_policy", registry: nil, want: "json"},
		{name: "quota.runs_per_month 为 TypeInt", key: "quota.runs_per_month", registry: registry, want: "int"},
		{name: "quota.tokens_per_month 为 TypeInt", key: "quota.tokens_per_month", registry: registry, want: "int"},
		{name: "未注册 key 返回 string", key: "nonexistent.key.xyz", registry: registry, want: "string"},
		{name: "nil registry 回退 DefaultRegistry", key: "quota.runs_per_month", registry: nil, want: "int"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cacheTypeForKey(tt.key, tt.registry)
			if got != tt.want {
				t.Fatalf("cacheTypeForKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestMonthRangeEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		year      int
		month     int
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "month=13 溢出到下一年1月",
			year:      2024,
			month:     13,
			wantStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "闰年2月 end 为3月1日",
			year:      2024,
			month:     2,
			wantStart: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "非闰年2月 end 为3月1日",
			year:      2025,
			month:     2,
			wantStart: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "month=0 溢出到前一年12月",
			year:      2025,
			month:     0,
			wantStart: time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := monthRange(tt.year, tt.month)
			if !start.Equal(tt.wantStart) {
				t.Fatalf("start = %v, want %v", start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Fatalf("end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

func TestResolveDeductionPolicy_NilPool(t *testing.T) {
	r := NewResolver(nil, nil)
	policy, err := r.ResolveDeductionPolicy(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policy.Tiers) != len(creditpolicy.DefaultPolicy.Tiers) {
		t.Fatalf("Tiers len = %d, want %d", len(policy.Tiers), len(creditpolicy.DefaultPolicy.Tiers))
	}
	for i, tier := range policy.Tiers {
		want := creditpolicy.DefaultPolicy.Tiers[i]
		if tier.Multiplier != want.Multiplier {
			t.Fatalf("Tier[%d].Multiplier = %f, want %f", i, tier.Multiplier, want.Multiplier)
		}
	}
}

func TestResolveInt_NonNumericReturnsZero(t *testing.T) {
	r := NewResolver(nil, nil)
	accountID := uuid.New()

	val, err := r.ResolveInt(context.Background(), accountID, "credit.deduction_policy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 0 {
		t.Fatalf("ResolveInt on JSON string = %d, want 0", val)
	}
}

func TestResolveFromDB_NilReceiver(t *testing.T) {
	var r *Resolver
	_, err := r.resolveFromDB(context.Background(), uuid.New(), "quota.runs_per_month")
	if err == nil {
		t.Fatal("nil receiver 应返回 error")
	}
	if err.Error() != "entitlement resolver not initialized" {
		t.Fatalf("错误信息不匹配: %v", err)
	}
}

func TestEntitlementCacheSigningEnabled(t *testing.T) {
	t.Setenv("ARKLOOP_AUTH_JWT_SECRET", "short")
	if EntitlementCacheSigningEnabled() {
		t.Fatal("secret 过短时不应启用签名")
	}

	t.Setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
	if !EntitlementCacheSigningEnabled() {
		t.Fatal("secret 足够长时应启用签名")
	}
}

func TestComputeEntitlementCacheSignature_Deterministic(t *testing.T) {
	t.Setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

	sig1, ok := ComputeEntitlementCacheSignature("arkloop:entitlement:org:key", "int:123")
	if !ok || sig1 == "" {
		t.Fatalf("ComputeEntitlementCacheSignature failed: ok=%v sig=%q", ok, sig1)
	}

	sig2, ok := ComputeEntitlementCacheSignature("arkloop:entitlement:org:key", "int:123")
	if !ok || sig2 == "" {
		t.Fatalf("ComputeEntitlementCacheSignature failed: ok=%v sig=%q", ok, sig2)
	}

	if sig1 != sig2 {
		t.Fatalf("同输入应得到稳定签名: %q != %q", sig1, sig2)
	}
}

func TestComputeEntitlementCacheSignature_BindsCacheKey(t *testing.T) {
	t.Setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

	sig1, ok := ComputeEntitlementCacheSignature("arkloop:entitlement:orgA:key", "int:123")
	if !ok {
		t.Fatal("sig1 ok=false")
	}
	sig2, ok := ComputeEntitlementCacheSignature("arkloop:entitlement:orgB:key", "int:123")
	if !ok {
		t.Fatal("sig2 ok=false")
	}
	if sig1 == sig2 {
		t.Fatalf("不同 cacheKey 不应得到相同签名: %q", sig1)
	}
}

func TestComputeEntitlementCacheSignature_DifferentValueDifferentSignature(t *testing.T) {
	t.Setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

	sig1, ok := ComputeEntitlementCacheSignature("arkloop:entitlement:org:key", "int:123")
	if !ok {
		t.Fatal("sig1 ok=false")
	}
	sig2, ok := ComputeEntitlementCacheSignature("arkloop:entitlement:org:key", "int:124")
	if !ok {
		t.Fatal("sig2 ok=false")
	}
	if sig1 == sig2 {
		t.Fatalf("不同 rawValue 不应得到相同签名: %q", sig1)
	}
}
