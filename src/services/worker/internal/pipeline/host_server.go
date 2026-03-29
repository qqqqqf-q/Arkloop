//go:build !desktop

package pipeline

import (
	"log/slog"
	"os"
)

var hostMode = initHostMode()

func initHostMode() string {
	v := os.Getenv("ARKLOOP_HOST_MODE")
	switch v {
	case "server", "cloud", "":
		if v == "" {
			return "server"
		}
		return v
	default:
		slog.Warn("ARKLOOP_HOST_MODE: unrecognized value, using 'server'", "value", v)
		return "server"
	}
}
