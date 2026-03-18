//go:build desktop

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

func TestDesktopChannelEndpointsReturnEmptyLists(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	channelsRepo, err := data.NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("new channels repo: %v", err)
	}
	identitiesRepo, err := data.NewChannelIdentitiesRepository(pool)
	if err != nil {
		t.Fatalf("new identities repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-test-secret", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credentialRepo,
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
		Logger:                observability.NewJSONLogger("api", io.Discard),
		Pool:                  pool,
		AuthService:           authService,
		AccountMembershipRepo: membershipRepo,
		ChannelsRepo:          channelsRepo,
		ChannelIdentitiesRepo: identitiesRepo,
		ProjectRepo:           projectRepo,
	})

	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "channels", path: "/v1/channels"},
		{name: "channel identities", path: "/v1/me/channel-identities"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(nethttp.MethodGet, testCase.path, nil)
			req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != nethttp.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}

			var body []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("expected empty list, got %s", rec.Body.String())
			}
		})
	}
}

func TestDesktopCreateChannelRepairsLegacySecretsSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data.db")

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, dbPath)
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	for _, stmt := range []string{
		`PRAGMA foreign_keys = OFF`,
		`DROP INDEX IF EXISTS secrets_platform_name_idx`,
		`DROP INDEX IF EXISTS secrets_user_name_idx`,
		`ALTER TABLE secrets RENAME TO secrets_aligned_backup`,
		`CREATE TABLE secrets (
			id              TEXT PRIMARY KEY,
			account_id      TEXT NOT NULL,
			name            TEXT NOT NULL,
			encrypted_value TEXT NOT NULL,
			key_version     INTEGER NOT NULL DEFAULT 1,
			created_at      TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(account_id, name)
		)`,
		`DROP TABLE channels`,
		`CREATE TABLE channels (
			id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			channel_type   TEXT NOT NULL,
			persona_id     TEXT REFERENCES personas(id) ON DELETE SET NULL,
			credentials_id TEXT REFERENCES secrets(id),
			webhook_secret TEXT,
			webhook_url    TEXT,
			is_active      INTEGER NOT NULL DEFAULT 0,
			config_json    TEXT NOT NULL DEFAULT '{}',
			created_at     TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE (account_id, channel_type)
		)`,
		`DROP TABLE secrets_aligned_backup`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := sqlitePool.Exec(ctx, stmt); err != nil {
			t.Fatalf("downgrade secrets schema: %v", err)
		}
	}

	if err := sqlitePool.Close(); err != nil {
		t.Fatalf("close sqlite before reopen: %v", err)
	}

	repairedSQLitePool, err := sqliteadapter.AutoMigrate(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen auto migrate sqlite: %v", err)
	}
	defer repairedSQLitePool.Close()

	repairedPool := sqlitepgx.New(repairedSQLitePool.Unwrap())
	handler := newDesktopChannelHandler(t, repairedPool)

	body, err := json.Marshal(map[string]any{
		"channel_type": "telegram",
		"bot_token":    "desktop-bot-token",
		"config_json": map[string]any{
			"allowed_user_ids": []string{"12345"},
		},
	})
	if err != nil {
		t.Fatalf("marshal create channel body: %v", err)
	}

	req := httptest.NewRequest(nethttp.MethodPost, "/v1/channels", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created["channel_type"] != "telegram" {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}

	listReq := httptest.NewRequest(nethttp.MethodGet, "/v1/channels", nil)
	listReq.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	listRec := httptest.NewRecorder()

	handler.ServeHTTP(listRec, listReq)

	if listRec.Code != nethttp.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}

	var channels []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &channels); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel after create, got %s", listRec.Body.String())
	}
}

func newDesktopChannelHandler(t *testing.T, pool data.DB) nethttp.Handler {
	t.Helper()

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
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	channelsRepo, err := data.NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("new channels repo: %v", err)
	}
	identitiesRepo, err := data.NewChannelIdentitiesRepository(pool)
	if err != nil {
		t.Fatalf("new identities repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	keyRing, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, keyRing)
	if err != nil {
		t.Fatalf("new secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-test-secret", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credentialRepo,
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

	return NewHandler(HandlerConfig{
		Logger:                observability.NewJSONLogger("api", io.Discard),
		Pool:                  pool,
		AuthService:           authService,
		AccountMembershipRepo: membershipRepo,
		ChannelsRepo:          channelsRepo,
		ChannelIdentitiesRepo: identitiesRepo,
		ProjectRepo:           projectRepo,
		SecretsRepo:           secretsRepo,
	})
}
