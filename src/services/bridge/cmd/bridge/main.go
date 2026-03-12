package main

import (
	"context"
	"os"

	"arkloop/services/bridge/internal/app"
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

	logger := app.NewJSONLogger("bridge", os.Stdout)
	application, err := app.NewApplication(cfg, logger)
	if err != nil {
		return err
	}
	return application.Run(context.Background())
}
