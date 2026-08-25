//go:build darwin

package sandboxshell

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/worker/internal/tools"
)

func TestExecuteExecCommandUsesProcessExecEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(processResponse{
			Status:     "running",
			ProcessRef: "proc_1",
			Cursor:     "0",
			NextCursor: "1",
		})
	}))
	defer server.Close()

	exec := NewExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":    "echo hello",
		"mode":       "follow",
		"timeout_ms": 5000,
	}, tools.ExecutionContext{}, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if gotPath != "/v1/process/exec" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestContinueProcessClampsWaitTimeToSoftLimit(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(processResponse{Status: "running", ProcessRef: "proc_1", Cursor: "0", NextCursor: "0"})
	}))
	defer server.Close()

	maxWait := 1200
	exec := NewExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "continue_process", map[string]any{
		"process_ref": "proc_1",
		"cursor":      "0",
		"wait_ms":     5000,
	}, tools.ExecutionContext{
		PerToolSoftLimits: tools.PerToolSoftLimits{
			"continue_process": {MaxWaitTimeMs: &maxWait},
		},
	}, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if gotBody["wait_ms"] != float64(maxWait) {
		t.Fatalf("expected wait_ms to be clamped to %d, got %#v", maxWait, gotBody["wait_ms"])
	}
}
