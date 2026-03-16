//go:build desktop

package main

import (
	"context"
	"os/signal"
	"syscall"

	"arkloop/services/api/internal/app"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.RunDesktop(ctx)
}
