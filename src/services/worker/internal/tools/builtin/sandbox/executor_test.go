package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestTierFromBudget(t *testing.T) {
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
	if receivedTier != "pro" {
		t.Errorf("expected tier=pro, got %s", receivedTier)
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
