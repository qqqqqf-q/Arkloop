package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateTail_Short(t *testing.T) {
	r := TruncateTail("hello world", 100)
	if r.Truncated {
		t.Fatal("should not truncate short input")
	}
	if r.Text != "hello world" {
		t.Fatalf("unexpected text: %s", r.Text)
	}
}

func TestTruncateTail_ExactLimit(t *testing.T) {
	s := strings.Repeat("a", 100)
	r := TruncateTail(s, 100)
	if r.Truncated {
		t.Fatal("should not truncate at exact limit")
	}
}

func TestTruncateTail_Truncates(t *testing.T) {
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = strings.Repeat("x", 50)
	}
	input := strings.Join(lines, "\n")
	r := TruncateTail(input, 500)
	if !r.Truncated {
		t.Fatal("should truncate")
	}
	if !strings.HasPrefix(r.Text, "... [truncated") {
		t.Fatalf("missing marker: %s", r.Text[:60])
	}
	if utf8.RuneCountInString(r.Text) > 600 {
		t.Fatalf("truncated output too long: %d runes", utf8.RuneCountInString(r.Text))
	}
}

func TestTruncateTail_UTF8(t *testing.T) {
	// each char is 3 bytes
	s := strings.Repeat("中", 200)
	r := TruncateTail(s, 100)
	if !r.Truncated {
		t.Fatal("should truncate")
	}
	if !utf8.ValidString(r.Text) {
		t.Fatal("result is not valid UTF-8")
	}
}

func TestPersistLargeOutput_UnderLimit(t *testing.T) {
	fp := PersistLargeOutput("test-run-1", "short")
	if fp != "" {
		t.Fatalf("should not persist short output, got: %s", fp)
	}
}

func TestPersistLargeOutput_OverLimit(t *testing.T) {
	runID := "test-persist-run"
	defer CleanupRunDisk(runID)

	output := strings.Repeat("a\n", TruncateMaxChars+1)
	fp := PersistLargeOutput(runID, output)
	if fp == "" {
		t.Fatal("should persist large output")
	}
	if !strings.Contains(fp, runID) {
		t.Fatalf("path should contain run_id: %s", fp)
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if string(data) != output {
		t.Fatal("persisted content mismatch")
	}
}

func TestCleanupRunDisk(t *testing.T) {
	runID := "test-cleanup-run"
	dir := filepath.Join(LargeOutputDir, runID)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "test.log"), []byte("data"), 0o644)

	CleanupRunDisk(runID)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory should be removed")
	}
}
