package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/sources/audio"
	"arkloop/services/activity-record/internal/sources/ax"
	"arkloop/services/activity-record/internal/syncer"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("activity-record: %v", err)
	}
}

func run() error {
	command := "sync"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "sync":
		return runSync(args)
	case "daemon":
		return runDaemon(args)
	case "service":
		return runService(args)
	case "check":
		return runCheck()
	case "devices":
		return runDevices()
	case "walk":
		return runWalk()
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runSync(args []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	dataDir := flags.String("data-dir", defaultDataDir(), "activity-record data directory")
	sourceList := flags.String("sources", "codex,chrome", "comma-separated source list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return syncer.Sync(context.Background(), syncer.Options{
		DataDir: *dataDir,
		Sources: splitList(*sourceList),
	})
}

func runDaemon(args []string) error {
	config, err := daemonConfigFromArgs(args)
	if err != nil {
		return err
	}
	return runDaemonConfig(config)
}

type daemonConfig struct {
	Version              int      `json:"version"`
	DataDir              string   `json:"data_dir"`
	SyncSources          []string `json:"sync_sources"`
	DaemonSources        []string `json:"daemon_sources"`
	SyncIntervalSeconds  int      `json:"sync_interval_seconds"`
	IdleThresholdSeconds int      `json:"idle_threshold_seconds"`
	AudioAPIBase         string   `json:"audio_api_base,omitempty"`
	AudioAPIKey          string   `json:"audio_api_key,omitempty"`
	AudioModel           string   `json:"audio_model,omitempty"`
	AudioLanguage        string   `json:"audio_language,omitempty"`
	AudioDevices         []string `json:"audio_devices,omitempty"`
	ExecutableSHA256     string   `json:"executable_sha256,omitempty"`
}

func daemonConfigFromArgs(args []string) (daemonConfig, error) {
	if len(args) == 2 && args[0] == "--config" {
		return readDaemonConfig(args[1])
	}
	for _, arg := range args {
		if arg == "--config" || strings.HasPrefix(arg, "--config=") {
			return daemonConfig{}, fmt.Errorf("--config cannot be combined with daemon flags")
		}
	}
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	dataDir := flags.String("data-dir", defaultDataDir(), "activity-record data directory")
	syncSources := flags.String("sync-sources", "codex,chrome,screentime,safari,shell", "comma-separated sync source list")
	daemonSources := flags.String("sources", "ax,window,clipboard,mouse", "comma-separated daemon source list")
	syncInterval := flags.Int("sync-interval", 300, "sync interval in seconds")
	idleThreshold := flags.Int("idle-threshold", 300, "idle detection threshold in seconds")
	audioAPIBase := flags.String("audio-api-base", "", "OpenAI-compatible transcription API base URL")
	audioAPIKey := flags.String("audio-api-key", os.Getenv("ARKLOOP_AUDIO_TRANSCRIPTION_API_KEY"), "transcription API key")
	audioModel := flags.String("audio-model", "qwen/qwen3-asr-flash-2026-02-10", "transcription model name")
	audioLanguage := flags.String("audio-language", "", "transcription language hint")
	audioDevices := flags.String("audio-devices", "default", `capture devices: "default", "all", or comma-separated device names (see "devices" command)`)
	if err := flags.Parse(args); err != nil {
		return daemonConfig{}, err
	}
	config := daemonConfig{
		Version:              1,
		DataDir:              *dataDir,
		SyncSources:          splitList(*syncSources),
		DaemonSources:        splitList(*daemonSources),
		SyncIntervalSeconds:  *syncInterval,
		IdleThresholdSeconds: *idleThreshold,
		AudioAPIBase:         *audioAPIBase,
		AudioAPIKey:          *audioAPIKey,
		AudioModel:           *audioModel,
		AudioLanguage:        *audioLanguage,
		AudioDevices:         splitList(*audioDevices),
	}
	return validateDaemonConfig(config)
}

