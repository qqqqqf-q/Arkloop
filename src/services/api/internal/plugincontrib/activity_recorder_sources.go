package plugincontrib

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

const activityRecorderPluginID = "arkloop.plugins.activity-recorder"
const aiContextSyncStaleAfter = 10 * time.Minute
const aiContextSyncRetryAfter = 15 * time.Minute

type aiContextSourceConfig struct {
	Key  string `json:"key"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type aiContextConfig struct {
	Sources []aiContextSourceConfig `json:"sources"`
}

func prepareActivityRecorderSources(ctx context.Context, pluginID string, settings, runtimeState map[string]any) {
	if pluginID != activityRecorderPluginID {
		return
	}
	if settingBool(settings, "enable_aicontext") {
		prepareAIContext(runtimeState)
	}
	if settingBool(settings, "enable_screentime") {
		checkScreenTimeAccess(runtimeState)
	}
	_ = ctx
}

func settingBool(settings map[string]any, key string) bool {
	value, ok := settings[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return fmt.Sprint(typed) == "true"
	}
}

func prepareAIContext(runtimeState map[string]any) {
	prefix := "aicontext."
	delete(runtimeState, prefix+"setup_error")
	home, err := os.UserHomeDir()
	if err != nil {
		runtimeState[prefix+"initialized"] = false
		runtimeState[prefix+"setup_error"] = err.Error()
		return
	}
	root := filepath.Join(home, ".aicontext")
	dataDir := filepath.Join(root, "data")
	configPath := filepath.Join(root, "config.json")
	dbPath := filepath.Join(dataDir, "activity.db")
	runtimeState[prefix+"config_path"] = configPath
	runtimeState[prefix+"db_path"] = dbPath

	config, err := readAIContextConfig(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		runtimeState[prefix+"initialized"] = false
		runtimeState[prefix+"setup_error"] = err.Error()
		return
	}
	if len(config.Sources) == 0 {
		config.Sources = discoverAIContextSources(home)
		if len(config.Sources) == 0 {
			runtimeState[prefix+"initialized"] = false
			runtimeState[prefix+"setup_error"] = "no supported source found"
			return
		}
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			runtimeState[prefix+"initialized"] = false
			runtimeState[prefix+"setup_error"] = err.Error()
			return
		}
		if err := writeAIContextConfig(configPath, config); err != nil {
			runtimeState[prefix+"initialized"] = false
			runtimeState[prefix+"setup_error"] = err.Error()
			return
		}
		runtimeState[prefix+"initialized_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	runtimeState[prefix+"initialized"] = true
	runtimeState[prefix+"source_count"] = len(config.Sources)
	if records, err := sqliteCount(dbPath, "SELECT COUNT(*) FROM activity"); err == nil {
		runtimeState[prefix+"db_records"] = records
		if records == 0 {
			startAIContextInitialSync(runtimeState)
			return
		}
		clearAIContextInitialSyncIfStopped(runtimeState)
		if latestUnix, err := sqliteCount(dbPath, "SELECT COALESCE(MAX(unixepoch(timestamp)), 0) FROM activity"); err == nil && latestUnix > 0 {
			latest := time.Unix(int64(latestUnix), 0).UTC()
			runtimeState[prefix+"db_latest_at"] = latest.Format(time.RFC3339)
			stale := time.Since(latest) > aiContextSyncStaleAfter
			runtimeState[prefix+"stale"] = stale
			if stale {
				startAIContextInitialSync(runtimeState)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		runtimeState[prefix+"db_error"] = err.Error()
	} else {
		runtimeState[prefix+"db_records"] = 0
		startAIContextInitialSync(runtimeState)
	}
}

func clearAIContextInitialSyncIfStopped(runtimeState map[string]any) {
	key := "aicontext.initial_sync"
	if pid := daemonPID(runtimeState, key); processRunning(pid) {
		runtimeState[key+".status"] = "running"
		return
	}
	removeDaemonPID(runtimeState, key)
	delete(runtimeState, key+".pid")
	if stringFromPluginMap(runtimeState, key+".status") == "running" {
		runtimeState[key+".status"] = "stopped"
	}
}

func readAIContextConfig(path string) (aiContextConfig, error) {
	var config aiContextConfig
	payload, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		return config, err
	}
	return config, nil
}

func writeAIContextConfig(path string, config aiContextConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}

func discoverAIContextSources(home string) []aiContextSourceConfig {
	candidates := []aiContextSourceConfig{
		{Key: "claude_code", Path: filepath.Join(home, ".claude", "projects"), Mode: "dynamic"},
		{Key: "codex", Path: filepath.Join(home, ".codex", "sessions"), Mode: "dynamic"},
		{Key: "browser_chrome", Path: filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "History"), Mode: "dynamic"},
		{Key: "browser_chrome", Path: filepath.Join(home, "Library", "Application Support", "Google", "Chrome Canary", "Default", "History"), Mode: "dynamic"},
		{Key: "browser_edge", Path: filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "Default", "History"), Mode: "dynamic"},
		{Key: "browser_dia", Path: filepath.Join(home, "Library", "Application Support", "Dia", "User Data", "Default", "History"), Mode: "dynamic"},
		{Key: "browser_safari", Path: filepath.Join(home, "Library", "Safari", "History.db"), Mode: "dynamic"},
	}
	out := make([]aiContextSourceConfig, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate.Path); err != nil {
			continue
		}
		key := candidate.Key + "\x00" + candidate.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

func startAIContextInitialSync(runtimeState map[string]any) {
	key := "aicontext.initial_sync"
	prefix := key + "."
	delete(runtimeState, prefix+"error")
	if pid := daemonPID(runtimeState, key); processRunning(pid) {
		runtimeState[prefix+"status"] = "running"
		return
	}
	removeDaemonPID(runtimeState, key)
	if recentAIContextSync(runtimeState, key) {
		runtimeState[prefix+"status"] = "stale"
		return
	}
	pluginData := stringFromPluginMap(runtimeState, "plugin_data")
	logPath := filepath.Join(pluginData, "runtime", "logs", "aicontext.initial-sync.log")
	if pluginData == "" {
		logPath = filepath.Join(os.TempDir(), "arkloop-aicontext.initial-sync.log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		runtimeState[prefix+"status"] = "error"
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		runtimeState[prefix+"status"] = "error"
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	cmd := exec.CommandContext(detachedContext(context.Background()), "uv", "tool", "run", "--from", "sophonme-aicontext", "aicontext", "sync")
	configureDaemonCommand(cmd)
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		runtimeState[prefix+"status"] = "error"
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	_ = logFile.Close()
	writeDaemonPID(runtimeState, key, cmd.Process.Pid)
	runtimeState[prefix+"pid"] = cmd.Process.Pid
	runtimeState[prefix+"log_path"] = logPath
	runtimeState[prefix+"started_at"] = time.Now().UTC().Format(time.RFC3339)
	runtimeState[prefix+"status"] = "running"
	go func() {
		_ = cmd.Wait()
	}()
}

func recentAIContextSync(runtimeState map[string]any, key string) bool {
	startedAt := stringFromPluginMap(runtimeState, key+".started_at")
	if startedAt == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return false
	}
	return time.Since(parsed) < aiContextSyncRetryAfter
}

func checkScreenTimeAccess(runtimeState map[string]any) {
	prefix := "screentime.permissions."
	runtimeState[prefix+"checked_at"] = time.Now().UTC().Format(time.RFC3339)
	delete(runtimeState, prefix+"error")
	if runtime.GOOS != "darwin" {
		runtimeState[prefix+"full_disk_access"] = true
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		runtimeState[prefix+"full_disk_access"] = false
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	path := filepath.Join(home, "Library", "Application Support", "Knowledge", "knowledgeC.db")
	runtimeState[prefix+"db_path"] = path
	if _, err := os.Stat(path); err != nil {
		runtimeState[prefix+"full_disk_access"] = false
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	_, err = sqliteCount(path, "SELECT COUNT(*) FROM ZOBJECT")
	if err != nil {
		runtimeState[prefix+"full_disk_access"] = false
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	runtimeState[prefix+"full_disk_access"] = true
}

func sqliteCount(path, query string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
