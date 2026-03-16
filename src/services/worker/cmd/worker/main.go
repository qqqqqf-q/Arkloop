package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func lookupDatabaseDSN() string {
for _, key := range []string{"ARKLOOP_DATABASE_URL", "DATABASE_URL"} {
value := strings.TrimSpace(os.Getenv(key))
if value != "" {
return value
}
}
return ""
}

func lookupDirectDatabaseDSN() string {
return strings.TrimSpace(os.Getenv("ARKLOOP_DATABASE_DIRECT_URL"))
}

func lookupRedisURL() string {
return strings.TrimSpace(os.Getenv("ARKLOOP_REDIS_URL"))
}

func normalizePostgresDSN(raw string) string {
parsed, err := url.Parse(raw)
if err != nil {
return raw
}
if parsed.Scheme == "postgresql+asyncpg" {
parsed.Scheme = "postgresql"
return parsed.String()
}
if strings.HasPrefix(parsed.Scheme, "postgresql") || parsed.Scheme == "postgres" {
return parsed.String()
}
_, _ = os.Stderr.WriteString(fmt.Sprintf("warning: unknown postgres scheme %q, keep original dsn\n", parsed.Scheme))
return raw
}
