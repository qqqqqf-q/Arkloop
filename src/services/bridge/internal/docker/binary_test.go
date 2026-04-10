package docker

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveBinaryUsesOverride(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, dockerTestBinaryName())
	writeExecutableFile(t, bin)

	t.Setenv("ARKLOOP_DOCKER_BIN", bin)
	t.Setenv("PATH", "")

	resolved, err := ResolveBinary()
	if err != nil {
		t.Fatalf("ResolveBinary error: %v", err)
	}
	if resolved != bin {
		t.Fatalf("expected %q, got %q", bin, resolved)
	}
}

func TestResolveBinaryUsesPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, dockerTestBinaryName())
	writeExecutableFile(t, bin)

	t.Setenv("ARKLOOP_DOCKER_BIN", "")
	t.Setenv("PATH", dir)

	resolved, err := ResolveBinary()
	if err != nil {
		t.Fatalf("ResolveBinary error: %v", err)
	}
	if resolved != bin {
		t.Fatalf("expected %q, got %q", bin, resolved)
	}
}

func TestResolveBinaryReturnsErrorForBadOverride(t *testing.T) {
	t.Setenv("ARKLOOP_DOCKER_BIN", filepath.Join(t.TempDir(), "missing-docker"))
	t.Setenv("PATH", "")

	_, err := ResolveBinary()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrDockerUnavailable) {
		t.Fatalf("expected ErrDockerUnavailable, got %v", err)
	}
}

func dockerTestBinaryName() string {
	if runtime.GOOS == "windows" {
		return "docker.exe"
	}
	return "docker"
}

func writeExecutableFile(t *testing.T, path string) {
	t.Helper()
	content := []byte("#!/bin/sh\nexit 0\n")
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		content = []byte("@echo off\r\nexit /b 0\r\n")
		mode = 0o644
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}
