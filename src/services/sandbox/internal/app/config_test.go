package app

import (
	"os"
	"testing"
)

func TestDefaultConfigRestoreTTLDays(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RestoreTTLDays != 7 {
		t.Fatalf("unexpected default ttl: %d", cfg.RestoreTTLDays)
	}
}

func TestLoadConfigFromEnvRestoreTTLDays(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS", "0")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.RestoreTTLDays != 0 {
		t.Fatalf("unexpected ttl: %d", cfg.RestoreTTLDays)
	}
}

func TestLoadConfigFromEnvRestoreTTLDaysRejectNegative(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS", "-1")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected ttl validation error")
	}
}

func TestLoadConfigFromEnvRestoreTTLDaysPrefersNewKey(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_RESTORE_TTL_DAYS", "3")
	t.Setenv("ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS", "9")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.RestoreTTLDays != 3 {
		t.Fatalf("unexpected ttl: %d", cfg.RestoreTTLDays)
	}
}

func TestLoadConfigFromEnvFlushSettings(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	t.Setenv("ARKLOOP_SANDBOX_FLUSH_DEBOUNCE_MS", "1500")
	t.Setenv("ARKLOOP_SANDBOX_FLUSH_MAX_DIRTY_AGE_MS", "9000")
	t.Setenv("ARKLOOP_SANDBOX_FLUSH_FORCE_BYTES_THRESHOLD", "1024")
	t.Setenv("ARKLOOP_SANDBOX_FLUSH_FORCE_COUNT_THRESHOLD", "32")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.FlushDebounceMS != 1500 || cfg.FlushMaxDirtyAgeMS != 9000 || cfg.FlushForceBytesThreshold != 1024 || cfg.FlushForceCountThreshold != 32 {
		t.Fatalf("unexpected flush config: %#v", cfg)
	}
}

func TestDefaultConfigAllowEgress(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AllowEgress {
		t.Fatal("allow_egress should default to enabled")
	}
}

func TestLoadConfigFromEnvAllowEgress(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_ALLOW_EGRESS", "false")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.AllowEgress {
		t.Fatal("expected allow_egress to be disabled")
	}
}

func TestLoadConfigFromEnvAllowEgressRejectInvalid(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_ALLOW_EGRESS", "maybe")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected allow_egress validation error")
	}
}

func TestLoadConfigFromEnvFirecrackerDNS(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	t.Setenv("ARKLOOP_SANDBOX_FIRECRACKER_DNS", "1.1.1.1, 8.8.8.8")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.FirecrackerDNS) != 2 {
		t.Fatalf("unexpected dns list: %#v", cfg.FirecrackerDNS)
	}
}

func unsetSandboxConfigRegistryEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ARKLOOP_DATABASE_URL"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s failed: %v", key, err)
		}
	}
}
