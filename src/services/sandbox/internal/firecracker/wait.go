package firecracker

import (
	"context"
	"fmt"
	"os"
	"time"
)

// WaitForSocket 轮询等待 Unix domain socket 文件出现。
func WaitForSocket(ctx context.Context, socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not ready within %s", socketPath, timeout)
}
