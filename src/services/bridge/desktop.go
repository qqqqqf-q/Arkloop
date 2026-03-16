//go:build desktop

package bridge

import (
	"context"
	"fmt"
	"os"

	"arkloop/services/bridge/internal/app"
)

// StartDesktop starts the bridge service in desktop mode.
// It is best-effort: if the bridge fails to initialize (e.g., missing
// modules file or Docker unavailable), it logs the error and returns
// without blocking the rest of the desktop application.
func StartDesktop(ctx context.Context) error {
	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return fmt.Errorf("bridge config: %w", err)
	}

	logger := app.NewJSONLogger("bridge", os.Stderr)
	application, err := app.NewApplication(cfg, logger)
	if err != nil {
		return fmt.Errorf("bridge init: %w", err)
	}

	return application.Run(ctx)
}
