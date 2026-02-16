package app

import (
	"slices"
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
	if cfg.Concurrency != want.Concurrency {
		t.Fatalf("config mismatch: concurrency got %d want %d", cfg.Concurrency, want.Concurrency)
	}
	if cfg.PollSeconds != want.PollSeconds {
		t.Fatalf("config mismatch: poll_seconds got %v want %v", cfg.PollSeconds, want.PollSeconds)
	}
	if cfg.LeaseSeconds != want.LeaseSeconds {
		t.Fatalf("config mismatch: lease_seconds got %d want %d", cfg.LeaseSeconds, want.LeaseSeconds)
	}
	if cfg.HeartbeatSeconds != want.HeartbeatSeconds {
		t.Fatalf("config mismatch: heartbeat_seconds got %v want %v", cfg.HeartbeatSeconds, want.HeartbeatSeconds)
	}
	if !slices.Equal(cfg.QueueJobTypes, want.QueueJobTypes) {
		t.Fatalf("config mismatch: queue_job_types got %#v want %#v", cfg.QueueJobTypes, want.QueueJobTypes)
	}
}

func TestLoadConfigFromEnv_ParsesOverrides(t *testing.T) {
	t.Setenv(workerConcurrencyEnv, "8")
	t.Setenv(workerPollSecondsEnv, "0.5")
	t.Setenv(workerLeaseSecondsEnv, "45")
	t.Setenv(workerHeartbeatSecondsEnv, "9")
	t.Setenv(workerQueueJobTypesEnv, "run.execute")

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
	if !slices.Equal(cfg.QueueJobTypes, []string{"run.execute"}) {
		t.Fatalf("unexpected queue_job_types: %#v", cfg.QueueJobTypes)
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

func TestLoadConfigFromEnv_RejectsUnknownJobType(t *testing.T) {
	t.Setenv(workerQueueJobTypesEnv, "run.execute.go_native")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}
