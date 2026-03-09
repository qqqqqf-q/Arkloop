package sandbox

import (
	"arkloop/services/worker/internal/data"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/testutil"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testContext() tools.ExecutionContext {
	return tools.ExecutionContext{RunID: uuid.New()}
}

func testContextWithSoftLimits(limits tools.PerToolSoftLimits) tools.ExecutionContext {
	return tools.ExecutionContext{RunID: uuid.New(), PerToolSoftLimits: limits}
}

func testContextWithRun(runID uuid.UUID) tools.ExecutionContext {
	return tools.ExecutionContext{RunID: runID}
}

func testContextWithOrg(runID uuid.UUID, orgID uuid.UUID) tools.ExecutionContext {
	return tools.ExecutionContext{RunID: runID, OrgID: &orgID}
}

func TestPythonExecute_Success(t *testing.T) {
	fixedRunID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/exec" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("expected Authorization=Bearer test-token, got %s", auth)
		}

		var body execRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Language != "python" {
			t.Errorf("expected language=python, got %s", body.Language)
		}
		if body.Code != "print('hello')" {
			t.Errorf("unexpected code: %s", body.Code)
		}
		if body.SessionID != fixedRunID.String() {
			t.Errorf("expected session_ref=%s, got %s", fixedRunID.String(), body.SessionID)
		}
		if body.Tier != "lite" {
			t.Errorf("expected tier=lite, got %s", body.Tier)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{
			SessionID:  body.SessionID,
			Stdout:     "hello\n",
			Stderr:     "",
			ExitCode:   0,
			DurationMs: 42,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "test-token")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "print('hello')"},
		testContextWithRun(fixedRunID),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["stdout"] != "hello\n" {
		t.Errorf("unexpected stdout: %v", result.ResultJSON["stdout"])
	}
	if result.ResultJSON["exit_code"] != 0 {
		t.Errorf("unexpected exit_code: %v", result.ResultJSON["exit_code"])
	}
}

func TestExecCommand_UsesExecEndpoint(t *testing.T) {
	runID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	orgID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	seenSessionRef := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/exec_command" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer shell-token" {
			t.Fatalf("unexpected auth header: %s", auth)
		}
		if got := r.Header.Get("X-Org-ID"); got != orgID.String() {
			t.Fatalf("unexpected org header: %s", got)
		}

		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !strings.HasPrefix(body.SessionID, "shref_") {
			t.Fatalf("unexpected session id: %s", body.SessionID)
		}
		seenSessionRef = body.SessionID
		if body.OrgID != orgID.String() {
			t.Fatalf("unexpected org id: %s", body.OrgID)
		}
		if body.Command != "pwd" {
			t.Fatalf("unexpected command: %s", body.Command)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{
			SessionID: body.SessionID,
			Status:    "idle",
			Cwd:       "/workspace",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "shell-token")
	result := exec.Execute(
		t.Context(),
		"exec_command",
		map[string]any{"command": "pwd"},
		testContextWithOrg(runID, orgID),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["status"] != "idle" {
		t.Fatalf("unexpected status: %v", result.ResultJSON["status"])
	}
	if result.ResultJSON["cwd"] != "/workspace" {
		t.Fatalf("unexpected cwd: %v", result.ResultJSON["cwd"])
	}
	if result.ResultJSON["session_ref"] != seenSessionRef {
		t.Fatalf("unexpected session_ref: %v", result.ResultJSON["session_ref"])
	}
}

func TestExecCommand_UsesDefaultSessionID(t *testing.T) {
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	seenSessionRef := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/exec_command" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !strings.HasPrefix(body.SessionID, "shref_") {
			t.Fatalf("unexpected session id: %s", body.SessionID)
		}
		seenSessionRef = body.SessionID
		if body.Command != "ls -la" {
			t.Fatalf("unexpected command: %s", body.Command)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{
			SessionID: body.SessionID,
			Status:    "idle",
			Cwd:       "/workspace",
			Output:    "total 0\n",
			Running:   false,
			TimedOut:  false,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	seedInMemorySession(exec.orchestrator, "sess-42")
	result := exec.Execute(
		t.Context(),
		"exec_command",
		map[string]any{"command": "ls -la"},
		testContextWithRun(runID),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["output"] != "total 0\n" {
		t.Fatalf("unexpected output: %v", result.ResultJSON["output"])
	}
	if result.ResultJSON["running"] != false {
		t.Fatalf("unexpected running: %v", result.ResultJSON["running"])
	}
	if result.ResultJSON["session_ref"] != seenSessionRef {
		t.Fatalf("unexpected session_ref: %v", result.ResultJSON["session_ref"])
	}
}

func TestWriteStdin_UsesPollEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/write_stdin" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body writeStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.SessionID != "sess-42" {
			t.Fatalf("unexpected session_ref: %s", body.SessionID)
		}
		if body.YieldTimeMs != 1500 {
			t.Fatalf("unexpected yield_time_ms: %d", body.YieldTimeMs)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Running: true})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	seedInMemorySession(exec.orchestrator, "sess-42")
	result := exec.Execute(
		t.Context(),
		"write_stdin",
		map[string]any{"session_ref": "sess-42", "yield_time_ms": float64(1500)},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["running"] != true {
		t.Fatalf("unexpected running: %v", result.ResultJSON["running"])
	}
}

func TestWriteStdin_UsesCharsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/write_stdin" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body writeStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Chars != "arkloop\n" {
			t.Fatalf("unexpected chars: %q", body.Chars)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", Output: "arkloop\n"})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	seedInMemorySession(exec.orchestrator, "sess-1")
	result := exec.Execute(t.Context(), "write_stdin", map[string]any{"session_ref": "sess-1", "chars": "arkloop\n"}, testContext(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
}

func TestExecCommandAndWriteStdin_ShareDefaultSessionAcrossCalls(t *testing.T) {
	runID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	var got []string
	firstSessionRef := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/exec_command" {
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			got = append(got, body.SessionID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Running: true})
			return
		}
		var body writeStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		got = append(got, body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	ctx := testContextWithRun(runID)
	first := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "sleep 1"}, ctx, "")
	firstSessionRef, _ = first.ResultJSON["session_ref"].(string)
	second := exec.Execute(t.Context(), "write_stdin", map[string]any{"session_ref": firstSessionRef}, ctx, "")

	if first.Error != nil || second.Error != nil {
		t.Fatalf("unexpected errors: first=%+v second=%+v", first.Error, second.Error)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(got))
	}
	for _, sessionID := range got {
		if sessionID != firstSessionRef {
			t.Fatalf("unexpected session id: %s", sessionID)
		}
	}
}

