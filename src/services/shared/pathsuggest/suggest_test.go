package pathsuggest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuggestSimilarPaths_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := SuggestSimilarPaths(filepath.Join(dir, "src", "readme.md"), dir)
	if len(results) == 0 {
		t.Fatal("expected case-insensitive suggestion")
	}
	if filepath.Base(results[0]) != "README.md" {
		t.Errorf("got %s, want README.md", filepath.Base(results[0]))
	}
}

func TestSuggestSimilarPaths_SynonymMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "utils"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "utils", "helper.go"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	results := SuggestSimilarPaths(filepath.Join(dir, "util", "helper.go"), dir)
	if len(results) == 0 {
		t.Fatal("expected synonym suggestion")
	}
}

func TestSuggestSimilarPaths_MissingLayer(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "services", "worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "services", "worker", "main.go"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// target missing "services" layer
	results := SuggestSimilarPaths(filepath.Join(dir, "src", "worker", "main.go"), dir)
	if len(results) == 0 {
		t.Fatal("expected missing-layer suggestion")
	}
}

func TestSuggestSimilarPaths_Empty(t *testing.T) {
	results := SuggestSimilarPaths("", "/tmp")
	if len(results) != 0 {
		t.Error("expected no results for empty path")
	}
}

func TestSuggestSimilarPaths_MaxResults(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"filea.go", "fileb.go", "filec.go", "filed.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	results := SuggestSimilarPaths(filepath.Join(dir, "filex.go"), dir)
	if len(results) > maxResults {
		t.Errorf("got %d results, want <= %d", len(results), maxResults)
	}
}

func TestSuggestSimilarPaths_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	results := SuggestSimilarPaths(filepath.Join(dir, "pkg", "index.js"), dir)
	for _, r := range results {
		if filepath.Base(filepath.Dir(filepath.Dir(r))) == "node_modules" {
			t.Errorf("should skip node_modules, got %s", r)
		}
	}
}

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "abcd", 1},
		{"", "abc", 3},
	}
	for _, tt := range tests {
		got := editDistance(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
