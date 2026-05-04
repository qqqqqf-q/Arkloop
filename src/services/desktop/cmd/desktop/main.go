//go:build desktop

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	desktopruntime "arkloop/services/desktop/runtime"
)

func main() {
	if err := run(); err != nil {
		slog.Error("desktop main error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		cancel()
	}()
	return desktopruntime.Run(ctx, desktopruntime.Options{
		Component:    "desktop",
		StartBridge:  true,
		StartSandbox: true,
	})
}