func TestPythonExecute_MissingCode(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestExecCommand_MissingCommand(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(
		t.Context(),
		"exec_command",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestWriteStdin_MissingSessionID(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(
		t.Context(),
		"write_stdin",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestNotConfigured(t *testing.T) {
	exec := NewToolExecutor("", "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorNotConfigured {
		t.Fatalf("expected not_configured, got: %+v", result.Error)
	}
}

func TestUnknownTool(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(
		t.Context(),
		"sandbox_unknown",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestHTTPError_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "sandbox.exec_error",
			"message": "VM crashed",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "import sys; sys.exit(1)"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorSandboxError {
		t.Fatalf("expected sandbox_error, got: %+v", result.Error)
	}
	if result.Error.Message != "VM crashed" {
		t.Errorf("unexpected message: %s", result.Error.Message)
	}
}

func TestHTTPError_ServiceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorSandboxUnavailable {
		t.Fatalf("expected sandbox_unavailable, got: %+v", result.Error)
	}
}

func TestHTTPError_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "timeout",
			"message": "execution timed out",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "import time; time.sleep(999)"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorSandboxTimeout {
		t.Fatalf("expected sandbox_timeout, got: %+v", result.Error)
	}
}

func TestNonZeroExitCode_NotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{
			Stdout:     "",
			Stderr:     "error: division by zero\n",
			ExitCode:   1,
			DurationMs: 5,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "1/0"},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["exit_code"] != 1 {
		t.Errorf("expected exit_code=1, got %v", result.ResultJSON["exit_code"])
	}
	if result.ResultJSON["stderr"] != "error: division by zero\n" {
		t.Errorf("unexpected stderr: %v", result.ResultJSON["stderr"])
	}
}

func TestOutputTruncation(t *testing.T) {
	largeOutput := strings.Repeat("x", 40*1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{
			Stdout:     largeOutput,
			Stderr:     "",
			ExitCode:   0,
			DurationMs: 100,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "print('x'*40960)"},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	stdout := result.ResultJSON["stdout"].(string)
	if len(stdout) > maxOutputBytes+200 {
		t.Errorf("stdout not properly truncated: %d bytes", len(stdout))
	}
	if !strings.Contains(stdout, "truncated") {
		t.Error("truncated output should contain truncation marker")
	}
}

func TestPythonExecute_UsesLiteTierByDefault(t *testing.T) {
	var receivedTier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedTier = body.Tier
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{ExitCode: 0})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x=1"},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedTier != "lite" {
		t.Errorf("expected tier=lite, got %s", receivedTier)
	}
}

func TestExecCommand_UsesProTierByDefault(t *testing.T) {
	var receivedTier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedTier = body.Tier
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle"})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"exec_command",
		map[string]any{"command": "pwd"},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedTier != "pro" {
		t.Errorf("expected tier=pro, got %s", receivedTier)
	}
}

func TestTierFromSandboxProfilesToolOverride(t *testing.T) {
	var receivedTier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedTier = body.Tier
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{ExitCode: 0})
	}))
	defer server.Close()

	ctx := testContext()
	ctx.Budget = map[string]any{"sandbox_profiles": map[string]any{"python_execute": "pro"}}

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x=1"},
		ctx,
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedTier != "pro" {
		t.Errorf("expected tier=pro, got %s", receivedTier)
	}
}

func TestTierFromSandboxProfilesWorkloadOverride(t *testing.T) {
	var receivedTier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedTier = body.Tier
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle"})
	}))
	defer server.Close()

	ctx := testContext()
	ctx.Budget = map[string]any{"sandbox_profiles": map[string]any{"interactive_shell": "lite"}}

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"exec_command",
		map[string]any{"command": "pwd"},
		ctx,
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedTier != "lite" {
		t.Errorf("expected tier=lite, got %s", receivedTier)
	}
}

func TestLegacySandboxTierIsIgnored(t *testing.T) {
	var receivedTier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedTier = body.Tier
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{ExitCode: 0})
	}))
	defer server.Close()

	ctx := testContext()
	ctx.Budget = map[string]any{"sandbox_tier": "pro"}

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x=1"},
		ctx,
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedTier != "lite" {
		t.Errorf("expected legacy sandbox_tier ignored, got %s", receivedTier)
	}
}

func TestTimeoutMs_Propagation(t *testing.T) {
	var receivedTimeout int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedTimeout = body.TimeoutMs
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{ExitCode: 0})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x=1", "timeout_ms": float64(60000)},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedTimeout != 60000 {
		t.Errorf("expected timeout_ms=60000, got %d", receivedTimeout)
	}
}

func TestWriteStdin_ClampsYieldTimeMsBySoftLimit(t *testing.T) {
	var receivedYield int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body writeStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		receivedYield = body.YieldTimeMs
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Running: true})
	}))
	defer server.Close()

	limits := tools.DefaultPerToolSoftLimits()
	writeLimit := limits["write_stdin"]
	writeLimit.MaxYieldTimeMs = intPtr(2500)
	limits["write_stdin"] = writeLimit

	exec := NewToolExecutor(server.URL, "")
	seedInMemorySession(exec.orchestrator, "sess-1")
	result := exec.Execute(
		t.Context(),
		"write_stdin",
		map[string]any{"session_ref": "sess-1", "yield_time_ms": float64(9000)},
		testContextWithSoftLimits(limits),
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedYield != 2500 {
		t.Fatalf("expected clamped yield_time_ms=2500, got %d", receivedYield)
	}
}

