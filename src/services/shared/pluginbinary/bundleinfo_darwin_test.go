//go:build darwin

package pluginbinary

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/pluginmanifest"
)

func TestDetectRuntimeDerivesHelperAppInfo(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "runtime", "CuaDriver.app")
	binaryPath := filepath.Join(appPath, "Contents", "MacOS", "cua-driver")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir binary: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write binary marker: %v", err)
	}
	infoPath := filepath.Join(appPath, "Contents", "Info.plist")
	if err := os.WriteFile(infoPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDisplayName</key>
  <string>Cua Driver</string>
  <key>CFBundleIdentifier</key>
  <string>com.trycua.driver</string>
</dict>
</plist>`), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	result := DetectRuntime(context.Background(), pluginmanifestRuntimeConfig(), DetectOptions{InstallRoot: root})
	if result.HelperAppName != "Cua Driver" {
		t.Fatalf("unexpected helper app name: %q", result.HelperAppName)
	}
	if result.HelperAppBundleID != "com.trycua.driver" {
		t.Fatalf("unexpected helper app bundle id: %q", result.HelperAppBundleID)
	}
}

func pluginmanifestRuntimeConfig() pluginmanifest.RuntimeConfig {
	return pluginmanifest.RuntimeConfig{
		ID:     "cua-driver",
		Detect: []pluginmanifest.RuntimeDetectConfig{{Path: "runtime/CuaDriver.app/Contents/MacOS/cua-driver"}},
	}
}
