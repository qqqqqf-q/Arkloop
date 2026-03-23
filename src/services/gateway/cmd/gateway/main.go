//go:build !desktop

package main

import (
	"context"
	"log/slog"
	"os"

	"arkloop/services/gateway/internal/app"
	sharedlog "arkloop/services/shared/log"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	// 统一 slog 输出格式
	slog.SetDefault(sharedlog.New(sharedlog.Config{
		Component: "gateway",
		Level:     slog.LevelDebug,
		Output:    os.Stdout,
	}))

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	application, err := app.NewApplication(cfg, slog.Default())
	if err != nil {
		return err
	}
	return application.Run(context.Background())
}