func TestExecCommand_TruncatesOutputBySoftLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{
			SessionID: "sess-1",
			Status:    "idle",
			Running:   false,
			Output:    strings.Repeat("x", 200),
		})
	}))
	defer server.Close()

	limits := tools.DefaultPerToolSoftLimits()
	execLimit := limits["exec_command"]
	execLimit.MaxOutputBytes = intPtr(64)
	limits["exec_command"] = execLimit

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"exec_command",
		map[string]any{"command": "echo hi"},
		testContextWithSoftLimits(limits),
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	output, _ := result.ResultJSON["output"].(string)
	if len(output) > 64 {
		t.Fatalf("expected truncated output <= 64 bytes, got %d", len(output))
	}
	if result.ResultJSON["truncated"] != true {
		t.Fatalf("expected truncated=true, got %v", result.ResultJSON["truncated"])
	}
}

func intPtr(value int) *int {
	return &value
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	exec.client.Timeout = 100 * time.Millisecond

	result := exec.Execute(
		t.Context(),
		"python_execute",
		map[string]any{"code": "x=1"},
		testContext(),
		"",
	)
	if result.Error == nil {
		t.Fatal("expected timeout error")
	}
	if result.Error.ErrorClass != errorSandboxTimeout && result.Error.ErrorClass != errorSandboxUnavailable {
		t.Fatalf("expected timeout or unavailable, got: %s", result.Error.ErrorClass)
	}
}

func TestOrgID_Propagation(t *testing.T) {
	var receivedOrgID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execRequest
		json.NewDecoder(r.Body).Decode(&body)
		receivedOrgID = body.OrgID
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{ExitCode: 0})
	}))
	defer server.Close()

	orgID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID: uuid.New(),
		OrgID: &orgID,
	}

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "python_execute", map[string]any{"code": "x=1"}, ctx, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if receivedOrgID != orgID.String() {
		t.Errorf("expected org_id=%s, got %s", orgID.String(), receivedOrgID)
	}
}

func TestNoAuthHeader_WhenTokenEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{ExitCode: 0})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "python_execute", map[string]any{"code": "x=1"}, testContext(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
}

func TestExecCommand_AutoReusesThreadDefaultAcrossRunsWithPool(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_exec_refs")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	threadID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	ctx1 := tools.ExecutionContext{
		RunID:        uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		OrgID:        &orgID,
		ThreadID:     &threadID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	ctx2 := tools.ExecutionContext{
		RunID:        uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa"),
		OrgID:        &orgID,
		ThreadID:     &threadID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}

	var sessionIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/exec_command" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		sessionIDs = append(sessionIDs, body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	first := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx1, "")
	second := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx2, "")
	if first.Error != nil || second.Error != nil {
		t.Fatalf("unexpected errors: first=%+v second=%+v", first.Error, second.Error)
	}
	if len(sessionIDs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(sessionIDs))
	}
	if sessionIDs[0] != sessionIDs[1] {
		t.Fatalf("expected same session_ref across runs, got %q vs %q", sessionIDs[0], sessionIDs[1])
	}
	if second.ResultJSON["resolved_via"] != "thread_default" {
		t.Fatalf("unexpected resolved_via: %v", second.ResultJSON["resolved_via"])
	}
}

func TestExecCommand_ForkRequiresFromSessionRef(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd", "session_mode": "fork"}, testContext(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got %+v", result.Error)
	}
}

func TestExecCommand_UsesCreateOpenModeForNewSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.OpenMode != openModeCreate {
			t.Fatalf("expected open_mode=%s, got %s", openModeCreate, body.OpenMode)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, testContext(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
}

func TestExecCommand_ResumeWithoutLiveOrRestoreFails(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_resume_strict")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_existing",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   data.ShellShareScopeWorkspace,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.OpenMode != openModeAttachOrRestore {
			t.Fatalf("expected attach_or_restore, got %s", body.OpenMode)
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"code": "sandbox.session_not_found", "message": "session not found"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_existing",
	}, ctx, "")
	if result.Error == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected single request, got %d", calls)
	}
}

func TestExecCommand_AutoFallsBackAfterStaleThreadDefault(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_auto_fallback")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	threadID := uuid.New()
	bindingKey := "thread:" + threadID.String()
	liveSessionID := "shref_old"
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:        "shref_old",
		OrgID:             orgID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      "wsref_test",
		ThreadID:          &threadID,
		ShareScope:        data.ShellShareScopeThread,
		State:             data.ShellSessionStateBusy,
		LiveSessionID:     &liveSessionID,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	var modes []string
	var sessionIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		modes = append(modes, body.OpenMode)
		sessionIDs = append(sessionIDs, body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		if body.SessionID == "shref_old" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"code": "sandbox.session_not_found", "message": "session not found"})
			return
		}
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		ThreadID:     &threadID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if len(sessionIDs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(sessionIDs))
	}
	if sessionIDs[0] != "shref_old" {
		t.Fatalf("expected first stale session, got %s", sessionIDs[0])
	}
	if modes[0] != openModeAttachOrRestore {
		t.Fatalf("expected first open_mode attach_or_restore, got %s", modes[0])
	}
	if modes[1] != openModeCreate {
		t.Fatalf("expected fallback open_mode create, got %s", modes[1])
	}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, "shref_old")
	if err != nil {
		t.Fatalf("reload stale session: %v", err)
	}
	if stored.LiveSessionID != nil {
		t.Fatalf("expected stale live_session_id cleared, got %#v", stored.LiveSessionID)
	}
}

