//go:build windows

package ax

import (
	"fmt"
	"runtime"
)

func walkOnThread(maxDepth, maxNodes int, timeoutMs float64) WalkResult {
	return WalkResult{Error: fmt.Errorf("ax source not supported on %s", runtime.GOOS)}
}

func idleSeconds() (float64, error) {
	return 0, fmt.Errorf("idle check not available on %s", runtime.GOOS)
}
