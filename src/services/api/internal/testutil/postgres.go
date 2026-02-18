package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

var safeIdentifierRegex = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type PostgresDatabase struct {
	DSN      string
	Database string
}

func SetupPostgresDatabase(t *testing.T, prefix string) *PostgresDatabase {
	t.Helper()

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
	defer adminConn.Close(context.Background())

	if _, err := adminConn.Exec(context.Background(), "CREATE DATABASE "+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database failed: %v", err)
	}

	t.Cleanup(func() {
		dropTemporaryDatabase(t, adminURL.String(), databaseName)
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

	t.Skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")
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

func quoteIdentifier(name string) string {
	if !safeIdentifierRegex.MatchString(name) {
		panic("illegal identifier")
	}
	return `"` + name + `"`
}

func dropTemporaryDatabase(t *testing.T, adminDSN string, databaseName string) {
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

	if _, err := adminConn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+quoteIdentifier(databaseName)); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("drop database %s failed: %v\n", databaseName, err))
	}
}