func TestExecCommand_AutoFallsBackAfterStaleWorkspaceDefaultKeepsWorkspaceScope(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_workspace_fallback_scope")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	workspaceRef := "wsref_test"
	bindingKey := "workspace:" + workspaceRef
	liveSessionID := "shref_workspace_old"
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:        "shref_workspace_old",
		OrgID:             orgID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      workspaceRef,
		ShareScope:        data.ShellShareScopeWorkspace,
		State:             data.ShellSessionStateBusy,
		LiveSessionID:     &liveSessionID,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	var sessionIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		sessionIDs = append(sessionIDs, body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		if body.SessionID == "shref_workspace_old" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"code": "sandbox.session_not_found", "message": "session not found"})
			return
		}
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: workspaceRef}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx, "call_workspace_fallback")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	newSessionRef, _ := result.ResultJSON["session_ref"].(string)
	if len(sessionIDs) != 2 || sessionIDs[1] != newSessionRef {
		t.Fatalf("unexpected fallback sessions: %#v", sessionIDs)
	}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, newSessionRef)
	if err != nil {
		t.Fatalf("get fallback session: %v", err)
	}
	if stored.ShareScope != data.ShellShareScopeWorkspace {
		t.Fatalf("expected workspace scope, got %s", stored.ShareScope)
	}
	if stored.DefaultBindingKey == nil || *stored.DefaultBindingKey != bindingKey {
		t.Fatalf("expected workspace default binding preserved, got %#v", stored.DefaultBindingKey)
	}
}

func TestExecCommand_WorkspaceDefaultUpdatesWorkspaceRegistry(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_workspace_default_registry")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	projectID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		ProjectID:    &projectID,
		UserID:       &userID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	sessionRef, _ := result.ResultJSON["session_ref"].(string)
	workspaceRepo := data.WorkspaceRegistriesRepository{}
	stored, err := workspaceRepo.Get(t.Context(), pool, "wsref_test")
	if err != nil {
		t.Fatalf("get workspace registry: %v", err)
	}
	if stored.DefaultShellSessionRef == nil || *stored.DefaultShellSessionRef != sessionRef {
		t.Fatalf("unexpected default_shell_session_ref: %#v", stored.DefaultShellSessionRef)
	}
	if stored.ProjectID == nil || *stored.ProjectID != projectID {
		t.Fatalf("unexpected project_id: %#v", stored.ProjectID)
	}
	if stored.OwnerUserID == nil || *stored.OwnerUserID != userID {
		t.Fatalf("unexpected owner_user_id: %#v", stored.OwnerUserID)
	}
}

func seedInMemorySession(orchestrator *sessionOrchestrator, sessionRef string) {
	orchestrator.mu.Lock()
	defer orchestrator.mu.Unlock()
	orchestrator.memorySessions[sessionRef] = data.ShellSessionRecord{
		SessionRef:   sessionRef,
		SessionType:  orchestrator.sessionType,
		ShareScope:   data.ShellShareScopeRun,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}
}

func TestExecCommand_InvalidShareScopeRejected(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":     "pwd",
		"share_scope": "invalid",
	}, testContext(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got %+v", result.Error)
	}
}

func TestExecCommand_ResumeRejectsShareScope(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_test",
		"share_scope":  data.ShellShareScopeThread,
	}, testContext(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got %+v", result.Error)
	}
}

func TestExecCommand_ForkRejectsShareScope(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":          "pwd",
		"session_mode":     "fork",
		"from_session_ref": "shref_test",
		"share_scope":      data.ShellShareScopeWorkspace,
	}, testContext(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got %+v", result.Error)
	}
}

func TestWriteStdin_RejectsShareScope(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(t.Context(), "write_stdin", map[string]any{
		"session_ref":   "shref_test",
		"share_scope":   data.ShellShareScopeThread,
		"yield_time_ms": 1,
	}, testContext(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got %+v", result.Error)
	}
}

func TestExecCommand_NewSessionPersistsRequestedShareScope(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_new_share_scope")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	seedMembership(t, pool, orgID, userID, "org_admin")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		UserID:       &userID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "new",
		"share_scope":  data.ShellShareScopeOrg,
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["share_scope"] != data.ShellShareScopeOrg {
		t.Fatalf("unexpected share_scope: %v", result.ResultJSON["share_scope"])
	}
	sessionRef, _ := result.ResultJSON["session_ref"].(string)
	repo := data.ShellSessionsRepository{}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, sessionRef)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.ShareScope != data.ShellShareScopeOrg {
		t.Fatalf("unexpected stored share_scope: %s", stored.ShareScope)
	}
}

func TestExecCommand_ResumeRunScopeRequiresSameRun(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_acl_run")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	repo := data.ShellSessionsRepository{}
	otherRunID := uuid.New()
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_run_only",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		RunID:        &otherRunID,
		ShareScope:   data.ShellShareScopeRun,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	exec := NewToolExecutorWithPool("http://localhost:9999", "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_run_only",
	}, ctx, "")
	if result.Error == nil || result.Error.ErrorClass != errorPermissionDenied {
		t.Fatalf("expected permission_denied, got %+v", result.Error)
	}
}

func TestExecCommand_ResumeOrgScopeRejectedForMember(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_acl_org_member")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	seedMembership(t, pool, orgID, userID, "org_member")
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_org",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   data.ShellShareScopeOrg,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	exec := NewToolExecutorWithPool("http://localhost:9999", "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, UserID: &userID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_org",
	}, ctx, "")
	if result.Error == nil || result.Error.ErrorClass != errorPermissionDenied {
		t.Fatalf("expected permission_denied, got %+v", result.Error)
	}
}

func TestExecCommand_ResumeOrgScopeAllowedForAdmin(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_acl_org_admin")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	seedMembership(t, pool, orgID, userID, "org_admin")
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_org_ok",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   data.ShellShareScopeOrg,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, UserID: &userID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_org_ok",
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if calls != 1 {
		t.Fatalf("expected one sandbox call, got %d", calls)
	}
	if result.ResultJSON["share_scope"] != data.ShellShareScopeOrg {
		t.Fatalf("unexpected share_scope: %v", result.ResultJSON["share_scope"])
	}
}

func TestExecCommand_ResumeOrgScopePreservesSourceWorkspaceIdentity(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_org_resume_identity")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	seedMembership(t, pool, orgID, userID, "org_admin")
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_org_identity",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_source",
		ProjectID:    uuidPtr(uuid.New()),
		ShareScope:   data.ShellShareScopeOrg,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	var body execCommandRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, UserID: &userID, ProfileRef: "pref_test", WorkspaceRef: "wsref_caller"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_org_identity",
	}, ctx, "call_org_resume")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if body.WorkspaceRef != "wsref_source" {
		t.Fatalf("expected source workspace_ref in sandbox request, got %s", body.WorkspaceRef)
	}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, "shref_org_identity")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if stored.WorkspaceRef != "wsref_source" {
		t.Fatalf("expected persisted workspace_ref unchanged, got %s", stored.WorkspaceRef)
	}
}

