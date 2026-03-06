package pipeline_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestToolProviderMiddlewareInjectsActiveProvider(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg_tool_provider")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 11)
	}
	keyHex := hex.EncodeToString(keyBytes)
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", keyHex)

	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE tool_provider_configs (
			id uuid PRIMARY KEY,
			org_id uuid NULL,
			scope text NOT NULL DEFAULT 'org',
			group_name text NOT NULL,
			provider_name text NOT NULL,
			is_active boolean NOT NULL DEFAULT false,
			secret_id uuid NULL,
			key_prefix text NULL,
			base_url text NULL,
			config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
			updated_at timestamptz NOT NULL DEFAULT now()
		);
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	orgID := uuid.New()
	secretID := uuid.New()
	apiKey := "tvly-test-key-123456"
	encrypted := encryptGCM(t, keyBytes, apiKey)

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO secrets (id, org_id, scope, encrypted_value, key_version)
		VALUES ($1, $2, 'org', $3, 1)
	`, secretID, orgID, encrypted); err != nil {
		t.Fatalf("insert secret: %v", err)
	}

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO tool_provider_configs (id, org_id, scope, group_name, provider_name, is_active, secret_id, key_prefix, config_json)
		VALUES ($1, $2, 'org', $3, $4, TRUE, $5, $6, '{}'::jsonb)
	`, uuid.New(), orgID, "web_search", "web_search.tavily", secretID, "tvly-test"); err != nil {
		t.Fatalf("insert config: %v", err)
	}

	cache := toolprovider.NewCache(0)
	mw := pipeline.NewToolProviderMiddleware(cache)

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			OrgID:    orgID,
			ThreadID: uuid.New(),
		},
		Pool:          pool,
		ToolExecutors: map[string]tools.Executor{},
	}

	called := false
	err = mw(context.Background(), rc, func(ctx context.Context, rc *pipeline.RunContext) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("middleware error: %v", err)
	}
	if !called {
		t.Fatal("expected next handler called")
	}

	if rc.ActiveToolProviderByGroup == nil || rc.ActiveToolProviderByGroup["web_search"] != "web_search.tavily" {
		t.Fatalf("unexpected active map: %+v", rc.ActiveToolProviderByGroup)
	}
	if rc.ToolExecutors["web_search.tavily"] == nil {
		t.Fatal("expected executor injected for web_search.tavily")
	}
}

func encryptGCM(t *testing.T, key []byte, plaintext string) string {
	t.Helper()

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand nonce: %v", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	buf := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(buf)
}
