package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestCleanupPersistedToolOutputs_EmptyArgs(t *testing.T) {
	// Should not panic or error when arguments are empty.
	CleanupPersistedToolOutputs("", "run-123")
	CleanupPersistedToolOutputs("/tmp", "")
	CleanupPersistedToolOutputs("", "")
}

func TestCleanupPersistedToolOutputs_NormalDeletion(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("77777777-7777-7777-7777-777777777777").String()
	target := filepath.Join(dir, ".tool-outputs", runID)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	CleanupPersistedToolOutputs(dir, runID)

	_, err := os.Stat(target)
	if err == nil {
		t.Fatalf("expected directory to be removed")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error: %v", err)
	}
}

func TestCleanupPersistedToolOutputs_MissingDirectory(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("88888888-8888-8888-8888-888888888888").String()
	// Directory does not exist; should not panic or error.
	CleanupPersistedToolOutputs(dir, runID)
}

func TestCleanupPersistedToolOutputs_InvalidRunID(t *testing.T) {
	dir := t.TempDir()
	// Create a directory with a malicious-looking name.
	malicious := "../etc"
	target := filepath.Join(dir, ".tool-outputs", malicious)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	CleanupPersistedToolOutputs(dir, malicious)

	// Directory should NOT be removed because run_id is invalid.
	_, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected directory to remain for invalid run_id: %v", err)
	}
}