func TestExecCommand_ResumeOrgScopeRejectsCrossProfile(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_acl_org_profile")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	seedMembership(t, pool, orgID, userID, "org_admin")
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_other_profile",
		OrgID:        orgID,
		ProfileRef:   "pref_other",
		WorkspaceRef: "wsref_test",
		ShareScope:   data.ShellShareScopeOrg,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	exec := NewToolExecutorWithPool("http://localhost:9999", "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, UserID: &userID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_other_profile",
	}, ctx, "")
	if result.Error == nil || result.Error.ErrorClass != errorPermissionDenied {
		t.Fatalf("expected permission_denied, got %+v", result.Error)
	}
}

func TestExecCommand_AutoSkipsUnauthorizedCandidateAndCreatesNew(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_acl_auto_skip")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	workspaceRef := "wsref_test"
	seedMembership(t, pool, orgID, userID, "org_member")
	repo := data.ShellSessionsRepository{}
	bindingKey := "workspace:" + workspaceRef
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:        "shref_forbidden",
		OrgID:             orgID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      workspaceRef,
		ShareScope:        data.ShellShareScopeOrg,
		State:             data.ShellSessionStateReady,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	var sessionIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		sessionIDs = append(sessionIDs, body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, UserID: &userID, ProfileRef: "pref_test", WorkspaceRef: workspaceRef}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if len(sessionIDs) != 1 {
		t.Fatalf("expected one sandbox call, got %d", len(sessionIDs))
	}
	if sessionIDs[0] == "shref_forbidden" {
		t.Fatalf("expected unauthorized session skipped, got %q", sessionIDs[0])
	}
	if result.ResultJSON["resolved_via"] != "new_session" {
		t.Fatalf("unexpected resolved_via: %v", result.ResultJSON["resolved_via"])
	}
}

func TestExecCommand_ForkInheritsShareScope(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_acl_fork_scope")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	userID := uuid.New()
	seedMembership(t, pool, orgID, userID, "org_admin")
	repo := data.ShellSessionsRepository{}
	restoreRev := "rev-1"
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:       "shref_source",
		OrgID:            orgID,
		ProfileRef:       "pref_test",
		WorkspaceRef:     "wsref_test",
		ShareScope:       data.ShellShareScopeOrg,
		State:            data.ShellSessionStateReady,
		LatestRestoreRev: &restoreRev,
		MetadataJSON:     map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions/fork":
			json.NewEncoder(w).Encode(forkSessionResponse{RestoreRevision: "rev-2"})
		case "/v1/exec_command":
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, UserID: &userID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":          "pwd",
		"session_mode":     "fork",
		"from_session_ref": "shref_source",
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["share_scope"] != data.ShellShareScopeOrg {
		t.Fatalf("unexpected share_scope: %v", result.ResultJSON["share_scope"])
	}
	newSessionRef, _ := result.ResultJSON["session_ref"].(string)
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, newSessionRef)
	if err != nil {
		t.Fatalf("get forked session: %v", err)
	}
	if stored.ShareScope != data.ShellShareScopeOrg {
		t.Fatalf("unexpected stored share_scope: %s", stored.ShareScope)
	}
	if stored.ProfileRef != "pref_test" || stored.WorkspaceRef != "wsref_test" {
		t.Fatalf("expected forked session identity preserved, got profile=%s workspace=%s", stored.ProfileRef, stored.WorkspaceRef)
	}
}

func TestExecCommandAndWriteStdin_SameRunKeepsWriterLease(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_same_run_writer_lease")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	runID := uuid.New()
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/exec_command":
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode exec body: %v", err)
			}
			calls = append(calls, "exec:"+body.SessionID)
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Running: true})
		case "/v1/write_stdin":
			var body writeStdinRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode write body: %v", err)
			}
			calls = append(calls, "write:"+body.SessionID)
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Running: true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: runID, OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	first := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "python server.py"}, ctx, "call_shared_writer")
	if first.Error != nil {
		t.Fatalf("unexpected exec error: %+v", first.Error)
	}
	sessionRef, _ := first.ResultJSON["session_ref"].(string)
	second := exec.Execute(t.Context(), "write_stdin", map[string]any{"session_ref": sessionRef, "chars": "yes\n"}, ctx, "call_shared_writer")
	if second.Error != nil {
		t.Fatalf("unexpected write error: %+v", second.Error)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 sandbox calls, got %d", len(calls))
	}
	repo := data.ShellSessionsRepository{}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, sessionRef)
	if err != nil {
		t.Fatalf("get stored session: %v", err)
	}
	if stored.LeaseOwnerID == nil || *stored.LeaseOwnerID != "run:"+runID.String()+":call:call_shared_writer" {
		t.Fatalf("unexpected lease owner: %#v", stored.LeaseOwnerID)
	}
	if stored.State != data.ShellSessionStateBusy {
		t.Fatalf("expected busy after running write, got %s", stored.State)
	}
	if stored.LeaseUntil == nil || !stored.LeaseUntil.After(time.Now().UTC()) {
		t.Fatalf("expected active lease_until, got %#v", stored.LeaseUntil)
	}
}

func TestWriteStdin_SameRunDifferentToolCallIDRejected(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_same_run_writer_conflict")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	runID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/exec_command":
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode exec body: %v", err)
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Running: true})
		case "/v1/write_stdin":
			t.Fatalf("write_stdin should be rejected before sandbox")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: runID, OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	first := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "python server.py"}, ctx, "call_writer_owner")
	if first.Error != nil {
		t.Fatalf("unexpected exec error: %+v", first.Error)
	}
	sessionRef, _ := first.ResultJSON["session_ref"].(string)
	second := exec.Execute(t.Context(), "write_stdin", map[string]any{"session_ref": sessionRef, "chars": "yes\n"}, ctx, "call_other_writer")
	if second.Error == nil || second.Error.ErrorClass != errorSandboxError {
		t.Fatalf("expected busy sandbox_error, got %+v", second.Error)
	}
	if retryVia, _ := second.Error.Details["retry_via"].(string); retryVia != "wait_for_current_writer" {
		t.Fatalf("unexpected retry_via: %+v", second.Error.Details)
	}
}

