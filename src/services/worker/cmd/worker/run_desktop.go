//go:build desktop

package main

import (
	"context"
	"os/signal"
	"syscall"

	"arkloop/services/worker/internal/desktoprun"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return desktoprun.RunDesktop(ctx)
}
