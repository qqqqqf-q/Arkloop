package plugincontrib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"arkloop/services/shared/pluginmanifest"
)

func TestSeededBuiltinManifestsParse(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "plugins"))
	for _, name := range builtinPluginDirs {
		name := name
		t.Run(name, func(t *testing.T) {
			payload, _, pluginRoot, cleanup, err := loadManifestPayload(context.Background(), InstallRequest{
				ManifestPath: filepath.Join(root, name, "manifest.yaml"),
			})
			if err != nil {
				t.Fatalf("load manifest: %v", err)
			}
			defer cleanup()
			manifest, _, err := decodeManifest(payload)
			if err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			if err := hydrateManifestContext(&manifest, pluginRoot); err != nil {
				t.Fatalf("hydrate context: %v", err)
			}
		})
	}
}

func TestCheckRuntimeDaemonsDoesNotWaitForHealthyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	manifest := Manifest{
		Runtime: []pluginmanifest.RuntimeConfig{{
			ID: "screenpipe",
			Daemon: &pluginmanifest.RuntimeDaemonConfig{
				ID:        "server",
				HealthURL: server.URL,
			},
		}},
	}
	state := map[string]any{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	checkRuntimeDaemons(ctx, manifest, nil, state)
	if elapsed := time.Since(startedAt); elapsed > 150*time.Millisecond {
		t.Fatalf("daemon status check waited too long: %s", elapsed)
	}
	if got := state["screenpipe.server.daemon.status"]; got != "stopped" {
		t.Fatalf("daemon status = %v, want stopped", got)
	}
}

func TestCheckRuntimeDaemonsClearsStalePID(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{
		"plugin_data":                  root,
		"screenpipe.server.daemon.pid": 999999,
	}
	pidPath := filepath.Join(root, "runtime", "screenpipe.server.pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatalf("mkdir pid dir: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(999999)), 0o644); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	manifest := Manifest{
		Runtime: []pluginmanifest.RuntimeConfig{{
			ID: "screenpipe",
			Daemon: &pluginmanifest.RuntimeDaemonConfig{
				ID:        "server",
				HealthURL: "http://127.0.0.1:1/health",
			},
		}},
	}

	checkRuntimeDaemons(context.Background(), manifest, nil, state)
	if got := state["screenpipe.server.daemon.status"]; got != "stopped" {
		t.Fatalf("daemon status = %v, want stopped", got)
	}
	if _, ok := state["screenpipe.server.daemon.pid"]; ok {
		t.Fatalf("stale pid must be removed from runtime state")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file must be removed, err=%v", err)
	}
}

func TestCheckRuntimeDaemonsTreatsLiveProcessWithoutHealthURLAsRunning(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{"plugin_data": root}
	pid := os.Getpid()
	writeDaemonPID(state, "activitywatch.window", pid)
	manifest := Manifest{
		Runtime: []pluginmanifest.RuntimeConfig{{
			ID: "activitywatch",
			Daemons: []pluginmanifest.RuntimeDaemonConfig{{
				ID: "window",
			}},
		}},
	}

	checkRuntimeDaemons(context.Background(), manifest, nil, state)
	if got := state["activitywatch.window.daemon.status"]; got != "running" {
		t.Fatalf("daemon status = %v, want running", got)
	}
}

func TestRenderDaemonLaunchAppliesConditionalArgs(t *testing.T) {
	daemon := pluginmanifest.RuntimeDaemonConfig{
		Command: "screenpipe",
		Args:    []string{"record"},
		ArgsWhen: []pluginmanifest.RuntimeArgsWhen{{
			Setting: "enable_audio",
			Equals:  false,
			Args:    []string{"--disable-audio"},
		}},
	}
	_, args, _, _, err := renderDaemonLaunch(pluginmanifest.RuntimeConfig{ID: "screenpipe"}, daemon, map[string]any{
		"enable_audio": false,
	}, map[string]any{})
	if err != nil {
		t.Fatalf("render daemon launch: %v", err)
	}
	if len(args) != 2 || args[0] != "record" || args[1] != "--disable-audio" {
		t.Fatalf("args = %#v, want conditional disable-audio", args)
	}
}
