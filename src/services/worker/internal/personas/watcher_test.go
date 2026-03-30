package personas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWatchedPersonaFilesIncludesSummarizePromptFiles(t *testing.T) {
	files := watchedSummarizePromptFiles(map[string]any{
		"title_summarize": map[string]any{"prompt_file": "title_summarize.md"},
		"result_summarize": map[string]any{"prompt_file": "result_summarize.md"},
	})
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0] != "title_summarize.md" {
		t.Fatalf("unexpected first file: %q", files[0])
	}
	if files[1] != "result_summarize.md" {
		t.Fatalf("unexpected second file: %q", files[1])
	}
}

func TestWatchedPersonaFilesReadsSummarizePromptFilesWithoutSoulFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "summarizer")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	yamlContent := "id: summarizer\nversion: \"1\"\ntitle: Summarizer\ntitle_summarize:\n  prompt_file: title_summarize.md\nresult_summarize:\n  prompt_file: result_summarize.md\n"
	if err := os.WriteFile(filepath.Join(dir, "persona.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile persona.yaml failed: %v", err)
	}
	files := watchedPersonaFiles(root, "summarizer")
	found := map[string]bool{}
	for _, file := range files {
		found[file] = true
	}
	if !found["title_summarize.md"] {
		t.Fatal("expected title_summarize.md to be watched")
	}
	if !found["result_summarize.md"] {
		t.Fatal("expected result_summarize.md to be watched")
	}
}
