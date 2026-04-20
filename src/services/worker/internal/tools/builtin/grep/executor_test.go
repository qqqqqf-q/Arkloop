package grep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

type failingBackend struct{}

func (f failingBackend) ReadFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("unused")
}
func (f failingBackend) WriteFile(context.Context, string, []byte) error { return errors.New("unused") }
func (f failingBackend) Stat(context.Context, string) (fileops.FileInfo, error) {
	return fileops.FileInfo{}, errors.New("unused")
}
func (f failingBackend) Exec(context.Context, string) (string, string, int, error) {
	return "", "", 2, errors.New("rg failed")
}
func (f failingBackend) NormalizePath(path string) string { return path }

func TestSearchFilesStructuredDoesNotFallbackOutsideBackend(t *testing.T) {
	_, _, err := searchFilesStructured(context.Background(), failingBackend{}, "hello", ".", "")
	if err == nil {
		t.Fatal("expected ripgrep error to be returned for non-local backend")
	}
}

func TestSearchFilesContextDoesNotFallbackOutsideBackend(t *testing.T) {
	_, _, _, err := searchFiles(context.Background(), failingBackend{}, "hello", ".", "", 2, "thread-1")
	if err == nil {
		t.Fatal("expected ripgrep error to be returned for non-local backend")
	}
}

func TestSearchFilesLocalFallbackStillWorks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", "")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	matches, _, _, err := searchFiles(context.Background(), &fileops.LocalBackend{WorkDir: root}, "hello", ".", "", 0, "thread-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].file != "notes.txt" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestSearchFilesFindsScopedToolOutputs(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(dataDir).Open(context.Background(), "tool-output-test")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	backend := &fileops.LocalBackend{WorkDir: workDir, ToolOutputScopeID: "thread-1", ToolOutputStore: store}
	if err := store.PutObject(context.Background(), "tool-outputs/thread-1/run-1/notes.txt", []byte("hello\nworld\n"), objectstore.PutOptions{}); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	matches, _, _, err := searchFiles(context.Background(), backend, "hello", filepath.Join(".tool-outputs", "thread-1"), "", 0, "thread-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].file != filepath.ToSlash(filepath.Join(".tool-outputs", "thread-1", "run-1", "notes.txt")) {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}
