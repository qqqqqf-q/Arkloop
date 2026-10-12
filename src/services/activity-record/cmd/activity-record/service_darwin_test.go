//go:build darwin

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchAgentPlistContainsOnlyConfigReference(t *testing.T) {
	payload, err := launchAgentPlist("/Applications/Arkloop & Dev/activity-record", "/tmp/private & config.json", "/tmp/daemon.log")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range [][]byte{
		[]byte("com.arkloop.activity-record"),
		[]byte("com.arkloop.desktop"),
		[]byte("/Applications/Arkloop &amp; Dev/activity-record"),
		[]byte("/tmp/private &amp; config.json"),
		[]byte("<key>KeepAlive</key>"),
		[]byte("<key>RunAtLoad</key>"),
	} {
		if !bytes.Contains(payload, expected) {
			t.Fatalf("plist missing %q:\n%s", expected, payload)
		}
	}
	if bytes.Contains(payload, []byte("audio_api_key")) {
		t.Fatalf("plist contains private config: %s", payload)
	}
}

func TestInstallServiceExecutableKeepsStableCopyUntilExplicitUpdate(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "build", "activity-record")
	destination := filepath.Join(dir, "installed", "activity-record")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installServiceExecutable(source, destination, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("version-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installServiceExecutable(source, destination, false); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, destination, "version-one")

	if _, err := installServiceExecutable(source, destination, true); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, destination, "version-two")
}

func assertFileContents(t *testing.T, path, expected string) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != expected {
		t.Fatalf("%s contains %q, want %q", path, payload, expected)
	}
}
