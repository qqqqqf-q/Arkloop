package pluginbinary

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/pluginmanifest"
)

func TestDetectRuntimeVersionInsufficient(t *testing.T) {
	root := t.TempDir()
	binaryPath := filepath.Join(root, "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write binary marker: %v", err)
	}
	result := DetectRuntime(context.Background(), pluginmanifest.RuntimeConfig{
		ID: "tool",
		Detect: []pluginmanifest.RuntimeDetectConfig{{
			Path:           "bin/tool",
			VersionCommand: []string{os.Args[0], "-test.run=TestVersionCommandHelper", "--", "tool 1.2.0"},
			VersionMin:     "2.0.0",
		}},
	}, DetectOptions{InstallRoot: root})
	if result.Status != StatusOutdated {
		t.Fatalf("expected outdated, got %#v", result)
	}
	if result.Version != "1.2.0" {
		t.Fatalf("unexpected version: %q", result.Version)
	}
}

func TestDetectRuntimeDerivesHelperAppPath(t *testing.T) {
	root := t.TempDir()
	binaryPath := filepath.Join(root, "runtime", "CuaDriver.app", "Contents", "MacOS", "cua-driver")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write binary marker: %v", err)
	}
	result := DetectRuntime(context.Background(), pluginmanifest.RuntimeConfig{
		ID:     "cua-driver",
		Detect: []pluginmanifest.RuntimeDetectConfig{{Path: "runtime/CuaDriver.app/Contents/MacOS/cua-driver"}},
	}, DetectOptions{InstallRoot: root})
	if result.Status != StatusInstalled {
		t.Fatalf("expected installed, got %#v", result)
	}
	if result.HelperAppPath != filepath.Join(root, "runtime", "CuaDriver.app") {
		t.Fatalf("unexpected helper app path: %q", result.HelperAppPath)
	}
}

func TestVersionCommandHelper(t *testing.T) {
	args := os.Args
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			fmt.Println(args[i+1])
			os.Exit(0)
		}
	}
}
