package fileops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSandboxExecDoesNotSendHostCwd(t *testing.T) {
	var seen sandboxProcessExecRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sandboxProcessExecResponse{
			Status:   "exited",
			Stdout:   "",
			Stderr:   "",
			ExitCode: intPtr(0),
		})
	}))
	defer server.Close()

	backend := &SandboxExecBackend{
		baseURL:      server.URL,
		sessionID:    "run-1/file",
		workspaceRef: "ws-test",
	}
	if _, _, _, err := backend.exec(context.Background(), "pwd", 1000); err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if seen.Cwd != "" {
		t.Fatalf("expected sandbox exec cwd to be empty, got %q", seen.Cwd)
	}
}

func TestSandboxExecNormalizePathKeepsGuestRelativePath(t *testing.T) {
	backend := &SandboxExecBackend{}
	if got := backend.NormalizePath("./src/../src/app.txt"); got != "src/app.txt" {
		t.Fatalf("unexpected normalized path: %q", got)
	}
}

func intPtr(v int) *int { return &v }
