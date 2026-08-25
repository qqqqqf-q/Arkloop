//go:build !windows

package pipeline

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func lockDesktopFile(ctx context.Context, f *os.File) (func(), error) {
	for {
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
			return func() {
				_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire flock: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
