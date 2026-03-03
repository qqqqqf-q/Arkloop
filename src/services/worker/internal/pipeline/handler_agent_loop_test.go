package pipeline

import (
	"math"
	"testing"

	"arkloop/services/shared/creditpolicy"
)

func TestCalcPlatformCost_AnthropicCacheFamily(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:         3000,
		totalOutputTokens:        1000,
		totalCacheCreationTokens: 2000,
		totalCacheReadTokens:     5000,
		costPer1kInput:           f64ptr(1.0),
		costPer1kOutput:          f64ptr(2.0),
		costPer1kCacheWrite:      f64ptr(1.25),
		costPer1kCacheRead:       f64ptr(0.1),
	}

	cost := w.calcPlatformCost()
	// output: 1k*2 + input:3k*1 + cache_write:2k*1.25 + cache_read:5k*0.1 = 8.0
	if math.Abs(cost-8.0) > 1e-9 {
		t.Fatalf("expected cost=8.0, got %.10f", cost)
	}
}

func TestCalcPlatformCost_OpenAICacheFamily(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:    10000,
		totalOutputTokens:   2000,
		totalCachedTokens:   4000,
		costPer1kInput:      f64ptr(1.0),
		costPer1kOutput:     f64ptr(2.0),
		costPer1kCacheRead:  nil,
		costPer1kCacheWrite: nil,
	}

	cost := w.calcPlatformCost()
	// output: 2k*2 + uncached:6k*1 + cached:4k*0.5 = 12.0
	if math.Abs(cost-12.0) > 1e-9 {
		t.Fatalf("expected cost=12.0, got %.10f", cost)
	}
}

func TestCalcCreditDeduction_AnthropicCacheFamilyFallback(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:         3000,
		totalOutputTokens:        1000,
		totalCacheCreationTokens: 2000,
		totalCacheReadTokens:     5000,
		totalCostUSD:             0,
		multiplier:               1.0,
		policy: creditpolicy.CreditDeductionPolicy{
			Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
		},
	}

	r := w.calcCreditDeduction()
	// effective = 3000 + 2000*1.25 + 5000*0.1 + 1000 = 7000 => ceil(7.0) = 7
	if r.Credits != 7 {
		t.Fatalf("expected credits=7, got %d", r.Credits)
	}
	if r.Metadata["method"] != "token_fallback" {
		t.Fatalf("expected method=token_fallback, got %v", r.Metadata["method"])
	}
}

func TestCalcCreditDeduction_OpenAICacheFamilyFallback(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:  10000,
		totalOutputTokens: 2000,
		totalCachedTokens: 4000,
		totalCostUSD:      0,
		multiplier:        1.0,
		policy: creditpolicy.CreditDeductionPolicy{
			Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
		},
	}

	r := w.calcCreditDeduction()
	// effective = (10000-4000) + 4000*0.5 + 2000 = 10000 => 10 credits
	if r.Credits != 10 {
		t.Fatalf("expected credits=10, got %d", r.Credits)
	}
}

func TestCalcCreditDeduction_MixedCacheFamilyKeepsLegacy(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:         10000,
		totalOutputTokens:        1000,
		totalCacheCreationTokens: 2000,
		totalCacheReadTokens:     2000,
		totalCachedTokens:        3000,
		totalCostUSD:             0,
		multiplier:               1.0,
		policy: creditpolicy.CreditDeductionPolicy{
			Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
		},
	}

	r := w.calcCreditDeduction()
	// legacy: nonCached=10000-2000-3000=5000
	// effective=5000 + 2000*1.25 + 2000*0.1 + 3000*0.5 + 1000 = 10200 => ceil(10.2)=11
	if r.Credits != 11 {
		t.Fatalf("expected legacy credits=11, got %d", r.Credits)
	}
}

func f64ptr(v float64) *float64 {
	return &v
}
