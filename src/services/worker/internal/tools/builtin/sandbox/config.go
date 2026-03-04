package sandbox

import (
	"os"
	"strings"
)

const (
	sandboxBaseURLEnv   = "ARKLOOP_SANDBOX_BASE_URL"
	sandboxAuthTokenEnv = "ARKLOOP_SANDBOX_AUTH_TOKEN"
)

func BaseURLFromEnv() string {
	raw := strings.TrimSpace(os.Getenv(sandboxBaseURLEnv))
	return strings.TrimRight(raw, "/")
}

func AuthTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv(sandboxAuthTokenEnv))
}
