package sandbox

import (
	"context"
	"testing"

	"arkloop/services/shared/creditpolicy"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

func TestCalcCredits_Normal(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
	}

	// 10 秒执行: ceil(1 + 10*0.5) = ceil(6.0) = 6
	got := CalcCredits(cfg, 10_000, policy)
	if got != 6 {
		t.Errorf("expected 6, got %d", got)
	}
}

func TestCalcCredits_ZeroDuration(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
	}

	// 0ms: ceil(1 + 0) = 1
	got := CalcCredits(cfg, 0, policy)
	if got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestCalcCredits_NegativeDuration(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
	}

	got := CalcCredits(cfg, -100, policy)
	if got != 1 {
		t.Errorf("expected 1 for negative duration, got %d", got)
	}
}

func TestCalcCredits_SubSecond(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
	}

	// 500ms: ceil(1 + 0.5*0.5) = ceil(1.25) = 2
	got := CalcCredits(cfg, 500, policy)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestCalcCredits_PolicyMultiplier(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	// catch-all multiplier = 2.0
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 2.0}},
	}

	// 10s: ceil((1 + 5.0) * 2.0) = ceil(12.0) = 12
	got := CalcCredits(cfg, 10_000, policy)
	if got != 12 {
		t.Errorf("expected 12, got %d", got)
	}
}

func TestCalcCredits_DefaultPolicySkipsTokenFreeZone(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	// DefaultPolicy: <2000 tokens -> multiplier 0, catch-all -> 1.0
	// sandbox 使用 MaxInt64 跳过 token 阈值，应命中 catch-all (1.0)
	got := CalcCredits(cfg, 5000, creditpolicy.DefaultPolicy)
	// ceil(1 + 5*0.5) = ceil(3.5) = 4
	if got != 4 {
		t.Errorf("expected 4, got %d", got)
	}
}

func TestCalcCredits_ZeroMultiplier(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 0}},
	}

	got := CalcCredits(cfg, 10_000, policy)
	if got != 0 {
		t.Errorf("expected 0 for zero multiplier, got %d", got)
	}
}

func TestCalcBaseOnlyCredits(t *testing.T) {
	cfg := BillingConfig{BaseFee: 2, RatePerSecond: 1.0}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
	}

	got := CalcBaseOnlyCredits(cfg, policy)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestCalcBaseOnlyCredits_WithMultiplier(t *testing.T) {
	cfg := BillingConfig{BaseFee: 1, RatePerSecond: 1.0}
	policy := creditpolicy.CreditDeductionPolicy{
		Tiers: []creditpolicy.CreditTier{{Multiplier: 1.5}},
	}

	// ceil(1 * 1.5) = 2
	got := CalcBaseOnlyCredits(cfg, policy)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

// mockExecutor 用于测试 BillingExecutor 的装饰逻辑
type mockExecutor struct {
	result tools.ExecutionResult
}

func (m *mockExecutor) Execute(_ context.Context, _ string, _ map[string]any, _ tools.ExecutionContext, _ string) tools.ExecutionResult {
	return m.result
}

func TestBillingExecutor_NoOrgID(t *testing.T) {
	inner := &mockExecutor{result: tools.ExecutionResult{
		ResultJSON: map[string]any{"duration_ms": int64(5000)},
	}}
	billing := NewBillingExecutor(inner, nil, nil, BillingConfig{BaseFee: 1, RatePerSecond: 0.5})

	// OrgID 为 nil 时不扣费，直接返回原始结果
	result := billing.Execute(context.Background(), "code_execute", nil, tools.ExecutionContext{
		RunID: uuid.New(),
	}, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["duration_ms"] != int64(5000) {
		t.Errorf("result should be passed through, got %v", result.ResultJSON)
	}
}

func TestBillingExecutor_PassesThroughResult(t *testing.T) {
	inner := &mockExecutor{result: tools.ExecutionResult{
		ResultJSON: map[string]any{
			"stdout":      "hello\n",
			"duration_ms": int64(1000),
			"exit_code":   0,
		},
	}}
	// pool 为 nil 时 DeductStandalone 不会被调用（credits <= 0 不扣减不成立，但 pool 为 nil 会报错并 warn）
	// 这里主要验证结果透传
	billing := NewBillingExecutor(inner, nil, nil, BillingConfig{BaseFee: 1, RatePerSecond: 0.5})

	orgID := uuid.New()
	result := billing.Execute(context.Background(), "code_execute", nil, tools.ExecutionContext{
		RunID: uuid.New(),
		OrgID: &orgID,
	}, "")

	if result.ResultJSON["stdout"] != "hello\n" {
		t.Errorf("result should be passed through unchanged")
	}
}

func TestExtractDurationMs(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected int64
	}{
		{"float64", map[string]any{"duration_ms": float64(42)}, 42},
		{"int64", map[string]any{"duration_ms": int64(100)}, 100},
		{"int", map[string]any{"duration_ms": 200}, 200},
		{"nil map", nil, 0},
		{"missing key", map[string]any{}, 0},
		{"string value", map[string]any{"duration_ms": "42"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDurationMs(tt.input)
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}
