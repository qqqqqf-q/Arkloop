//go:build desktop && !windows

package pipeline

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockDesktopFile(_ context.Context, f *os.File) (func(), error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return nil, fmt.Errorf("acquire flock: %w", err)
	}
	return func() {}, nil
}
