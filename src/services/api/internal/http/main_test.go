//go:build !desktop

package http

import (
	"os"
	"testing"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

func TestMain(m *testing.M) {
	_ = os.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	os.Exit(m.Run())
}
