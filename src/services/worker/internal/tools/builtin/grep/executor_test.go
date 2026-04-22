package grep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	_, _, err := searchFilesStructured(context.Background(), failingBackend{}, "hello", ".", "", defaultLimit)
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

	matches, _, err := searchFilesStructured(context.Background(), &fileops.LocalBackend{WorkDir: root}, "hello", ".", "", defaultLimit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].file != "notes.txt" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestAutoContextLines(t *testing.T) {
	tests := []struct {
		count    int
		expected int
	}{
		{1, 30},
		{2, 10},
		{3, 10},
		{5, 3},
		{10, 3},
		{11, 0},
		{100, 0},
	}
	for _, tt := range tests {
		got := autoContextLines(tt.count)
		if got != tt.expected {
			t.Errorf("autoContextLines(%d) = %d, want %d", tt.count, got, tt.expected)
		}
	}
}

func TestPaginatedResult(t *testing.T) {
	started := time.Now()
	result := paginatedResult("a\nb\nc", 3, true, 10, 3, 0, started)
	json := result.ResultJSON
	if json["truncated"] != true {
		t.Fatal("expected truncated=true")
	}
	hint, ok := json["pagination_hint"].(string)
	if !ok || hint == "" {
		t.Fatal("expected pagination_hint")
	}
}

func TestSplitNonEmpty(t *testing.T) {
	lines := splitNonEmpty("a\n\nb\n  \nc\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
}
