//go:build desktop

package app

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqlitepgx"
)

func TestDesktopSubAgentSchemaAvailable(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitepgx.Open(filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if desktopSubAgentSchemaAvailable(ctx, db) {
		t.Fatal("expected sub-agent schema to be absent")
	}

	for _, stmt := range []string{
		`CREATE TABLE sub_agents (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sub_agent_events (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sub_agent_pending_inputs (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sub_agent_context_snapshots (id TEXT PRIMARY KEY)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	if !desktopSubAgentSchemaAvailable(ctx, db) {
		t.Fatal("expected sub-agent schema to be detected")
	}
}
