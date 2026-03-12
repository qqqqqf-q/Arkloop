//go:build !desktop

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestNewApplicationInitializesEmptyDynamicConfig(t *testing.T) {
	app, err := NewApplication(DefaultConfig(), NewJSONLogger("gateway", io.Discard))
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	cfg := app.getDynamicConfig()
	if cfg == nil {
		t.Fatal("dynamic config should not be nil")
	}
	if cfg.IPMode != "" {
		t.Fatalf("unexpected ip mode: %q", cfg.IPMode)
	}
	if len(cfg.TrustedCIDRs) != 0 {
		t.Fatalf("unexpected trusted cidrs: %#v", cfg.TrustedCIDRs)
	}
}

func TestNewApplicationLogsWhenJWTSecretMissing(t *testing.T) {
	var buf bytes.Buffer
	_, err := NewApplication(DefaultConfig(), NewJSONLogger("gateway", &buf))
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if bytes.Contains(buf.Bytes(), []byte(`"msg":"jwt secret missing"`)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing jwt secret warning log: %s", buf.String())
}

func TestLoadDynamicConfigOverridesEffectiveValues(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := DefaultConfig()
	cfg.IPMode = IPModeDirect
	cfg.TrustedCIDRs = []string{"10.0.0.0/8"}
	cfg.RiskRejectThreshold = 30
	cfg.RateLimit.Capacity = 10
	cfg.RateLimit.RatePerMinute = 20

	app, err := NewApplication(cfg, NewJSONLogger("gateway", io.Discard))
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	payload, err := json.Marshal(gatewayDynamicConfig{
		IPMode:              string(IPModeTrustedProxy),
		TrustedCIDRs:        []string{"192.168.0.0/16", "172.16.0.0/12"},
		RiskRejectThreshold: intPtr(75),
		RateLimitCapacity:   50,
		RateLimitPerMinute:  80,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := rdb.Set(context.Background(), gatewayConfigRedisKey, payload, 0).Err(); err != nil {
		t.Fatalf("redis set: %v", err)
	}

	app.loadDynamicConfig(context.Background(), rdb)

	if got := app.effectiveIPMode(); got != IPModeTrustedProxy {
		t.Fatalf("effectiveIPMode = %q, want %q", got, IPModeTrustedProxy)
	}
	if got := app.effectiveTrustedCIDRs(); len(got) != 2 || got[0] != "192.168.0.0/16" || got[1] != "172.16.0.0/12" {
		t.Fatalf("effectiveTrustedCIDRs = %#v", got)
	}
	if got := app.effectiveRiskThreshold(); got != 75 {
		t.Fatalf("effectiveRiskThreshold = %d, want 75", got)
	}

	rl := app.effectiveRateLimit()
	if rl.Capacity != 50 {
		t.Fatalf("rate limit capacity = %v, want 50", rl.Capacity)
	}
	if rl.RatePerMinute != 80 {
		t.Fatalf("rate limit per minute = %v, want 80", rl.RatePerMinute)
	}
}

func TestLoadDynamicConfigAllowsZeroRiskThreshold(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := DefaultConfig()
	cfg.RiskRejectThreshold = 30

	app, err := NewApplication(cfg, NewJSONLogger("gateway", io.Discard))
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	payload, err := json.Marshal(gatewayDynamicConfig{RiskRejectThreshold: intPtr(0)})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := rdb.Set(context.Background(), gatewayConfigRedisKey, payload, 0).Err(); err != nil {
		t.Fatalf("redis set: %v", err)
	}

	app.loadDynamicConfig(context.Background(), rdb)

	if got := app.effectiveRiskThreshold(); got != 0 {
		t.Fatalf("effectiveRiskThreshold = %d, want 0", got)
	}
}

func TestLoadDynamicConfigWithoutRiskThresholdFallsBackToConfig(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := DefaultConfig()
	cfg.RiskRejectThreshold = 30

	app, err := NewApplication(cfg, NewJSONLogger("gateway", io.Discard))
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	payload, err := json.Marshal(gatewayDynamicConfig{IPMode: string(IPModeTrustedProxy)})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := rdb.Set(context.Background(), gatewayConfigRedisKey, payload, 0).Err(); err != nil {
		t.Fatalf("redis set: %v", err)
	}

	app.loadDynamicConfig(context.Background(), rdb)

	if got := app.effectiveRiskThreshold(); got != 30 {
		t.Fatalf("effectiveRiskThreshold = %d, want 30", got)
	}
}

func TestDynamicConfigConcurrentAccess(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	app, err := NewApplication(DefaultConfig(), NewJSONLogger("gateway", io.Discard))
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	ctx := context.Background()
	values := []gatewayDynamicConfig{
		{IPMode: string(IPModeDirect), TrustedCIDRs: []string{"10.0.0.0/8"}, RiskRejectThreshold: intPtr(10), RateLimitCapacity: 10, RateLimitPerMinute: 20},
		{IPMode: string(IPModeTrustedProxy), TrustedCIDRs: []string{"192.168.0.0/16"}, RiskRejectThreshold: intPtr(20), RateLimitCapacity: 20, RateLimitPerMinute: 40},
		{IPMode: string(IPModeCloudflare), TrustedCIDRs: []string{"173.245.48.0/20"}, RiskRejectThreshold: intPtr(30), RateLimitCapacity: 30, RateLimitPerMinute: 60},
	}

	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 200; i++ {
			payload, err := json.Marshal(values[i%len(values)])
			if err != nil {
				t.Errorf("json.Marshal: %v", err)
				return
			}
			if err := rdb.Set(ctx, gatewayConfigRedisKey, payload, 0).Err(); err != nil {
				t.Errorf("redis set: %v", err)
				return
			}
			app.loadDynamicConfig(ctx, rdb)
		}
	}()

	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 200; j++ {
				_ = app.effectiveIPMode()
				_ = app.effectiveTrustedCIDRs()
				_ = app.effectiveRiskThreshold()
				_ = app.effectiveRateLimit()
				if app.getDynamicConfig() == nil {
					t.Error("dynamic config should not be nil")
					return
				}
			}
		}()
	}

	writers.Wait()
	readers.Wait()
}

func intPtr(v int) *int {
	return &v
}
