//go:build !desktop

package main

import (
	"context"
	"log/slog"
	"os"

	"arkloop/services/api/internal/app"
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

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	application, err := app.NewApplication(cfg, logger)
	if err != nil {
		return err
	}
	return application.Run(context.Background())
}
