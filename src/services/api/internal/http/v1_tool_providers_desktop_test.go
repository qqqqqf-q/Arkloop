//go:build desktop

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/http/catalogapi"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"
)

type desktopTPList struct {
	Groups []struct {
		GroupName string `json:"group_name"`
		Providers []struct {
			ProviderName    string          `json:"provider_name"`
			IsActive        bool            `json:"is_active"`
			RuntimeState    string          `json:"runtime_state"`
			RuntimeStatus   string          `json:"runtime_status"`
			RuntimeSource   string          `json:"runtime_source"`
			ConfigStatus    string          `json:"config_status"`
			ConfigJSON      json.RawMessage `json:"config_json,omitempty"`
			RequiresAPIKey  bool            `json:"requires_api_key"`
			RequiresBaseURL bool            `json:"requires_base_url"`
		} `json:"providers"`
	} `json:"groups"`
}

func TestDesktopToolProvidersListActivateAndConfigACP(t *testing.T) {
	prevProbe := catalogapiTestSwapDesktopSandboxHealthProbe(func(addr string) bool {
		return addr == ""
	})
	defer prevProbe()

	desktop.SetExecutionMode("local")
	desktop.SetSandboxAddr("")
	desktop.SetMemoryRuntime("")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "tp.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	toolProvidersRepo, err := data.NewToolProviderConfigsRepository(pool)
	if err != nil {
		t.Fatalf("new tool providers repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	ring, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, ring)
	if err != nil {
		t.Fatalf("secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-tp-test-secret-key-32bytes!!", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credRepo,
		membershipRepo,
		passwordHasher,
		tokenService,
		refreshTokenRepo,
		nil,
		projectRepo,
	)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	handler := NewHandler(HandlerConfig{
		Logger:                  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                    pool,
		AuthService:             authService,
		AccountMembershipRepo:   membershipRepo,
		ToolProviderConfigsRepo: toolProvidersRepo,
		SecretsRepo:             secretsRepo,
		ProjectRepo:             projectRepo,
	})

	token := auth.DesktopToken()
	authH := map[string]string{"Authorization": "Bearer " + token}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/tool-providers", nil)
	for k, v := range authH {
		req.Header.Set(k, v)
	}
	handler.ServeHTTP(rec, req)
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}
	var initial desktopTPList
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(initial.Groups) == 0 {
		t.Fatal("expected non-empty groups")
	}

	put := func(path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			rdr = bytes.NewReader(b)
		}
		r := httptest.NewRequest(nethttp.MethodPut, path, rdr)
		r.Header.Set("Content-Type", "application/json")
		for k, v := range authH {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	act := put("/v1/tool-providers/acp/acp.opencode/activate", nil)
	if act.Code != nethttp.StatusNoContent {
		t.Fatalf("activate: %d %s", act.Code, act.Body.String())
	}

	cfgBody := map[string]any{
		"command":    []string{"opencode", "acp"},
		"extra_args": []string{"--experimental-acp"},
	}
	cfgResp := put("/v1/tool-providers/acp/acp.opencode/config", cfgBody)
	if cfgResp.Code != nethttp.StatusNoContent {
		t.Fatalf("config: %d %s", cfgResp.Code, cfgResp.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(nethttp.MethodGet, "/v1/tool-providers", nil)
	for k, v := range authH {
		req2.Header.Set(k, v)
	}
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != nethttp.StatusOK {
		t.Fatalf("list2: %d %s", rec2.Code, rec2.Body.String())
	}
	var after desktopTPList
	if err := json.Unmarshal(rec2.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode list2: %v", err)
	}

	var found bool
	for _, g := range after.Groups {
		if g.GroupName != "acp" {
			continue
		}
		for _, p := range g.Providers {
			if p.ProviderName != "acp.opencode" {
				continue
			}
			found = true
			if !p.IsActive {
				t.Fatal("expected acp.opencode active")
			}
			if p.RuntimeState != "ready" {
				t.Fatalf("expected runtime_state=ready, got %q", p.RuntimeState)
			}
			if p.RuntimeStatus != "available" {
				t.Fatalf("expected runtime_status=available, got %q", p.RuntimeStatus)
			}
			if p.RuntimeSource != "local" {
				t.Fatalf("expected runtime_source=local, got %q", p.RuntimeSource)
			}
			if p.ConfigStatus != "active" {
				t.Fatalf("expected config_status=active, got %q", p.ConfigStatus)
			}
			var parsed map[string]any
			if err := json.Unmarshal(p.ConfigJSON, &parsed); err != nil {
				t.Fatalf("config json: %v", err)
			}
			cmd, ok := parsed["command"].([]any)
			if !ok || len(cmd) != 2 {
				t.Fatalf("command: %#v", parsed["command"])
			}
			if cmd[0] != "opencode" || cmd[1] != "acp" {
				t.Fatalf("command elems: %v", cmd)
			}
			xa, ok := parsed["extra_args"].([]any)
			if !ok || len(xa) != 1 || xa[0] != "--experimental-acp" {
				t.Fatalf("extra_args: %#v", parsed["extra_args"])
			}
		}
	}
	if !found {
		t.Fatal("acp.opencode not in list")
	}
}

func TestDesktopToolProvidersListMemoryLocalRuntime(t *testing.T) {
	prevProbe := catalogapiTestSwapDesktopSandboxHealthProbe(func(addr string) bool {
		return addr == ""
	})
	defer prevProbe()

	desktop.SetExecutionMode("local")
	desktop.SetSandboxAddr("")
	desktop.SetMemoryRuntime("local")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "tp-memory.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	toolProvidersRepo, err := data.NewToolProviderConfigsRepository(pool)
	if err != nil {
		t.Fatalf("new tool providers repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	ring, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, ring)
	if err != nil {
		t.Fatalf("secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-tp-memory-test-secret-32bytes", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credRepo,
		membershipRepo,
		passwordHasher,
		tokenService,
		refreshTokenRepo,
		nil,
		projectRepo,
	)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	handler := NewHandler(HandlerConfig{
		Logger:                  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                    pool,
		AuthService:             authService,
		AccountMembershipRepo:   membershipRepo,
		ToolProviderConfigsRepo: toolProvidersRepo,
		SecretsRepo:             secretsRepo,
		ProjectRepo:             projectRepo,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/tool-providers", nil)
	req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	handler.ServeHTTP(rec, req)
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}

	var payload desktopTPList
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	var found bool
	for _, group := range payload.Groups {
		if group.GroupName != "memory" {
			continue
		}
		for _, provider := range group.Providers {
			if provider.ProviderName != "memory.openviking" {
				continue
			}
			found = true
			if provider.RuntimeStatus != "available" {
				t.Fatalf("expected runtime_status=available, got %q", provider.RuntimeStatus)
			}
			if provider.RuntimeSource != "local" {
				t.Fatalf("expected runtime_source=local, got %q", provider.RuntimeSource)
			}
			if provider.ConfigStatus != "inactive" {
				t.Fatalf("expected config_status=inactive, got %q", provider.ConfigStatus)
			}
		}
	}
	if !found {
		t.Fatal("memory.openviking not in list")
	}
}

func TestDesktopToolProvidersListShowsOnlySelectedSearchProviderRunning(t *testing.T) {
	prevProbe := catalogapiTestSwapDesktopSandboxHealthProbe(func(addr string) bool {
		return addr == ""
	})
	defer prevProbe()

	desktop.SetExecutionMode("local")
	desktop.SetSandboxAddr("")
	desktop.SetMemoryRuntime("")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "tp-search.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	toolProvidersRepo, err := data.NewToolProviderConfigsRepository(pool)
	if err != nil {
		t.Fatalf("new tool providers repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 19)
	}
	ring, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, ring)
	if err != nil {
		t.Fatalf("secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-tp-search-test-secret-32byt", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credRepo,
		membershipRepo,
		passwordHasher,
		tokenService,
		refreshTokenRepo,
		nil,
		projectRepo,
	)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	handler := NewHandler(HandlerConfig{
		Logger:                  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                    pool,
		AuthService:             authService,
		AccountMembershipRepo:   membershipRepo,
		ToolProviderConfigsRepo: toolProvidersRepo,
		SecretsRepo:             secretsRepo,
		ProjectRepo:             projectRepo,
	})

	put := func(path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			rdr = bytes.NewReader(b)
		}
		r := httptest.NewRequest(nethttp.MethodPut, path, rdr)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	act := put("/v1/tool-providers/web_search/web_search.duckduckgo/activate", nil)
	if act.Code != nethttp.StatusNoContent {
		t.Fatalf("activate duckduckgo: %d %s", act.Code, act.Body.String())
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/tool-providers", nil)
	req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	handler.ServeHTTP(rec, req)
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}

	var payload desktopTPList
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	var duckduckgoFound bool
	var tavilyFound bool
	for _, group := range payload.Groups {
		if group.GroupName != "web_search" {
			continue
		}
		for _, provider := range group.Providers {
			switch provider.ProviderName {
			case "web_search.duckduckgo":
				duckduckgoFound = true
				if provider.RuntimeStatus != "available" {
					t.Fatalf("expected duckduckgo available, got %q", provider.RuntimeStatus)
				}
			case "web_search.tavily":
				tavilyFound = true
				if provider.RuntimeStatus != "unavailable" {
					t.Fatalf("expected tavily unavailable, got %q", provider.RuntimeStatus)
				}
			}
		}
	}
	if !duckduckgoFound || !tavilyFound {
		t.Fatalf("expected both duckduckgo and tavily in payload: duck=%v tavily=%v", duckduckgoFound, tavilyFound)
	}
}

func TestDesktopToolProvidersListSeparatesDockerAndFirecrackerRuntime(t *testing.T) {
	prevProbe := catalogapiTestSwapDesktopSandboxHealthProbe(func(addr string) bool {
		return addr == "127.0.0.1:19002"
	})
	defer prevProbe()

	desktop.SetExecutionMode("local")
	desktop.SetSandboxAddr("127.0.0.1:19002")
	desktop.SetMemoryRuntime("")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "tp-sandbox.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	toolProvidersRepo, err := data.NewToolProviderConfigsRepository(pool)
	if err != nil {
		t.Fatalf("new tool providers repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 23)
	}
	ring, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, ring)
	if err != nil {
		t.Fatalf("secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-tp-sandbox-test-secret-32by", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credRepo,
		membershipRepo,
		passwordHasher,
		tokenService,
		refreshTokenRepo,
		nil,
		projectRepo,
	)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	handler := NewHandler(HandlerConfig{
		Logger:                  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                    pool,
		AuthService:             authService,
		AccountMembershipRepo:   membershipRepo,
		ToolProviderConfigsRepo: toolProvidersRepo,
		SecretsRepo:             secretsRepo,
		ProjectRepo:             projectRepo,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/tool-providers", nil)
	req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	handler.ServeHTTP(rec, req)
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}

	var payload desktopTPList
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	var dockerFound bool
	var firecrackerFound bool
	for _, group := range payload.Groups {
		if group.GroupName != "sandbox" {
			continue
		}
		for _, provider := range group.Providers {
			switch provider.ProviderName {
			case "sandbox.docker":
				dockerFound = true
				if provider.RuntimeStatus != "available" {
					t.Fatalf("expected sandbox.docker available, got %q", provider.RuntimeStatus)
				}
			case "sandbox.firecracker":
				firecrackerFound = true
				if provider.RuntimeStatus != "unavailable" {
					t.Fatalf("expected sandbox.firecracker unavailable, got %q", provider.RuntimeStatus)
				}
			}
		}
	}
	if !dockerFound || !firecrackerFound {
		t.Fatalf("expected both sandbox providers in payload: docker=%v firecracker=%v", dockerFound, firecrackerFound)
	}
}

func catalogapiTestSwapDesktopSandboxHealthProbe(
	probe func(addr string) bool,
) func() {
	return catalogapi.SetDesktopSandboxHealthProbeForTest(probe)
}
