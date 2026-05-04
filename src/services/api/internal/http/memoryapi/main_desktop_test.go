//go:build desktop

package memoryapi

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN")) == "" {
		_ = os.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")
	}
	os.Exit(m.Run())
}
