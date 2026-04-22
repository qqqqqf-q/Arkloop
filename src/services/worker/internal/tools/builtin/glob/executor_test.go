package glob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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

func TestGlobFilesDoesNotFallbackOutsideBackend(t *testing.T) {
	_, _, err := globFiles(context.Background(), failingBackend{}, "*.go", ".")
	if err == nil {
		t.Fatal("expected ripgrep error to be returned for non-local backend")
	}
}

func TestGlobFilesLocalFallbackStillWorks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", "")
	backend := &fileops.LocalBackend{WorkDir: root}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "one.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "two.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	matches, _, err := globFiles(context.Background(), backend, "*.txt", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].Path != "nested/one.txt" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}
