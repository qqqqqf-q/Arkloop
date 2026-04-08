//go:build desktop

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"arkloop/services/worker/internal/desktoprun"
)

func run() error {
	if os.Getenv("ARKLOOP_DESKTOP_ALLOW_SPLIT_PROCESS") != "1" {
		return fmt.Errorf("desktop worker standalone mode is disabled; start the integrated desktop runtime or set ARKLOOP_DESKTOP_ALLOW_SPLIT_PROCESS=1")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return desktoprun.RunDesktop(ctx)
}