func readDaemonConfig(path string) (daemonConfig, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return daemonConfig{}, fmt.Errorf("read daemon config: %w", err)
	}
	var config daemonConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return daemonConfig{}, fmt.Errorf("decode daemon config: %w", err)
	}
	return validateDaemonConfig(config)
}

func validateDaemonConfig(config daemonConfig) (daemonConfig, error) {
	if config.Version != 1 {
		return daemonConfig{}, fmt.Errorf("unsupported daemon config version %d", config.Version)
	}
	config.DataDir = strings.TrimSpace(config.DataDir)
	if config.DataDir == "" {
		return daemonConfig{}, fmt.Errorf("data directory is required")
	}
	if config.SyncIntervalSeconds <= 0 {
		config.SyncIntervalSeconds = 300
	}
	if config.IdleThresholdSeconds <= 0 {
		config.IdleThresholdSeconds = 300
	}
	return config, nil
}

func runDaemonConfig(config daemonConfig) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchParentProcess(ctx, cancel)
	return syncer.Daemon(ctx, syncer.DaemonOptions{
		DataDir:       config.DataDir,
		SyncSources:   config.SyncSources,
		DaemonSources: config.DaemonSources,
		SyncInterval:  time.Duration(config.SyncIntervalSeconds) * time.Second,
		IdleThreshold: time.Duration(config.IdleThresholdSeconds) * time.Second,
		AudioAPIBase:  config.AudioAPIBase,
		AudioAPIKey:   config.AudioAPIKey,
		AudioModel:    config.AudioModel,
		AudioLanguage: config.AudioLanguage,
		AudioDevices:  config.AudioDevices,
	})
}

// watchParentProcess ties this daemon's lifetime to the spawning process. When
// the parent (desktop host) exits, the daemon shuts itself down so it never
// outlives its host. No-op when launched without a parent pid (manual runs).
func watchParentProcess(ctx context.Context, cancel context.CancelFunc) {
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_ACTIVITY_RECORD_PARENT_PID"))
	if raw == "" {
		return
	}
	ppid, err := strconv.Atoi(raw)
	if err != nil || ppid <= 1 {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !processAlive(ppid) {
					log.Printf("parent process %d exited, shutting down", ppid)
					cancel()
					return
				}
			}
		}
	}()
}

func defaultDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("ARKLOOP_ACTIVITY_RECORD_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".activity-record"
	}
	return filepath.Join(home, ".Arkloop", "activity-record")
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func printUsage() {
	fmt.Fprintln(os.Stdout, `Usage:
  activity-record sync   [--data-dir DIR] [--sources codex,chrome]
  activity-record daemon [--data-dir DIR] [--sync-sources codex,chrome,screentime,safari,shell] [--sources ax,window,clipboard,mouse] [--sync-interval 300] [--idle-threshold 300]
	activity-record service install [daemon options]
	activity-record service update  [daemon options]
	activity-record service status
	activity-record service uninstall
  activity-record check`)
}

func runCheck() error {
	result := map[string]any{
		"ax_permission":  ax.CheckAXPermission(),
		"mic_permission": audio.CheckMicPermission(),
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func runDevices() error {
	devices, err := audio.ListCaptureDevices()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(devices)
}

func runWalk() error {
	r := ax.TestWalk(30, 5000, 500.0)
	out := map[string]any{
		"app":          r.AppName,
		"window_title": r.WindowTitle,
		"pid":          r.PID,
		"url":          r.BrowserURL,
		"text_len":     len(r.TextContent),
		"elements":     r.ElementCount,
		"walk_ms":      r.WalkDurationMs,
		"truncated":    r.Truncated,
		"trunc_reason": r.TruncationReason,
	}
	if r.Error != nil {
		out["error"] = r.Error.Error()
	}
	if len(r.TextContent) > 0 {
		text := r.TextContent
		if len([]rune(text)) > 500 {
			text = string([]rune(text)[:500])
		}
		out["text_preview"] = text
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
