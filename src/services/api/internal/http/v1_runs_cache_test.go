//go:build !desktop

package http

import (
	"math"
	"testing"
)

func TestCalcCacheHitRate_Anthropic(t *testing.T) {
	rate := calcCacheHitRate(i64Ptr(3000), i64Ptr(5000), i64Ptr(2000), nil)
	if rate == nil {
		t.Fatal("expected cache hit rate, got nil")
	}
	if math.Abs(*rate-0.5) > 1e-9 {
		t.Fatalf("expected 0.5, got %.10f", *rate)
	}
}

func TestCalcCacheHitRate_AnthropicCreationOnly(t *testing.T) {
	rate := calcCacheHitRate(i64Ptr(3000), i64Ptr(0), i64Ptr(2000), nil)
	if rate == nil {
		t.Fatal("expected cache hit rate, got nil")
	}
	if *rate != 0 {
		t.Fatalf("expected 0, got %.10f", *rate)
	}
}

func TestCalcCacheHitRate_OpenAI(t *testing.T) {
	rate := calcCacheHitRate(i64Ptr(10000), nil, nil, i64Ptr(4000))
	if rate == nil {
		t.Fatal("expected cache hit rate, got nil")
	}
	if math.Abs(*rate-0.4) > 1e-9 {
		t.Fatalf("expected 0.4, got %.10f", *rate)
	}
}

func TestCalcCacheHitRate_MixedProviderCache(t *testing.T) {
	rate := calcCacheHitRate(i64Ptr(10000), i64Ptr(2000), i64Ptr(1000), i64Ptr(4000))
	if rate != nil {
		t.Fatalf("expected nil for mixed provider cache fields, got %.10f", *rate)
	}
}

func i64Ptr(v int64) *int64 {
	return &v
}
