package fileops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// LocalBackend performs file operations directly on the host filesystem,
// resolving relative paths from WorkDir.
type LocalBackend struct {
	WorkDir string
}

func (b *LocalBackend) resolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.WorkDir, path)
	}
	return filepath.Clean(path), nil
}

// ResolvePath validates and resolves a path, returning the cleaned absolute path.
// Relative paths are resolved from WorkDir; absolute paths are preserved.
func (b *LocalBackend) ResolvePath(path string) (string, error) {
	return b.resolvePath(path)
}

func (b *LocalBackend) ReadFile(_ context.Context, path string) ([]byte, error) {
	resolved, err := b.resolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

func (b *LocalBackend) NormalizePath(path string) string {
	resolved, err := b.resolvePath(path)
	if err != nil {
		return normalizePathKey(path)
	}
	return filepath.ToSlash(resolved)
}

func (b *LocalBackend) WriteFile(_ context.Context, path string, data []byte) error {
	resolved, err := b.resolvePath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".arkloop-write-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, resolved); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func (b *LocalBackend) Stat(_ context.Context, path string) (FileInfo, error) {
	resolved, err := b.resolvePath(path)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

func (b *LocalBackend) Exec(ctx context.Context, command string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if b.WorkDir != "" {
		cmd.Dir = b.WorkDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", -1, err
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}