func TestExecCommand_BusySessionRejectedBeforeSandbox(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_exec_busy_reject")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	threadID := uuid.New()
	leaseUntil := time.Now().UTC().Add(2 * time.Minute)
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_busy",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ThreadID:     &threadID,
		ShareScope:   data.ShellShareScopeThread,
		State:        data.ShellSessionStateBusy,
		LeaseOwnerID: stringPtr("run:" + uuid.NewString()),
		LeaseUntil:   &leaseUntil,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		t.Fatalf("sandbox should not be called")
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, ThreadID: &threadID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "pwd",
		"session_mode": "resume",
		"session_ref":  "shref_busy",
	}, ctx, "")
	if result.Error == nil || result.Error.ErrorClass != errorSandboxError {
		t.Fatalf("expected sandbox_error busy, got %+v", result.Error)
	}
	if code, _ := result.Error.Details["code"].(string); code != "shell.session_busy" {
		t.Fatalf("unexpected busy code: %+v", result.Error)
	}
	if retryVia, _ := result.Error.Details["retry_via"].(string); retryVia != "fork" {
		t.Fatalf("unexpected retry_via: %+v", result.Error)
	}
	if called != 0 {
		t.Fatalf("expected 0 sandbox calls, got %d", called)
	}
}

func TestWriteStdin_PollAllowedButCrossRunInputRejected(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_write_poll_acl")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	leaseUntil := time.Now().UTC().Add(2 * time.Minute)
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_busy",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   data.ShellShareScopeWorkspace,
		State:        data.ShellSessionStateBusy,
		LeaseOwnerID: stringPtr("run:" + uuid.NewString()),
		LeaseUntil:   &leaseUntil,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	writeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/write_stdin" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeCalls++
		var body writeStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode write body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", Running: false})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: uuid.New(), OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	busy := exec.Execute(t.Context(), "write_stdin", map[string]any{"session_ref": "shref_busy", "chars": "no\n"}, ctx, "")
	if busy.Error == nil || busy.Error.ErrorClass != errorSandboxError {
		t.Fatalf("expected busy sandbox_error, got %+v", busy.Error)
	}
	if writeCalls != 0 {
		t.Fatalf("expected 0 sandbox calls for cross-run write, got %d", writeCalls)
	}

	poll := exec.Execute(t.Context(), "write_stdin", map[string]any{"session_ref": "shref_busy"}, ctx, "")
	if poll.Error != nil {
		t.Fatalf("unexpected poll error: %+v", poll.Error)
	}
	if writeCalls != 1 {
		t.Fatalf("expected 1 sandbox call after poll, got %d", writeCalls)
	}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, "shref_busy")
	if err != nil {
		t.Fatalf("get stored session: %v", err)
	}
	if stored.LeaseOwnerID != nil || stored.LeaseUntil != nil {
		t.Fatalf("expected poll completion to clear lease, got owner=%#v until=%#v", stored.LeaseOwnerID, stored.LeaseUntil)
	}
	if stored.State != data.ShellSessionStateReady {
		t.Fatalf("expected ready after poll completion, got %s", stored.State)
	}
}

func TestExecCommand_ExpiredLeaseCanBeTakenOver(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_sandbox_exec_takeover")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	leaseUntil := time.Now().UTC().Add(-time.Minute)
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:   "shref_stale",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   data.ShellShareScopeWorkspace,
		State:        data.ShellSessionStateBusy,
		LeaseOwnerID: stringPtr("run:" + uuid.NewString()),
		LeaseUntil:   &leaseUntil,
		LeaseEpoch:   2,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("seed stale session: %v", err)
	}

	runID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode exec body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Running: true})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{RunID: runID, OrgID: &orgID, ProfileRef: "pref_test", WorkspaceRef: "wsref_test"}
	result := exec.Execute(t.Context(), "exec_command", map[string]any{
		"command":      "tail -f server.log",
		"session_mode": "resume",
		"session_ref":  "shref_stale",
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected exec error: %+v", result.Error)
	}
	stored, err := repo.GetBySessionRef(t.Context(), pool, orgID, "shref_stale")
	if err != nil {
		t.Fatalf("get stored stale session: %v", err)
	}
	if stored.LeaseOwnerID == nil || *stored.LeaseOwnerID != "run:"+runID.String()+":call:direct" {
		t.Fatalf("unexpected takeover owner: %#v", stored.LeaseOwnerID)
	}
	if stored.LeaseEpoch != 3 {
		t.Fatalf("expected lease epoch increment on takeover, got %d", stored.LeaseEpoch)
	}
}

func seedMembership(t *testing.T, pool *pgxpool.Pool, orgID uuid.UUID, userID uuid.UUID, role string) {
	t.Helper()
	_, err := pool.Exec(
		t.Context(),
		`INSERT INTO org_memberships (org_id, user_id, role)
		 VALUES ($1, $2, $3)`,
		orgID,
		userID,
		role,
	)
	if err != nil {
		t.Fatalf("insert membership: %v", err)
	}
}

