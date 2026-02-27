package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

func testContext() tools.ExecutionContext {
	orgID := uuid.New()
	threadID := uuid.New()
	return tools.ExecutionContext{
		RunID:    uuid.New(),
		OrgID:    &orgID,
		ThreadID: &threadID,
	}
}

func TestNavigate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/navigate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Session-ID") == "" {
			t.Error("missing X-Session-ID")
		}
		if r.Header.Get("X-Org-ID") == "" {
			t.Error("missing X-Org-ID")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["url"] != "https://example.com" {
			t.Errorf("unexpected url: %v", body["url"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"page_url":           "https://example.com",
			"page_title":         "Example",
			"screenshot_url":     "https://minio/screenshots/1.png",
			"content_text":       "hello world",
			"accessibility_tree": "[document]",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "https://example.com"},
		testContext(),
		"",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["page_title"] != "Example" {
		t.Errorf("unexpected page_title: %v", result.ResultJSON["page_title"])
	}
}

func TestNavigate_MissingURL(t *testing.T) {
	exec := NewToolExecutor("http://localhost:9999")
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid error, got: %+v", result.Error)
	}
}

func TestInteract_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/interact" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"page_url":           "https://example.com/dashboard",
			"page_title":         "Dashboard",
			"screenshot_url":     "https://minio/screenshots/2.png",
			"content_text":       "dashboard content",
			"accessibility_tree": "[document]",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_interact",
		map[string]any{"action": "click", "selector": "#login-btn"},
		testContext(),
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["page_title"] != "Dashboard" {
		t.Errorf("unexpected page_title: %v", result.ResultJSON["page_title"])
	}
}

func TestExtract_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":    "extracted text",
			"word_count": 2,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_extract",
		map[string]any{"mode": "text"},
		testContext(),
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["content"] != "extracted text" {
		t.Errorf("unexpected content: %v", result.ResultJSON["content"])
	}
}

func TestScreenshot_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"screenshot_url": "https://minio/screenshots/3.png",
			"width":          1280,
			"height":         720,
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_screenshot",
		map[string]any{"full_page": true},
		testContext(),
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
}

func TestSessionClose_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_session_close",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["closed"] != true {
		t.Errorf("expected closed=true, got: %v", result.ResultJSON)
	}
}

func TestHTTPError_BrowserError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "browser_error",
			"message": "page crashed",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "https://example.com"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorBrowserError {
		t.Fatalf("expected browser_error, got: %+v", result.Error)
	}
	if result.Error.Message != "page crashed" {
		t.Errorf("unexpected message: %s", result.Error.Message)
	}
}

func TestHTTPError_NetworkBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "network_blocked",
			"message": "internal network access denied",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "http://redis:6379"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorNetworkBlocked {
		t.Fatalf("expected network_blocked, got: %+v", result.Error)
	}
}

func TestHTTPError_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "timeout",
			"message": "navigation timed out",
		})
	}))
	defer server.Close()

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "https://slow-site.com"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorTimeout {
		t.Fatalf("expected timeout, got: %+v", result.Error)
	}
}

func TestNotConfigured(t *testing.T) {
	exec := NewToolExecutor("")
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "https://example.com"},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorNotConfigured {
		t.Fatalf("expected not_configured, got: %+v", result.Error)
	}
}

func TestMissingContext(t *testing.T) {
	exec := NewToolExecutor("http://localhost:3000")
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "https://example.com"},
		tools.ExecutionContext{RunID: uuid.New()},
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestTimeoutPropagation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	timeoutMs := 100
	ctx := testContext()
	ctx.TimeoutMs = &timeoutMs

	exec := NewToolExecutor(server.URL)
	result := exec.Execute(
		t.Context(),
		"browser_navigate",
		map[string]any{"url": "https://example.com"},
		ctx,
		"",
	)
	if result.Error == nil {
		t.Fatal("expected timeout error")
	}
	if result.Error.ErrorClass != errorTimeout && result.Error.ErrorClass != errorBrowserError {
		t.Fatalf("expected timeout or browser_error, got: %s", result.Error.ErrorClass)
	}
}

func TestUnknownTool(t *testing.T) {
	exec := NewToolExecutor("http://localhost:3000")
	result := exec.Execute(
		t.Context(),
		"browser_unknown",
		map[string]any{},
		testContext(),
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}
