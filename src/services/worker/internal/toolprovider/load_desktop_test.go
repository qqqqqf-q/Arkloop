//go:build desktop

package toolprovider

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

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
		"command":                []string{"mycli", "sub"},
		"extra_args":             []string{"--x"},
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

	platform, user, err := LoadDesktopActiveToolProviders(ctx, db, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(user) != 0 {
		t.Fatalf("expected no user rows, got %d", len(user))
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
