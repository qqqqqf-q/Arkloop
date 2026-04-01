//go:build desktop && windows

package pipeline

import (
	"context"
	"fmt"
	"os"
	"time"
)

func lockDesktopFile(ctx context.Context, f *os.File) (func(), error) {
	for {
		if err := tryWindowsLock(f); err == nil {
			return func() {
				_ = unlockWindowsFile(f)
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire file lock: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
