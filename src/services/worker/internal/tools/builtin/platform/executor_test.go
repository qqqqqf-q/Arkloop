package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arkloop/services/worker/internal/tools"
)

func newTestExecutor(handler http.Handler) *Executor {
	srv := httptest.NewServer(handler)
	tp := NewTokenProvider([]byte("test-secret-at-least-32-bytes-long!!"), "u", "a", "platform_admin", 15*time.Minute)
	return &Executor{
		http:    srv.Client(),
		apiBase: srv.URL,
		tp:      tp,
	}
}

func exec(e *Executor, action string, params map[string]any) tools.ExecutionResult {
	args := map[string]any{"action": action}
	if params != nil {
		args["params"] = params
	}
	return e.Execute(context.Background(), "platform_manage", args, tools.ExecutionContext{}, "")
}

// --- action routing ---

func TestExecutor_MissingAction(t *testing.T) {
	e := newTestExecutor(http.NotFoundHandler())
	result := e.Execute(context.Background(), "platform_manage", map[string]any{}, tools.ExecutionContext{}, "")
	if result.Error == nil || result.Error.ErrorClass != errArgsInvalid {
		t.Fatal("expected args_invalid for missing action")
	}
}

func TestExecutor_UnknownAction(t *testing.T) {
	e := newTestExecutor(http.NotFoundHandler())
	result := exec(e, "nonexistent_action", nil)
	if result.Error == nil || result.Error.ErrorClass != errArgsInvalid {
		t.Fatal("expected args_invalid for unknown action")
	}
}

func TestExecutor_GetSettings(t *testing.T) {
	var capturedPath string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "test"})
	}))
	result := exec(e, "get_settings", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if capturedPath != "/v1/admin/platform-settings" {
		t.Fatalf("expected /v1/admin/platform-settings, got %s", capturedPath)
	}
}

func TestExecutor_ListProviders(t *testing.T) {
	var capturedPath, capturedMethod string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{map[string]any{"id": "p1"}})
	}))
	result := exec(e, "list_providers", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if capturedPath != "/v1/llm-providers" {
		t.Fatalf("expected /v1/llm-providers, got %s", capturedPath)
	}
	if capturedMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", capturedMethod)
	}
}

func TestExecutor_DeleteProvider_ValidUUID(t *testing.T) {
	var capturedPath, capturedMethod string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	result := exec(e, "delete_provider", map[string]any{"id": "550e8400-e29b-41d4-a716-446655440000"})
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if capturedMethod != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", capturedMethod)
	}
	if capturedPath != "/v1/llm-providers/550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("wrong path: %s", capturedPath)
	}
}

// --- UUID validation (path traversal prevention) ---

func TestExecutor_RequireGet_InvalidID(t *testing.T) {
	e := newTestExecutor(http.NotFoundHandler())
	result := exec(e, "get_agent", map[string]any{"id": "../admin/platform-settings"})
	if result.Error == nil || result.Error.ErrorClass != errArgsInvalid {
		t.Fatal("expected args_invalid for path traversal id")
	}
	if result.Error.Message != "id must be a valid UUID" {
		t.Fatalf("wrong message: %s", result.Error.Message)
	}
}

func TestExecutor_RequireDelete_InvalidID(t *testing.T) {
	e := newTestExecutor(http.NotFoundHandler())
	result := exec(e, "delete_provider", map[string]any{"id": "not-a-uuid"})
	if result.Error == nil || result.Error.ErrorClass != errArgsInvalid {
		t.Fatal("expected args_invalid for non-uuid id")
	}
}

func TestExecutor_RequireGet_EmptyID(t *testing.T) {
	e := newTestExecutor(http.NotFoundHandler())
	result := exec(e, "get_agent", map[string]any{})
	if result.Error == nil || result.Error.Message != "id is required" {
		t.Fatal("expected id is required error")
	}
}

// --- params flattening ---

func TestExecutor_ParamsFlattening_NoMutation(t *testing.T) {
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	innerParams := map[string]any{"key": "site_name", "value": "Test"}
	originalArgs := map[string]any{
		"action": "set_setting",
		"params": innerParams,
	}
	e.Execute(context.Background(), "platform_manage", originalArgs, tools.ExecutionContext{}, "")

	if _, exists := innerParams["action"]; exists {
		t.Fatal("params map was mutated: 'action' key leaked into caller's params")
	}
}

// --- HTTP error handling ---

func TestExecutor_Unauthorized(t *testing.T) {
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	result := exec(e, "get_settings", nil)
	if result.Error == nil || result.Error.ErrorClass != errUnauthorized {
		t.Fatal("expected unauthorized error")
	}
}

func TestExecutor_ServerError(t *testing.T) {
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	result := exec(e, "list_providers", nil)
	if result.Error == nil || result.Error.ErrorClass != errHTTP {
		t.Fatal("expected http_error for 500")
	}
}

func TestExecutor_NoContent(t *testing.T) {
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	result := exec(e, "delete_provider", map[string]any{"id": "550e8400-e29b-41d4-a716-446655440000"})
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.ResultJSON["status"] != "ok" {
		t.Fatalf("expected status=ok for 204, got %v", result.ResultJSON["status"])
	}
}

func TestExecutor_ArrayResponse(t *testing.T) {
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{"a", "b"})
	}))
	result := exec(e, "list_providers", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	items, ok := result.ResultJSON["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected items array with 2 elements, got %v", result.ResultJSON)
	}
}

// --- Authorization header ---

func TestExecutor_SetsAuthHeader(t *testing.T) {
	var capturedAuth string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	exec(e, "get_settings", nil)
	if capturedAuth == "" || capturedAuth[:7] != "Bearer " {
		t.Fatalf("expected Bearer token, got: %s", capturedAuth)
	}
}

// --- Action routing coverage (sample of each category) ---

func TestExecutor_ListAgents(t *testing.T) {
	var path string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	exec(e, "list_agents", nil)
	if path != "/v1/personas" {
		t.Fatalf("expected /v1/personas, got %s", path)
	}
}

func TestExecutor_ListSkills(t *testing.T) {
	var path string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	exec(e, "list_skills", nil)
	if path != "/v1/skill-packages" {
		t.Fatalf("expected /v1/skill-packages, got %s", path)
	}
}

func TestExecutor_ListIPRules(t *testing.T) {
	var path string
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	exec(e, "list_ip_rules", nil)
	if path != "/v1/ip-rules" {
		t.Fatalf("expected /v1/ip-rules, got %s", path)
	}
}

// --- DurationMs sanity ---

func TestExecutor_DurationMsPositive(t *testing.T) {
	e := newTestExecutor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	result := exec(e, "get_settings", nil)
	if result.DurationMs < 0 {
		t.Fatalf("expected non-negative duration, got %d", result.DurationMs)
	}
}

// --- bridge not configured ---

func TestExecutor_BridgeNotConfigured(t *testing.T) {
	e := newTestExecutor(http.NotFoundHandler())
	e.bridgeBase = ""

	for _, action := range []string{"get_status", "list_modules", "install_module", "trigger_update"} {
		params := map[string]any{}
		if action == "install_module" {
			params["name"] = "redis"
		}
		result := exec(e, action, params)
		if result.Error != nil {
			t.Fatalf("%s: expected degraded result, got error: %v", action, result.Error)
		}
		if result.ResultJSON["status"] != "degraded" {
			t.Fatalf("%s: expected status=degraded, got %v", action, result.ResultJSON["status"])
		}
	}
}
