//go:build desktop

package sqliteadapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDumpSchema(t *testing.T) {
	outPath := os.Getenv("SCHEMA_DUMP_PATH")
	if outPath == "" {
		t.Skip("SCHEMA_DUMP_PATH not set")
	}

	pool := migratedTestDB(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx,
		`SELECT type, name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type, name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var out strings.Builder
	out.WriteString("-- SQLite schema snapshot\n")
	out.WriteString("-- Auto-generated from migration-to-latest on in-memory database.\n")
	out.WriteString("-- Do NOT edit manually. Regenerate after adding migrations.\n\n")

	currentType := ""
	for rows.Next() {
		var objType, name, sqlStr string
		if err := rows.Scan(&objType, &name, &sqlStr); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "goose_db_version" || strings.HasPrefix(name, "sqlite_") || name == "_sequences" {
			continue
		}
		if objType != currentType {
			if currentType != "" {
				out.WriteString("\n")
			}
			out.WriteString(fmt.Sprintf("-- %ss\n\n", strings.ToUpper(objType)))
			currentType = objType
		}
		out.WriteString(sqlStr)
		out.WriteString(";\n\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(out.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("Schema written to %s (%d bytes)", outPath, out.Len())
}
