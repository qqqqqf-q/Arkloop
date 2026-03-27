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
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

type desktopTPList struct {
	Groups []struct {
		GroupName string `json:"group_name"`
		Providers []struct {
			ProviderName    string          `json:"provider_name"`
			IsActive        bool            `json:"is_active"`
			RuntimeState    string          `json:"runtime_state"`
			ConfigJSON      json.RawMessage `json:"config_json,omitempty"`
			RequiresAPIKey  bool            `json:"requires_api_key"`
			RequiresBaseURL bool            `json:"requires_base_url"`
		} `json:"providers"`
	} `json:"groups"`
}

func TestDesktopToolProvidersListActivateAndConfigACP(t *testing.T) {
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
