//go:build desktop

package toolprovider

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"
	sharedencryption "arkloop/services/shared/encryption"

	"github.com/google/uuid"
)

func TestLoadDesktopActiveToolProvidersPlatformRow(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "loadtp.db"))
	if err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlitePool.Close()
	})
	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	userID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	if _, err := db.Exec(ctx, `
		INSERT INTO users (id, username, email, status)
		VALUES ($1, 'u', 'u@test', 'active')
		ON CONFLICT (id) DO NOTHING`, userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO accounts (id, slug, name, type, owner_user_id)
		VALUES ($1, 'a', 'A', 'personal', $2)
		ON CONFLICT (id) DO NOTHING`, accountID, userID,
	); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	cfg := map[string]any{
		"command":                 []string{"mycli", "sub"},
		"extra_args":              []string{"--x"},
		"delegate_model_selector": "cred^m1",
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO tool_provider_configs (
			account_id, owner_kind, group_name, provider_name, is_active, config_json
		) VALUES ($1, 'platform', 'acp', 'acp.opencode', 1, $2)`,
		accountID.String(), string(cfgBytes),
	); err != nil {
		t.Fatalf("insert tool_provider_configs: %v", err)
	}

	platform, err := LoadDesktopActiveToolProviders(ctx, db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(platform) != 1 {
		t.Fatalf("expected 1 platform row, got %d", len(platform))
	}
	c := platform[0]
	if c.GroupName != "acp" || c.ProviderName != "acp.opencode" {
		t.Fatalf("unexpected row: %+v", c)
	}
	raw, err := json.Marshal(c.ConfigJSON)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["delegate_model_selector"] != "cred^m1" {
		t.Fatalf("delegate_model_selector: %v", out["delegate_model_selector"])
	}
	rt := ToRuntimeProviderConfig(c)
	if rt.GroupName != "acp" || rt.ProviderName != "acp.opencode" {
		t.Fatalf("runtime: %+v", rt)
	}
	if rt.ConfigJSON == nil {
		t.Fatal("expected ConfigJSON")
	}
}

func TestLoadDesktopActiveToolProvidersDecryptsWithEncryptionFile(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "encryption.key"), []byte(hex.EncodeToString(key)), 0o600); err != nil {
		t.Fatalf("write encryption key: %v", err)
	}
	ring, err := sharedencryption.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "loadtp-secret.db"))
	if err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlitePool.Close()
	})
	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000012")
	if _, err := db.Exec(ctx, `
		INSERT INTO accounts (id, slug, name, type)
		VALUES ($1, 'platform-account', 'Platform Account', 'personal')
		ON CONFLICT (id) DO NOTHING`, accountID,
	); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	encrypted, ver, err := ring.Encrypt([]byte("minimax-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	secretID := uuid.New()
	if _, err := db.Exec(ctx, `
		INSERT INTO secrets (id, account_id, owner_kind, name, encrypted_value, key_version)
		VALUES ($1, $2, 'platform', 'tool_provider:image', $3, $4)`,
		secretID, accountID, encrypted, ver,
	); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO tool_provider_configs (
			account_id, owner_kind, group_name, provider_name, is_active, secret_id
		) VALUES ($1, 'platform', 'image_understanding', 'image_understanding.minimax', 1, $2)`,
		accountID, secretID,
	); err != nil {
		t.Fatalf("seed tool provider: %v", err)
	}

	platform, err := LoadDesktopActiveToolProviders(ctx, db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(platform) != 1 {
		t.Fatalf("expected 1 platform row, got %d", len(platform))
	}
	if platform[0].APIKeyValue == nil || *platform[0].APIKeyValue != "minimax-secret" {
		t.Fatalf("unexpected api key value: %#v", platform[0].APIKeyValue)
	}

	_, err = desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		t.Fatalf("desktop key ring should load: %v", err)
	}
}
