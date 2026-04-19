package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPersistLargeResult_UnderThreshold(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	execCtx := ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
	}
	result := ExecutionResult{
		ResultJSON: map[string]any{"output": "small"},
	}
	raw, _ := json.Marshal(result.ResultJSON)
	out := PersistLargeResult(context.Background(), execCtx, "tc1", raw, result)
	if out.ResultJSON["persisted"] != nil {
		t.Fatalf("expected no persistence for small result")
	}
}

func TestPersistLargeResult_ExactThreshold(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	execCtx := ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
	}

	// Build a payload that is exactly PersistThreshold bytes after marshal.
	var exact int
	for n := 0; n < PersistThreshold; n++ {
		large := map[string]any{"output": strings.Repeat("a", n)}
		raw, _ := json.Marshal(large)
		if len(raw) == PersistThreshold {
			exact = n
			break
		}
	}
	if exact == 0 {
		t.Fatalf("could not find exact payload size for threshold %d", PersistThreshold)
	}

	large := map[string]any{"output": strings.Repeat("a", exact)}
	result := ExecutionResult{ResultJSON: large}
	raw, _ := json.Marshal(result.ResultJSON)
	out := PersistLargeResult(context.Background(), execCtx, "tc6", raw, result)
	if out.ResultJSON["persisted"] != nil {
		t.Fatalf("expected no persistence at exact threshold")
	}

	// PersistThreshold + 1 should trigger persistence.
	large["output"] = strings.Repeat("a", exact+1)
	result = ExecutionResult{ResultJSON: large}
	raw, _ = json.Marshal(result.ResultJSON)
	out = PersistLargeResult(context.Background(), execCtx, "tc6b", raw, result)
	if out.ResultJSON["persisted"] != true {
		t.Fatalf("expected persistence at threshold+1")
	}
}

func TestPersistLargeResult_OverThreshold(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	execCtx := ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
	}
	large := map[string]any{
		"output": string(make([]byte, PersistThreshold+1)),
	}
	result := ExecutionResult{
		ResultJSON: large,
	}
	raw, _ := json.Marshal(result.ResultJSON)
	out := PersistLargeResult(context.Background(), execCtx, "tc2", raw, result)
	if out.ResultJSON["persisted"] != true {
		t.Fatalf("expected persistence for large result")
	}
	filePath, _ := out.ResultJSON["filepath"].(string)
	expectedPath := filepath.Join(".tool-outputs", runID.String(), "tc2.txt")
	if filePath != expectedPath {
		t.Fatalf("unexpected filepath: %s, want %s", filePath, expectedPath)
	}
	ob, _ := out.ResultJSON["original_bytes"].(int)
	if ob <= PersistThreshold {
		t.Fatalf("expected original_bytes > threshold, got %d", ob)
	}

	data, err := os.ReadFile(filepath.Join(dir, filePath))
	if err != nil {
		t.Fatalf("read persisted file failed: %v", err)
	}
	// content should be the raw output string, not JSON
	if len(data) != PersistThreshold+1 {
		t.Fatalf("expected %d bytes of raw output, got %d", PersistThreshold+1, len(data))
	}
}

func TestPersistLargeResult_KeepsMetadata(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	execCtx := ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
	}
	large := map[string]any{
		"output":    string(make([]byte, PersistThreshold+1)),
		"exit_code": 42,
		"cwd":       "/tmp",
	}
	result := ExecutionResult{
		ResultJSON: large,
	}
	raw, _ := json.Marshal(result.ResultJSON)
	out := PersistLargeResult(context.Background(), execCtx, "tc3", raw, result)
	if out.ResultJSON["exit_code"] != 42 {
		t.Fatalf("expected exit_code preserved, got %v", out.ResultJSON["exit_code"])
	}
	if out.ResultJSON["cwd"] != "/tmp" {
		t.Fatalf("expected cwd preserved, got %v", out.ResultJSON["cwd"])
	}
}

func TestPersistLargeResult_InvalidToolCallID(t *testing.T) {
	dir := t.TempDir()
	runID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	execCtx := ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
	}
	large := map[string]any{
		"output": string(make([]byte, PersistThreshold+1)),
	}
	result := ExecutionResult{
		ResultJSON: large,
	}
	raw, _ := json.Marshal(result.ResultJSON)

	for _, badID := range []string{"../../etc/passwd", "tc/4", "tc 4", "tc\\4", "", "tc\x004"} {
		out := PersistLargeResult(context.Background(), execCtx, badID, raw, result)
		if out.ResultJSON["persisted"] != nil {
			t.Fatalf("expected no persistence for invalid toolCallID %q", badID)
		}
	}
}

func TestPersistLargeResult_FallbackOnWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory test skipped on Windows")
	}
	// Use a read-only temp dir so WriteFile fails.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot make directory read-only on this platform")
	}
	defer os.Chmod(dir, 0o755)

	runID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	execCtx := ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
	}
	large := map[string]any{
		"output": string(make([]byte, PersistThreshold+1)),
	}
	result := ExecutionResult{
		ResultJSON: large,
	}
	raw, _ := json.Marshal(result.ResultJSON)
	out := PersistLargeResult(context.Background(), execCtx, "tc5", raw, result)
	if out.ResultJSON["persisted"] != nil {
		t.Fatalf("expected fallback when write fails")
	}
}

func TestPersistLargeResult_Preview(t *testing.T) {
	tests := []struct {
		name   string
		raw    []byte
		budget int
		want   string
	}{
		{
			name:   "fits budget",
			raw:    []byte("hello world"),
			budget: 100,
			want:   "hello world",
		},
		{
			name:   "truncates at newline",
			raw:    []byte("line one\nline two\nline three\nline four"),
			budget: 25,
			want:   "line one\nline two\n...[truncated]",
		},
		{
			name:   "truncates hard when no newline in second half",
			raw:    []byte("abcdefghijklmnopqrstuvwxyz"),
			budget: 20,
			want:   "abcdefghijklmnopqrst\n...[truncated]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generatePreview(tt.raw, tt.budget)
			if got != tt.want {
				t.Fatalf("generatePreview() = %q, want %q", got, tt.want)
			}
		})
	}
}
