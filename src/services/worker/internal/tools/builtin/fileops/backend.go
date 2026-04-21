package fileops

import (
	"context"
	"os"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

// Backend abstracts filesystem operations so that file tools
// can operate on a local directory or through a sandbox exec session.
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

// ResolveBackend returns a SandboxExecBackend only when the current runtime
// should execute file operations in sandbox, otherwise it falls back to a
// LocalBackend rooted at workDir.
func ResolveBackend(snapshot *sharedtoolruntime.RuntimeSnapshot, workDir string, runID, accountID, profileRef, workspaceRef string) Backend {
	resolvedWorkDir := resolveWorkDir(workDir)
	if useSandboxBackend(snapshot) {
		return &SandboxExecBackend{
			baseURL:      snapshot.SandboxBaseURL,
			authToken:    snapshot.SandboxAuthToken,
			sessionID:    runID + "/file",
			accountID:    accountID,
			profileRef:   profileRef,
			workspaceRef: workspaceRef,
		}
	}
	return &LocalBackend{WorkDir: resolvedWorkDir}
}

func IsLocalBackend(backend Backend) bool {
	_, ok := backend.(*LocalBackend)
	return ok
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
