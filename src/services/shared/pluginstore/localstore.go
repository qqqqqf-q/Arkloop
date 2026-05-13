package pluginstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]*$`)

type LocalStore struct {
	base string
}

func NewLocalStore(base string) (*LocalStore, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, fmt.Errorf("plugin store base must not be empty")
	}
	return &LocalStore{base: base}, nil
}

func (s *LocalStore) Root(pluginID, version string) (string, error) {
	pluginID, version, err := validateIdentity(pluginID, version)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.base, pluginID, version), nil
}

func (s *LocalStore) Path(pluginID, version, relPath string) (string, error) {
	root, err := s.Root(pluginID, version)
	if err != nil {
		return "", err
	}
	return safeJoin(root, relPath)
}

func (s *LocalStore) Exists(pluginID, version, relPath string) (bool, error) {
	path, err := s.Path(pluginID, version, relPath)
	if err != nil {
		return false, err
	}
	_, statErr := os.Stat(path)
	if statErr == nil {
		return true, nil
	}
	if os.IsNotExist(statErr) {
		return false, nil
	}
	return false, statErr
}

func (s *LocalStore) Read(_ context.Context, pluginID, version, relPath string) ([]byte, error) {
	path, err := s.Path(pluginID, version, relPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin store file: %w", err)
	}
	return data, nil
}

func (s *LocalStore) Write(_ context.Context, pluginID, version, relPath string, data []byte) error {
	return s.WriteWithMode(context.Background(), pluginID, version, relPath, data, 0o600)
}

func (s *LocalStore) WriteWithMode(_ context.Context, pluginID, version, relPath string, data []byte, mode os.FileMode) error {
	path, err := s.Path(pluginID, version, relPath)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create plugin store dir: %w", err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write plugin store file: %w", err)
	}
	return nil
}

func (s *LocalStore) Open(_ context.Context, pluginID, version, relPath string) (io.ReadCloser, error) {
	path, err := s.Path(pluginID, version, relPath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open plugin store file: %w", err)
	}
	return file, nil
}

func (s *LocalStore) Remove(_ context.Context, pluginID, version string) error {
	root, err := s.Root(pluginID, version)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove plugin store root: %w", err)
	}
	return nil
}

func safeJoin(root, relPath string) (string, error) {
	relPath = strings.TrimSpace(filepath.ToSlash(relPath))
	if relPath == "" || relPath == "." {
		return "", fmt.Errorf("plugin store path must not be empty")
	}
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, `\`) {
		return "", fmt.Errorf("plugin store path must be relative")
	}
	cleaned := filepath.Clean(filepath.FromSlash(relPath))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(filepath.ToSlash(cleaned), "../") {
		return "", fmt.Errorf("plugin store path escapes plugin root")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootAbs, cleaned)
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return "", fmt.Errorf("plugin store path escapes plugin root")
	}
	return target, nil
}

func validateIdentity(pluginID, version string) (string, string, error) {
	pluginID = strings.TrimSpace(pluginID)
	version = strings.TrimSpace(version)
	if !idPattern.MatchString(pluginID) {
		return "", "", fmt.Errorf("plugin id is invalid")
	}
	if !idPattern.MatchString(version) {
		return "", "", fmt.Errorf("plugin version is invalid")
	}
	return pluginID, version, nil
}
