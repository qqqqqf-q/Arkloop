package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	daemonSources := flags.String("sources", "window,clipboard,mouse", "comma-separated daemon source list")
	syncInterval := flags.Int("sync-interval", 300, "sync interval in seconds")
	idleThreshold := flags.Int("idle-threshold", 300, "idle detection threshold in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return syncer.Daemon(context.Background(), syncer.DaemonOptions{
		DataDir:       *dataDir,
		SyncSources:   splitList(*syncSources),
		DaemonSources: splitList(*daemonSources),
		SyncInterval:  time.Duration(*syncInterval) * time.Second,
		IdleThreshold: time.Duration(*idleThreshold) * time.Second,
	})
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
  activity-record daemon [--data-dir DIR] [--sync-sources codex,chrome,screentime,safari,shell] [--sources window,clipboard,mouse] [--sync-interval 300] [--idle-threshold 300]`)
}
