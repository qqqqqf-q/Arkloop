//go:build desktop

package localfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolvePath_RelativeInWorkspace(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	got, err := e.resolvePath("foo/bar.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/tmp/workspace/foo/bar.txt"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePath_TraversalBlocked(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	_, err := e.resolvePath("../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestResolvePath_AbsoluteInWorkspace(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	got, err := e.resolvePath("/tmp/workspace/subdir/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/tmp/workspace/subdir/file.txt"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePath_AbsoluteOutsideWorkspace(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	_, err := e.resolvePath("/etc/passwd")
	if err == nil {
		t.Error("expected error for path outside workspace, got nil")
	}
}

func TestResolvePath_Empty(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	_, err := e.resolvePath("")
	if err == nil {
		t.Error("expected error for empty path, got nil")
	}
}

func TestResolvePath_DotDotInMiddle(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	_, err := e.resolvePath("foo/../../bar")
	if err == nil {
		t.Error("expected error for path traversal via .., got nil")
	}
}

func TestResolvePath_WorkspaceRootItself(t *testing.T) {
	e := &Executor{workspaceRoot: "/tmp/workspace"}
	got, err := e.resolvePath(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Clean("/tmp/workspace")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyLineRange(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"

	tests := []struct {
		name      string
		offset    int
		limit     int
		wantLines string
		wantTrunc bool
	}{
		{"no range", 0, 0, content, false},
		{"offset 2", 2, 0, "line2\nline3\nline4\nline5", true},
		{"limit 2", 0, 2, "line1\nline2", true},
		{"offset 2 limit 2", 2, 2, "line2\nline3", true},
		{"offset beyond", 10, 0, "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := applyLineRange(content, tc.offset, tc.limit)
			if got != tc.wantLines {
				t.Errorf("content: got %q, want %q", got, tc.wantLines)
			}
			if truncated != tc.wantTrunc {
				t.Errorf("truncated: got %v, want %v", truncated, tc.wantTrunc)
			}
		})
	}
}

func TestExecuteReadWrite(t *testing.T) {
	dir := t.TempDir()
	e := &Executor{workspaceRoot: dir}

	// Write a file
	writeResult := e.executeWrite(map[string]any{
		"path":    "test.txt",
		"content": "hello world\n",
	}, now())
	if writeResult.Error != nil {
		t.Fatalf("write failed: %v", writeResult.Error)
	}
	if writeResult.ResultJSON["success"] != true {
		t.Error("expected success=true")
	}

	// Read back
	readResult := e.executeRead(map[string]any{
		"path": "test.txt",
	}, now())
	if readResult.Error != nil {
		t.Fatalf("read failed: %v", readResult.Error)
	}
	if readResult.ResultJSON["content"] != "hello world\n" {
		t.Errorf("content mismatch: got %q", readResult.ResultJSON["content"])
	}
}

func TestExecuteReadNotFound(t *testing.T) {
	dir := t.TempDir()
	e := &Executor{workspaceRoot: dir}
	result := e.executeRead(map[string]any{"path": "nonexistent.txt"}, now())
	if result.Error == nil {
		t.Error("expected error for nonexistent file")
	}
	if result.Error.ErrorClass != errorFileNotFound {
		t.Errorf("expected error class %s, got %s", errorFileNotFound, result.Error.ErrorClass)
	}
}

func TestExecuteWriteCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	e := &Executor{workspaceRoot: dir}

	result := e.executeWrite(map[string]any{
		"path":    "a/b/c/deep.txt",
		"content": "nested",
	}, now())
	if result.Error != nil {
		t.Fatalf("write failed: %v", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(dir, "a/b/c/deep.txt"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("content mismatch: got %q", string(data))
	}
}

func TestExecuteReadDirectory(t *testing.T) {
	dir := t.TempDir()
	e := &Executor{workspaceRoot: dir}

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	result := e.executeRead(map[string]any{"path": "."}, now())
	if result.Error != nil {
		t.Fatalf("read dir failed: %v", result.Error)
	}
	if result.ResultJSON["is_dir"] != true {
		t.Error("expected is_dir=true")
	}
}

func TestExecuteWritePathTraversal(t *testing.T) {
	dir := t.TempDir()
	e := &Executor{workspaceRoot: dir}

	result := e.executeWrite(map[string]any{
		"path":    "../../etc/evil",
		"content": "bad",
	}, now())
	if result.Error == nil {
		t.Error("expected error for path traversal")
	}
	if result.Error.ErrorClass != errorPathDenied {
		t.Errorf("expected error class %s, got %s", errorPathDenied, result.Error.ErrorClass)
	}
}

func now() time.Time { return time.Now() }