func TestBrowser_UsesBrowserTierAndAgentBrowserCommand(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        runID,
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var calls []execCommandRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/exec_command" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		calls = append(calls, body)
		w.Header().Set("Content-Type", "application/json")
		resp := execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", Output: "ok", ExitCode: intPtr(0)}
		if len(calls) == 2 {
			resp.Artifacts = []artifactRef{{
				Key:      "org/browser/2/browser-screenshot.png",
				Filename: "browser-screenshot.png",
				Size:     1024,
				MimeType: "image/png",
			}}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{"command": "navigate https://example.com"}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if len(calls) < 1 {
		t.Fatalf("expected at least 1 call, got %d", len(calls))
	}
	primary := calls[0]
	if primary.Tier != "browser" {
		t.Fatalf("expected browser tier, got %q", primary.Tier)
	}
	if !strings.HasPrefix(primary.SessionID, "brref_") {
		t.Fatalf("expected browser session ref, got %q", primary.SessionID)
	}
	if !strings.HasPrefix(primary.Command, "agent-browser --session '") {
		t.Fatalf("unexpected browser command: %q", primary.Command)
	}
	if !strings.Contains(primary.Command, "navigate https://example.com") {
		t.Fatalf("missing browser subcommand: %q", primary.Command)
	}
	if result.ResultJSON["session_ref"] != primary.SessionID {
		t.Fatalf("unexpected session_ref: %v", result.ResultJSON["session_ref"])
	}
	// navigate triggers auto-screenshot
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (navigate + screenshot), got %d", len(calls))
	}
	screenshot := calls[1]
	if !strings.Contains(screenshot.Command, buildAutoScreenshotCommand()) {
		t.Fatalf("expected screenshot command, got %q", screenshot.Command)
	}
	if screenshot.SessionID != primary.SessionID {
		t.Fatalf("screenshot session mismatch: %q vs %q", screenshot.SessionID, primary.SessionID)
	}
	artifacts, ok := result.ResultJSON["artifacts"].([]artifactRef)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected merged screenshot artifact, got %#v", result.ResultJSON["artifacts"])
	}
	if result.ResultJSON["has_screenshot"] != true {
		t.Fatalf("expected has_screenshot=true, got %#v", result.ResultJSON["has_screenshot"])
	}
}

func TestBrowser_ForwardsYieldTimeMs(t *testing.T) {
	orgID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var seen execCommandRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: seen.SessionID, Status: "idle", Cwd: "/workspace", ExitCode: intPtr(0)})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{
		"command":       "snapshot",
		"yield_time_ms": float64(2500),
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if seen.YieldTimeMs != 2500 {
		t.Fatalf("expected yield_time_ms=2500, got %d", seen.YieldTimeMs)
	}
}

func TestBrowser_AutoScreenshotCommandRaisesTinyYieldTime(t *testing.T) {
	orgID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var seen execCommandRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: seen.SessionID, Status: "idle", Cwd: "/workspace", ExitCode: intPtr(0)})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{
		"command":       "navigate https://example.com",
		"yield_time_ms": float64(50),
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if seen.YieldTimeMs != autoScreenshotMinYieldTimeMs {
		t.Fatalf("expected yield_time_ms=%d, got %d", autoScreenshotMinYieldTimeMs, seen.YieldTimeMs)
	}
}

func TestBrowser_AutoPollsRunningResultBeforeScreenshot(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        runID,
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var paths []string
	var pollSeen writeStdinRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/exec_command":
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode exec body: %v", err)
			}
			if len(paths) == 1 {
				json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Output: "loading", Running: true})
				return
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", Output: "ok", ExitCode: intPtr(0), Artifacts: []artifactRef{{
				Key:      "org/browser/3/browser-screenshot.png",
				Filename: "browser-screenshot.png",
				Size:     1024,
				MimeType: "image/png",
			}}})
		case "/v1/write_stdin":
			if err := json.NewDecoder(r.Body).Decode(&pollSeen); err != nil {
				t.Fatalf("decode poll body: %v", err)
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: pollSeen.SessionID, Status: "idle", Cwd: "/workspace", Output: "Example Domain", ExitCode: intPtr(0)})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{"command": "navigate https://example.com", "yield_time_ms": float64(50)}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if !reflect.DeepEqual(paths, []string{"/v1/exec_command", "/v1/write_stdin", "/v1/exec_command"}) {
		t.Fatalf("unexpected request paths: %#v", paths)
	}
	if pollSeen.YieldTimeMs != autoScreenshotMinYieldTimeMs {
		t.Fatalf("expected poll yield_time_ms=%d, got %d", autoScreenshotMinYieldTimeMs, pollSeen.YieldTimeMs)
	}
	if result.ResultJSON["running"] != false {
		t.Fatalf("expected settled browser result, got %#v", result.ResultJSON)
	}
	if result.ResultJSON["has_screenshot"] != true {
		t.Fatalf("expected has_screenshot=true, got %#v", result.ResultJSON["has_screenshot"])
	}
	artifacts, ok := result.ResultJSON["artifacts"].([]artifactRef)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected screenshot artifact after polling, got %#v", result.ResultJSON["artifacts"])
	}
	if got := result.ResultJSON["output"]; got != "Example Domain" {
		t.Fatalf("expected settled output, got %#v", got)
	}
}

func TestBrowser_DoesNotAutoScreenshotWhileRunning(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        runID,
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var execCalls int
	var pollCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/exec_command":
			execCalls++
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode exec body: %v", err)
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Output: "ok", Running: true})
		case "/v1/write_stdin":
			pollCalls++
			var body writeStdinRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode poll body: %v", err)
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "running", Cwd: "/workspace", Output: "ok", Running: true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{"command": "navigate https://example.com", "yield_time_ms": float64(50)}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if execCalls != 1 {
		t.Fatalf("expected exactly one primary exec command, got %d", execCalls)
	}
	if pollCalls != browserAutoPollAttempts {
		t.Fatalf("expected %d browser polls, got %d", browserAutoPollAttempts, pollCalls)
	}
	if _, ok := result.ResultJSON["has_screenshot"]; ok {
		t.Fatalf("did not expect screenshot marker on running result: %#v", result.ResultJSON)
	}
}

