package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

type Application struct {
	config Config
	logger *slog.Logger
}

func NewApplication(config Config, logger *slog.Logger) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	return &Application{
		config: config,
		logger: logger,
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case <-ctx.Done():
		return nil
	case <-signals:
		return nil
	}
}
