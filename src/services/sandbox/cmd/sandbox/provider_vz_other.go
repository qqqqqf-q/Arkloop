//go:build !darwin || !cgo

package main

import (
	"fmt"

	"arkloop/services/sandbox/internal/app"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

func buildVzPool(_ app.Config, _ *logging.JSONLogger) (session.VMPool, error) {
	return nil, fmt.Errorf("vz provider requires macOS (darwin)")
}
