//go:build desktop

package memoryapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

func TestBuildNowledgeSnapshotBlock(t *testing.T) {
	block := buildNowledgeSnapshotBlock([]nowledgeListedMemory{
		{ID: "mem-1", Title: "偏好", Content: "用户偏好中文回复，并且希望答案短一点。"},
	})
	if !strings.Contains(block, "<memory>") {
		t.Fatalf("expected memory block wrapper, got %q", block)
	}
	if !strings.Contains(block, "[偏好] 用户偏好中文回复") {
		t.Fatalf("expected linear fragment line, got %q", block)
	}
}

func TestBuildNowledgeSnapshotHits(t *testing.T) {
	hits := buildNowledgeSnapshotHits([]nowledgeListedMemory{
		{ID: "mem-9", Title: "", Content: "这是一段用于摘要回退的内容。"},
	})
	if len(hits) != 1 {
		t.Fatalf("expected one hit, got %#v", hits)
	}
	if hits[0].URI != "nowledge://memory/mem-9" {
		t.Fatalf("unexpected uri: %#v", hits[0])
	}
	if hits[0].Abstract == "" {
		t.Fatalf("expected non-empty abstract: %#v", hits[0])
	}
	if !hits[0].IsLeaf {
		t.Fatalf("expected leaf hit: %#v", hits[0])
	}
}

func TestResolveNowledgeConfigFallsBackToLocalDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".nowledge-mem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"apiUrl":"http://127.0.0.1:14242","apiKey":"local-key"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &handler{memoryProvider: "nowledge"}
	cfg, err := h.resolveNowledgeConfig()
	if err != nil {
		t.Fatalf("resolveNowledgeConfig: %v", err)
	}
	if cfg.baseURL != "http://127.0.0.1:14242" {
		t.Fatalf("unexpected base url: %#v", cfg)
	}
	if cfg.apiKey != "local-key" {
		t.Fatalf("unexpected api key: %#v", cfg)
	}
}

func TestNowledgeMemoryIDFromURI(t *testing.T) {
	id, err := nowledgeMemoryIDFromURI("nowledge://memory/mem-42")
	if err != nil {
		t.Fatalf("nowledgeMemoryIDFromURI: %v", err)
	}
	if id != "mem-42" {
		t.Fatalf("unexpected id: %q", id)
	}
}

func TestCheckNowledgeSearchTreatsEndpointAvailabilityAsHealthy(t *testing.T) {
	var sawAuth bool
	var sawAgent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memories/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization") == "Bearer test-key"
		sawAgent = strings.HasPrefix(r.Header.Get("X-Arkloop-Agent"), "user_")
		_ = json.NewEncoder(w).Encode(map[string]any{"memories": []map[string]any{}})
	}))
	defer srv.Close()

	h := &handler{}
	ok, err := h.checkNowledgeSearch(context.Background(), "user_test", nowledgeConfig{
		baseURL:        srv.URL,
		apiKey:         "test-key",
		requestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("checkNowledgeSearch: %v", err)
	}
	if !ok {
		t.Fatal("expected search probe to report healthy")
	}
	if !sawAuth || !sawAgent {
		t.Fatalf("expected nowledge headers, auth=%v agent=%v", sawAuth, sawAgent)
	}
}

func TestGetStatusTreatsLocalNowledgeWithoutAPIKeyAsConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.2.3"})
		case "/memories/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"memories": []map[string]any{}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	h := &handler{
		authService:              newTestAuthService(t),
		memoryProvider:           "nowledge",
		nowledgeBaseURL:          srv.URL,
		nowledgeAPIKey:           "",
		nowledgeRequestTimeoutMs: 1000,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/desktop/memory/status", nil)
	req.Header.Set("Authorization", "Bearer "+issueTestAccessToken(t, h.authService))
	rr := httptest.NewRecorder()

	h.getStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d body=%s", rr.Code, rr.Body.String())
	}

	var status memoryRuntimeStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Configured {
		t.Fatalf("expected local nowledge without api key to be configured: %#v", status)
	}
}

func newTestAuthService(t *testing.T) *auth.Service {
	t.Helper()
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	t.Cleanup(func() { sqlitePool.Close() })

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("memory-test-secret", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return authService
}

func issueTestAccessToken(t *testing.T, authService *auth.Service) string {
	t.Helper()
	issued, err := authService.IssueTokenPairForUser(context.Background(), auth.DesktopUserID)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	return issued.AccessToken
}

func TestCheckOpenVikingHealthSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := &handler{ovBaseURL: srv.URL}
	ok, err := h.checkOpenVikingHealth(context.Background())
	if err != nil {
		t.Fatalf("checkOpenVikingHealth: %v", err)
	}
	if !ok {
		t.Fatal("expected openviking health check to pass")
	}
}
