package app

import (
	"strings"
	"testing"
)

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv(workerConcurrencyEnv, "")
	t.Setenv(workerPollSecondsEnv, "")
	t.Setenv(workerLeaseSecondsEnv, "")
	t.Setenv(workerHeartbeatSecondsEnv, "")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv returned error: %v", err)
	}

	want := DefaultConfig()
	if cfg != want {
		t.Fatalf("config mismatch: got %+v want %+v", cfg, want)
	}
}

func TestLoadConfigFromEnv_ParsesOverrides(t *testing.T) {
	t.Setenv(workerConcurrencyEnv, "8")
	t.Setenv(workerPollSecondsEnv, "0.5")
	t.Setenv(workerLeaseSecondsEnv, "45")
	t.Setenv(workerHeartbeatSecondsEnv, "9")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv returned error: %v", err)
	}

	if cfg.Concurrency != 8 {
		t.Fatalf("unexpected concurrency: %d", cfg.Concurrency)
	}
	if cfg.PollSeconds != 0.5 {
		t.Fatalf("unexpected poll seconds: %v", cfg.PollSeconds)
	}
	if cfg.LeaseSeconds != 45 {
		t.Fatalf("unexpected lease seconds: %d", cfg.LeaseSeconds)
	}
	if cfg.HeartbeatSeconds != 9 {
		t.Fatalf("unexpected heartbeat seconds: %v", cfg.HeartbeatSeconds)
	}
}

func TestLoadConfigFromEnv_RejectsInvalidValue(t *testing.T) {
	t.Setenv(workerConcurrencyEnv, "0")

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if got, want := err.Error(), workerConcurrencyEnv; !strings.Contains(got, want) {
		t.Fatalf("error mismatch: got %q, want to contain %q", got, want)
	}
}
