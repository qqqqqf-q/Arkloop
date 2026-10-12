package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDaemonConfigFromArgs(t *testing.T) {
	t.Setenv("ARKLOOP_AUDIO_TRANSCRIPTION_API_KEY", "secret-from-env")
	config, err := daemonConfigFromArgs([]string{
		"--data-dir", "/tmp/activity",
		"--sources", "ax,audio",
		"--sync-sources", "codex,chrome",
		"--sync-interval", "45",
		"--idle-threshold", "90",
		"--audio-api-base", "https://example.test/v1",
		"--audio-model", "test-model",
		"--audio-devices", "Mic A,Mic B",
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.AudioAPIKey != "secret-from-env" {
		t.Fatalf("audio key = %q", config.AudioAPIKey)
	}
	if !reflect.DeepEqual(config.DaemonSources, []string{"ax", "audio"}) {
		t.Fatalf("daemon sources = %#v", config.DaemonSources)
	}
	if !reflect.DeepEqual(config.AudioDevices, []string{"Mic A", "Mic B"}) {
		t.Fatalf("audio devices = %#v", config.AudioDevices)
	}
	if config.SyncIntervalSeconds != 45 || config.IdleThresholdSeconds != 90 {
		t.Fatalf("intervals = %d/%d", config.SyncIntervalSeconds, config.IdleThresholdSeconds)
	}
}

func TestDaemonConfigFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	payload := []byte(`{
  "version": 1,
  "data_dir": "/tmp/activity",
  "sync_sources": ["codex"],
  "daemon_sources": ["window"],
  "sync_interval_seconds": 30,
  "idle_threshold_seconds": 60,
  "audio_api_key": "private"
}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := daemonConfigFromArgs([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if config.DataDir != "/tmp/activity" || config.AudioAPIKey != "private" {
		t.Fatalf("config = %#v", config)
	}
}

func TestDaemonConfigRejectsMixedConfigAndFlags(t *testing.T) {
	if _, err := daemonConfigFromArgs([]string{"--config", "/tmp/daemon.json", "--sync-interval", "5"}); err == nil {
		t.Fatal("expected mixed --config and flags to fail")
	}
}
