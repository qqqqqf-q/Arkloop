//go:build desktop

package http

import (
	"os"
	"strings"
	"testing"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

func TestMain(m *testing.M) {
	_ = os.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	if strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN")) == "" {
		_ = os.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")
	}
	os.Exit(m.Run())
}