func TestBrowser_AutoSessionDoesNotReuseShellSession(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_browser_shell_type_isolation")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	runID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        runID,
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var calls []execCommandRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/exec_command" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		calls = append(calls, body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", ExitCode: intPtr(0)})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	firstShell := exec.Execute(t.Context(), "exec_command", map[string]any{"command": "pwd"}, ctx, "")
	if firstShell.Error != nil {
		t.Fatalf("unexpected shell error: %+v", firstShell.Error)
	}
	firstBrowser := exec.Execute(t.Context(), "browser", map[string]any{"command": "snapshot"}, ctx, "")
	if firstBrowser.Error != nil {
		t.Fatalf("unexpected browser error: %+v", firstBrowser.Error)
	}
	secondBrowser := exec.Execute(t.Context(), "browser", map[string]any{"command": "console"}, ctx, "")
	if secondBrowser.Error != nil {
		t.Fatalf("unexpected second browser error: %+v", secondBrowser.Error)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if calls[0].Tier != "pro" {
		t.Fatalf("expected shell pro tier, got %q", calls[0].Tier)
	}
	if calls[1].Tier != "browser" || calls[2].Tier != "browser" {
		t.Fatalf("expected browser tier calls, got %#v", calls)
	}
	if calls[0].SessionID == calls[1].SessionID {
		t.Fatalf("browser session reused shell session: %q", calls[1].SessionID)
	}
	if calls[1].SessionID != calls[2].SessionID {
		t.Fatalf("expected browser auto reuse, got %q and %q", calls[1].SessionID, calls[2].SessionID)
	}
	if firstBrowser.ResultJSON["resolved_via"] != "new_session" {
		t.Fatalf("unexpected first browser resolution: %v", firstBrowser.ResultJSON["resolved_via"])
	}
	if secondBrowser.ResultJSON["resolved_via"] != "run_default" {
		t.Fatalf("unexpected second browser resolution: %v", secondBrowser.ResultJSON["resolved_via"])
	}
}

func TestBrowser_ExplicitSessionRefCreatesWhenMissing(t *testing.T) {
	orgID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var seen execCommandRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: seen.SessionID, Status: "idle", Cwd: "/workspace", ExitCode: intPtr(0)})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{
		"session_ref": "brref_manual",
		"command":     "snapshot",
	}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if seen.SessionID != "brref_manual" {
		t.Fatalf("expected explicit session ref, got %q", seen.SessionID)
	}
	if result.ResultJSON["resolved_via"] != "explicit_new" {
		t.Fatalf("unexpected resolved_via: %v", result.ResultJSON["resolved_via"])
	}
}

func TestBrowser_AutoFallsBackAfterDisconnectedThreadDefault(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_browser_disconnected_fallback")
	pool, err := pgxpool.New(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	threadID := uuid.New()
	bindingKey := "thread:" + threadID.String()
	liveSessionID := "brref_live_old"
	repo := data.ShellSessionsRepository{}
	if err := repo.Upsert(t.Context(), pool, data.ShellSessionRecord{
		SessionRef:        "brref_old",
		SessionType:       data.ShellSessionTypeBrowser,
		OrgID:             orgID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      "wsref_test",
		ThreadID:          &threadID,
		ShareScope:        data.ShellShareScopeThread,
		State:             data.ShellSessionStateBusy,
		LiveSessionID:     &liveSessionID,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{},
	}); err != nil {
		t.Fatalf("seed browser session: %v", err)
	}

	var sessionIDs []string
	var statuses []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		sessionIDs = append(sessionIDs, body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		if body.SessionID == "brref_old" {
			statuses = append(statuses, http.StatusInternalServerError)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"code": "sandbox.shell_error", "message": "prepare environment: connect to agent: dial tcp 172.24.0.4:8080: connect: no route to host"})
			return
		}
		statuses = append(statuses, http.StatusOK)
		json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", Output: "ok", ExitCode: intPtr(0)})
	}))
	defer server.Close()

	exec := NewToolExecutorWithPool(server.URL, "", pool)
	ctx := tools.ExecutionContext{
		RunID:        uuid.New(),
		OrgID:        &orgID,
		ThreadID:     &threadID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	result := exec.Execute(t.Context(), "browser", map[string]any{"command": "snapshot", "yield_time_ms": float64(1500)}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if len(sessionIDs) != 2 {
		t.Fatalf("expected 2 browser requests, got %d", len(sessionIDs))
	}
	if sessionIDs[0] != "brref_old" {
		t.Fatalf("expected first stale browser session, got %s", sessionIDs[0])
	}
	if sessionIDs[1] == "brref_old" {
		t.Fatalf("expected fallback browser session, got %#v", sessionIDs)
	}
	stored, err := repo.GetBySessionRefAndType(t.Context(), pool, orgID, "brref_old", data.ShellSessionTypeBrowser)
	if err != nil {
		t.Fatalf("reload stale browser session: %v", err)
	}
	if stored.LiveSessionID != nil {
		t.Fatalf("expected stale browser live_session_id cleared, got %#v", stored.LiveSessionID)
	}
}

func TestBrowser_RetriesAfterSessionBusy(t *testing.T) {
	orgID := uuid.New()
	runID := uuid.New()
	ctx := tools.ExecutionContext{
		RunID:        runID,
		OrgID:        &orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
	}
	var paths []string
	var pollSeen writeStdinRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/exec_command":
			var body execCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode exec body: %v", err)
			}
			if len(paths) == 1 {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"code": "shell.session_busy", "message": "shell session is busy"})
				return
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: body.SessionID, Status: "idle", Cwd: "/workspace", Output: "- document", ExitCode: intPtr(0)})
		case "/v1/write_stdin":
			if err := json.NewDecoder(r.Body).Decode(&pollSeen); err != nil {
				t.Fatalf("decode poll body: %v", err)
			}
			json.NewEncoder(w).Encode(execSessionResponse{SessionID: pollSeen.SessionID, Status: "idle", Cwd: "/workspace", Output: "previous command done", ExitCode: intPtr(0)})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(t.Context(), "browser", map[string]any{"command": "snapshot", "yield_time_ms": float64(5000)}, ctx, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if !reflect.DeepEqual(paths, []string{"/v1/exec_command", "/v1/write_stdin", "/v1/exec_command"}) {
		t.Fatalf("unexpected request sequence: %#v", paths)
	}
	if pollSeen.YieldTimeMs != 5000 {
		t.Fatalf("expected poll yield_time_ms=5000, got %d", pollSeen.YieldTimeMs)
	}
	if got := result.ResultJSON["output"]; got != "- document" {
		t.Fatalf("expected retried snapshot output, got %#v", got)
	}
}
