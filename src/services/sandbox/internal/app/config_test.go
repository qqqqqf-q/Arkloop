package app

import (
	"os"
	"testing"
)

func TestDefaultConfigSessionStateTTLDays(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SessionStateTTLDays != 7 {
		t.Fatalf("unexpected default ttl: %d", cfg.SessionStateTTLDays)
	}
}

func TestLoadConfigFromEnvSessionStateTTLDays(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS", "0")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.SessionStateTTLDays != 0 {
		t.Fatalf("unexpected ttl: %d", cfg.SessionStateTTLDays)
	}
}

func TestLoadConfigFromEnvSessionStateTTLDaysRejectNegative(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS", "-1")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected ttl validation error")
	}
}

func TestDefaultConfigDockerAllowEgress(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DockerAllowEgress {
		t.Fatal("docker egress should default to disabled")
	}
}

func TestLoadConfigFromEnvDockerAllowEgress(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_DOCKER_ALLOW_EGRESS", "true")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if !cfg.DockerAllowEgress {
		t.Fatal("expected docker egress to be enabled")
	}
}

func TestLoadConfigFromEnvDockerAllowEgressRejectInvalid(t *testing.T) {
	t.Setenv("ARKLOOP_SANDBOX_DOCKER_ALLOW_EGRESS", "maybe")
	t.Setenv("ARKLOOP_SANDBOX_ADDR", "127.0.0.1:8002")
	unsetSandboxConfigRegistryEnv(t)

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected docker egress validation error")
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
