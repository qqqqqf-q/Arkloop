package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	sharedtestutil "arkloop/services/shared/testutil"

	"github.com/jackc/pgx/v5"
)

type PostgresDatabase struct {
	DSN      string
	Database string
}

func SetupPostgresDatabase(t *testing.T, prefix string) *PostgresDatabase {
	t.Helper()

	sharedtestutil.RequireIntegrationTests(t)
	baseDSN := lookupDatabaseDSN(t)
	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse database dsn failed: %v", err)
	}
	if parsed.Scheme == "postgresql+asyncpg" {
		parsed.Scheme = "postgresql"
	}

	adminURL := *parsed
	adminURL.Path = "/postgres"

	databaseName := buildDatabaseName(prefix)
	adminConn, err := pgx.Connect(context.Background(), adminURL.String())
	if err != nil {
		t.Fatalf("connect admin database failed: %v", err)
	}
	defer func() { _ = adminConn.Close(context.Background()) }()

	if _, err := adminConn.Exec(context.Background(), "CREATE DATABASE "+sharedtestutil.QuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database failed: %v", err)
	}

	t.Cleanup(func() {
		sharedtestutil.DropTemporaryDatabase(t, adminURL.String(), databaseName)
	})

	dbURL := *parsed
	dbURL.Path = "/" + databaseName

	return &PostgresDatabase{
		DSN:      dbURL.String(),
		Database: databaseName,
	}
}

func lookupDatabaseDSN(t *testing.T) string {
	t.Helper()

	for _, key := range []string{"ARKLOOP_DATABASE_URL", "DATABASE_URL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}

	t.Skip("ARKLOOP_DATABASE_URL (or compatible DATABASE_URL) not set")
	return ""
}

func buildDatabaseName(prefix string) string {
	cleanedPrefix := strings.TrimSpace(prefix)
	if cleanedPrefix == "" {
		cleanedPrefix = "arkloop_api"
	}

	cleanedPrefix = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-' || r == '.':
			return '_'
		default:
			return '_'
		}
	}, cleanedPrefix)

	cleanedPrefix = strings.Trim(cleanedPrefix, "_")
	if cleanedPrefix == "" {
		cleanedPrefix = "arkloop_api"
	}

	suffix := randomHex(8)
	return cleanedPrefix + "_" + suffix
}

func randomHex(nBytes int) string {
	if nBytes <= 0 {
		nBytes = 8
	}
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "deadbeef"
	}
	return hex.EncodeToString(buf)
}
