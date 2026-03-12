package testutil

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

var SafeIdentifierRegex = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func QuoteIdentifier(name string) string {
	if !SafeIdentifierRegex.MatchString(name) {
		panic("illegal identifier")
	}
	return `"` + name + `"`
}

func RequireIntegrationTests(t *testing.T) {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv("ARKLOOP_RUN_INTEGRATION_TESTS"))
	if raw == "" {
		t.Skip("integration tests disabled")
	}
	lower := strings.ToLower(raw)
	if lower == "1" || lower == "true" || lower == "yes" || lower == "on" {
		return
	}
	t.Skip("integration tests disabled")
}

// DropTemporaryDatabase terminates active connections to the given database
// and drops it. Intended for t.Cleanup after integration test setup.
func DropTemporaryDatabase(t *testing.T, adminDSN string, databaseName string) {
	t.Helper()

	adminConn, err := pgx.Connect(context.Background(), adminDSN)
	if err != nil {
		t.Fatalf("connect admin database failed: %v", err)
	}
	defer adminConn.Close(context.Background())

	_, _ = adminConn.Exec(
		context.Background(),
		`SELECT pg_terminate_backend(pid)
		 FROM pg_stat_activity
		 WHERE datname = $1
		   AND pid <> pg_backend_pid()`,
		databaseName,
	)

	if _, err := adminConn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+QuoteIdentifier(databaseName)); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("drop database %s failed: %v\n", databaseName, err))
	}
}
