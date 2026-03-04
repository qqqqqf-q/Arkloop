package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

func testContext() tools.ExecutionContext {
	return tools.ExecutionContext{
		RunID: uuid.New(),
	}
}

func TestCodeExecute_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/exec" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		// 验证 auth header
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
		if body.SessionID == "" {
			t.Error("session_id should not be empty")
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
		"code_execute",
		map[string]any{"code": "print('hello')"},
		testContext(),
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

func TestShellExecute_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body execRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Language != "shell" {
			t.Errorf("expected language=shell, got %s", body.Language)
		}
		if body.Code != "ls -la" {
			t.Errorf("unexpected code: %s", body.Code)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(execResponse{
			Stdout:     "total 0\n",
			ExitCode:   0,
			DurationMs: 10,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	result := exec.Execute(
		t.Context(),
		"shell_execute",
		map[string]any{"command": "ls -la"},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["stdout"] != "total 0\n" {
		t.Errorf("unexpected stdout: %v", result.ResultJSON["stdout"])
	}
}

func TestCodeExecute_MissingCode(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(
		t.Context(),
		"code_execute",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestShellExecute_MissingCommand(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999", "")
	result := exec.Execute(
		t.Context(),
		"shell_execute",
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
		"code_execute",
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
		"code_execute",
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
		"code_execute",
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
		"code_execute",
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
		"code_execute",
		map[string]any{"code": "1/0"},
		testContext(),
		"",
	)

	// exit_code != 0 应该返回正常结果，让 agent 看到 stderr 调试
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
		"code_execute",
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
		"code_execute",
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
		"code_execute",
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

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL, "")
	// 使用极短的 HTTP client timeout 触发超时
	exec.client.Timeout = 100 * time.Millisecond

	result := exec.Execute(
		t.Context(),
		"code_execute",
		map[string]any{"code": "x=1"},
		testContext(),
		"",
	)
	if result.Error == nil {
		t.Fatal("expected timeout error")
	}
	// 客户端超时可能是 sandbox_timeout 或 sandbox_unavailable
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
	result := exec.Execute(t.Context(), "code_execute", map[string]any{"code": "x=1"}, ctx, "")

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
	result := exec.Execute(t.Context(), "code_execute", map[string]any{"code": "x=1"}, testContext(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
}
