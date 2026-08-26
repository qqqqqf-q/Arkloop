package fileops

import (
	"context"
	"os"
	"time"
)

// Backend abstracts filesystem operations for file tools.
type Backend interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte) error
	Stat(ctx context.Context, path string) (FileInfo, error)
	Exec(ctx context.Context, command string) (stdout, stderr string, exitCode int, err error)
	NormalizePath(path string) string
}

type FileInfo struct {
	Size    int64
	IsDir   bool
	ModTime time.Time
}

// ResolveBackend 返回以 workDir 为根的本机文件后端。
// sandbox 后端已移除，本机是唯一文件操作路径。
func ResolveBackend(workDir string) Backend {
	return &LocalBackend{WorkDir: resolveWorkDir(workDir)}
}

func resolveWorkDir(workDir string) string {
	if workDir == "" {
		workDir = os.Getenv("ARKLOOP_WORKING_DIR")
	}
	if workDir == "" {
		workDir = os.Getenv("ARKLOOP_LOCAL_SHELL_WORKSPACE")
	}
	if workDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workDir = wd
		}
	}
	return workDir
}
