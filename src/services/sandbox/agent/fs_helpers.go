package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func pathWithinRoot(rootPath, targetPath string) bool {
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func linkTargetWithinRoot(rootPath, currentPath, linkTarget string) bool {
	resolved := linkTarget
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(currentPath), resolved)
	}
	return pathWithinRoot(rootPath, resolved)
}

func fileModeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode <= 0 {
		return fallback
	}
	return os.FileMode(mode)
}

func restoreRegularFile(reader io.Reader, targetPath string, mode int64) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("restore file parent %s: %w", targetPath, err)
	}
	if err := os.RemoveAll(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace restore file %s: %w", targetPath, err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".restore-*")
	if err != nil {
		return fmt.Errorf("create restore temp file %s: %w", targetPath, err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := io.Copy(tempFile, reader); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write restore file %s: %w", targetPath, err)
	}
	if err := tempFile.Chmod(fileModeOrDefault(mode, 0o644)); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod restore file %s: %w", targetPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close restore file %s: %w", targetPath, err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("replace restore file %s: %w", targetPath, err)
	}
	return nil
}

func ensureEnvironmentRoot(rootPath string) error {
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return fmt.Errorf("ensure environment root %s: %w", rootPath, err)
	}
	return nil
}

func makeEnvironmentTreeWritable(rootPath string) error {
	return filepath.Walk(rootPath, func(current string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info == nil {
			return nil
		}
		if info.IsDir() {
			if err := os.Chmod(current, 0o755); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		if err := os.Chmod(current, 0o644); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}

func pruneEnvironmentRootChildren(rootPath string) error {
	if err := ensureEnvironmentRoot(rootPath); err != nil {
		return err
	}
	if err := makeEnvironmentTreeWritable(rootPath); err != nil {
		return fmt.Errorf("make environment root writable %s: %w", rootPath, err)
	}
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return fmt.Errorf("read environment root %s: %w", rootPath, err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(rootPath, entry.Name())); err != nil {
			return fmt.Errorf("prune environment root %s: %w", rootPath, err)
		}
	}
	return nil
}

func pruneEnvironmentPaths(rootPath string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	ordered := normalizePrunePaths(paths)
	for _, relativePath := range ordered {
		targetPath := filepath.Join(rootPath, filepath.FromSlash(relativePath))
		if !pathWithinRoot(rootPath, targetPath) {
			return fmt.Errorf("environment path escapes root: %s", relativePath)
		}
		if err := makeEnvironmentTreeWritable(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("make environment path writable %s: %w", targetPath, err)
		}
		if err := os.RemoveAll(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prune environment path %s: %w", targetPath, err)
		}
	}
	return nil
}

func normalizePrunePaths(paths []string) []string {
	unique := make(map[string]struct{}, len(paths))
	ordered := make([]string, 0, len(paths))
	for _, item := range paths {
		normalized := strings.TrimSpace(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(item, "/"))))
		if normalized == "" || normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
			continue
		}
		if _, ok := unique[normalized]; ok {
			continue
		}
		unique[normalized] = struct{}{}
		ordered = append(ordered, normalized)
	}
	sort.Slice(ordered, func(i, j int) bool {
		depthI := strings.Count(ordered[i], "/")
		depthJ := strings.Count(ordered[j], "/")
		if depthI == depthJ {
			return ordered[i] > ordered[j]
		}
		return depthI > depthJ
	})
	return ordered
}
