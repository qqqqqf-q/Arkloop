package bridgehttp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/bridge/internal/audit"
	"arkloop/services/bridge/internal/docker"
	"arkloop/services/bridge/internal/module"
)

type noopLogger struct{}

func (n *noopLogger) Info(msg string, extra map[string]any)  {}
func (n *noopLogger) Error(msg string, extra map[string]any) {}

// findRepoRoot walks up from cwd until it finds a directory containing ".git".
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (.git)")
		}
		dir = parent
	}
}

func newTestHandler(t *testing.T) (*Handler, *http.ServeMux) {
	t.Helper()

	yamlPath := filepath.Join(findRepoRoot(t), "install", "modules.yaml")
	reg, err := module.LoadRegistry(yamlPath)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	compose := docker.NewCompose("/tmp/nonexistent-bridge-test-project", &noopLogger{})
	operations := docker.NewOperationStore()
	auditLog := audit.NewLogger(io.Discard)

	handler := NewHandler(reg, compose, operations, auditLog, &noopLogger{})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return handler, mux
}

func TestPlatformDetectEndpoint(t *testing.T) {
	_, mux := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/platform/detect", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	for _, field := range []string{"os", "docker_available", "kvm_available"} {
		if _, ok := body[field]; !ok {
			t.Errorf("expected field %q in response", field)
		}
	}
}

func TestListModulesEndpoint(t *testing.T) {
	_, mux := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/modules", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var modules []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&modules); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	if len(modules) == 0 {
		t.Fatal("expected non-empty modules array")
	}

	requiredFields := []string{"id", "name", "description", "category", "status", "capabilities", "depends_on", "mutually_exclusive"}
	for i, m := range modules {
		for _, field := range requiredFields {
			if _, ok := m[field]; !ok {
				t.Errorf("module[%d] missing field %q", i, field)
			}
		}
	}
}

func TestGetModuleEndpoint(t *testing.T) {
	_, mux := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/modules/openviking", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	if body["id"] != "openviking" {
		t.Errorf("id = %v, want %q", body["id"], "openviking")
	}
	if body["name"] != "OpenViking" {
		t.Errorf("name = %v, want %q", body["name"], "OpenViking")
	}
}

func TestGetModuleNotFound(t *testing.T) {
	_, mux := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/modules/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPerformActionInvalidBody(t *testing.T) {
	_, mux := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/modules/openviking/actions", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPerformActionUnknownModule(t *testing.T) {
	_, mux := newTestHandler(t)

	body := `{"action":"install"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/modules/nonexistent/actions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
