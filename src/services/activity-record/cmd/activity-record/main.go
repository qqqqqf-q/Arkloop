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
	case "check":
		return runCheck()
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
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	dataDir := flags.String("data-dir", defaultDataDir(), "activity-record data directory")
	syncSources := flags.String("sync-sources", "codex,chrome,screentime,safari,shell", "comma-separated sync source list")
	daemonSources := flags.String("sources", "ax,window,keyboard,mouse,clipboard,process-metrics,battery,wifi,bluetooth,fs-events,security,location", "comma-separated daemon source list")
	syncInterval := flags.Int("sync-interval", 300, "sync interval in seconds")
	idleThreshold := flags.Int("idle-threshold", 300, "idle detection threshold in seconds")
	audioAPIBase := flags.String("audio-api-base", "", "OpenAI-compatible transcription API base URL")
	audioAPIKey := flags.String("audio-api-key", os.Getenv("ARKLOOP_AUDIO_TRANSCRIPTION_API_KEY"), "transcription API key")
	audioModel := flags.String("audio-model", "qwen/qwen3-asr-flash-2026-02-10", "transcription model name")
	audioLanguage := flags.String("audio-language", "", "transcription language hint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchParentProcess(ctx, cancel)
	return syncer.Daemon(ctx, syncer.DaemonOptions{
		DataDir:       *dataDir,
		SyncSources:   splitList(*syncSources),
		DaemonSources: splitList(*daemonSources),
		SyncInterval:  time.Duration(*syncInterval) * time.Second,
		IdleThreshold: time.Duration(*idleThreshold) * time.Second,
		AudioAPIBase:  *audioAPIBase,
		AudioAPIKey:   *audioAPIKey,
		AudioModel:    *audioModel,
		AudioLanguage: *audioLanguage,
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
  activity-record daemon [--data-dir DIR] [--sync-sources codex,chrome,screentime,safari,shell] [--sources ax,window,keyboard,mouse,clipboard,process-metrics,battery,wifi,bluetooth,fs-events,security,location] [--sync-interval 300] [--idle-threshold 300]
  activity-record check`)
}

func runCheck() error {
	result := map[string]any{
		"ax_permission":  ax.CheckAXPermission(),
		"mic_permission": audio.CheckMicPermission(),
	}
	return json.NewEncoder(os.Stdout).Encode(result)
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
