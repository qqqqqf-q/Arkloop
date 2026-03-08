package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arkloop/services/sandbox/internal/environment"
	environmentcontract "arkloop/services/sandbox/internal/environment/contract"
)

func buildEnvironmentManifest(scope string, subtrees []string) (*environmentcontract.Manifest, error) {
	roots, err := environmentRoots(scope)
	if err != nil {
		return nil, err
	}
	entries := make([]environmentcontract.ManifestEntry, 0)
	for _, root := range roots {
		if err := os.MkdirAll(root.HostPath, 0o755); err != nil {
			return nil, fmt.Errorf("ensure environment root %s: %w", root.HostPath, err)
		}
		err = filepath.WalkDir(root.HostPath, func(current string, dirEntry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == root.HostPath {
				return nil
			}
			info, err := dirEntry.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root.HostPath, current)
			if err != nil {
				return err
			}
			rel = environmentPath(rel)
			if rel == "" {
				return nil
			}
			mode := info.Mode()
			switch {
			case mode.IsDir():
				entries = append(entries, environmentcontract.ManifestEntry{Path: rel, Type: environment.EntryTypeDir, Mode: int64(info.Mode().Perm())})
			case mode.IsRegular():
				digest, size, err := digestFile(current)
				if err != nil {
					return err
				}
				entries = append(entries, environmentcontract.ManifestEntry{Path: rel, Type: environment.EntryTypeFile, Mode: int64(info.Mode().Perm()), Size: size, SHA256: digest, MtimeUnixMs: info.ModTime().UTC().UnixMilli()})
			case mode&os.ModeSymlink != 0:
				linkTarget, err := os.Readlink(current)
				if err != nil {
					return err
				}
				if !linkTargetWithinRoot(root.HostPath, current, linkTarget) {
					return nil
				}
				entries = append(entries, environmentcontract.ManifestEntry{Path: rel, Type: environment.EntryTypeSymlink, Mode: int64(info.Mode().Perm()), LinkTarget: linkTarget})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk environment root %s: %w", root.HostPath, err)
		}
	}
	manifest := environment.NormalizeManifest(environmentcontract.Manifest{
		Version: environment.CurrentManifestVersion,
		Scope:   strings.TrimSpace(scope),
		Entries: entries,
	})
	if len(subtrees) == 0 {
		return &manifest, nil
	}
	filtered := environmentcontract.Manifest{Scope: strings.TrimSpace(scope)}
	for _, entry := range manifest.Entries {
		for _, subtree := range subtrees {
			normalized := environmentPath(subtree)
			if normalized == "" || entry.Path == normalized || strings.HasPrefix(entry.Path, normalized+"/") {
				filtered.Entries = append(filtered.Entries, entry)
				break
			}
		}
	}
	filtered = environment.NormalizeManifest(filtered)
	return &filtered, nil
}

func readEnvironmentPaths(scope string, paths []string) ([]environment.FilePayload, error) {
	root, err := singleEnvironmentRoot(scope)
	if err != nil {
		return nil, err
	}
	unique := make(map[string]struct{}, len(paths))
	for _, item := range paths {
		normalized := environmentPath(item)
		if normalized == "" {
			return nil, fmt.Errorf("environment path is invalid")
		}
		unique[normalized] = struct{}{}
	}
	ordered := make([]string, 0, len(unique))
	for item := range unique {
		ordered = append(ordered, item)
	}
	sort.Strings(ordered)

	result := make([]environment.FilePayload, 0, len(ordered))
	for _, relativePath := range ordered {
		targetPath, err := environmentTargetPath(root.HostPath, relativePath)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(targetPath)
		if err != nil {
			return nil, fmt.Errorf("stat environment path %s: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("environment path %s is not a regular file", relativePath)
		}
		data, err := os.ReadFile(targetPath)
		if err != nil {
			return nil, fmt.Errorf("read environment path %s: %w", relativePath, err)
		}
		sum := sha256.Sum256(data)
		result = append(result, environment.EncodeFilePayload(relativePath, data, environment.ManifestEntry{
			Path:   relativePath,
			Type:   environment.EntryTypeFile,
			Mode:   int64(info.Mode().Perm()),
			Size:   info.Size(),
			SHA256: hex.EncodeToString(sum[:]),
		}))
	}
	return result, nil
}

func applyEnvironment(scope string, manifest environmentcontract.Manifest, files []environment.FilePayload, reset bool) error {
	root, err := singleEnvironmentRoot(scope)
	if err != nil {
		return err
	}
	normalized := environment.NormalizeManifest(manifest)
	if normalized.Scope != "" && normalized.Scope != scope {
		return fmt.Errorf("environment scope mismatch: %s", normalized.Scope)
	}
	if reset {
		if err := resetCheckpointRoot(root.HostPath); err != nil {
			return err
		}
	} else if err := os.MkdirAll(root.HostPath, 0o755); err != nil {
		return fmt.Errorf("ensure environment root %s: %w", root.HostPath, err)
	}

	fileData := make(map[string][]byte, len(files))
	entries := environment.EntryMap(normalized.Entries)
	for _, payload := range files {
		entry, ok := entries[payload.Path]
		if !ok || entry.Type != environment.EntryTypeFile {
			return fmt.Errorf("environment payload %s is not declared as file", payload.Path)
		}
		decoded, err := environment.DecodeFilePayload(payload)
		if err != nil {
			return err
		}
		fileData[payload.Path] = decoded
	}

	for _, entry := range normalized.Entries {
		if entry.Type != environment.EntryTypeDir {
			continue
		}
		targetPath, err := environmentTargetPath(root.HostPath, entry.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(targetPath, fileModeOrDefault(entry.Mode, 0o755)); err != nil {
			return fmt.Errorf("restore environment dir %s: %w", targetPath, err)
		}
	}
	for _, entry := range normalized.Entries {
		if entry.Type != environment.EntryTypeSymlink {
			continue
		}
		targetPath, err := environmentTargetPath(root.HostPath, entry.Path)
		if err != nil {
			return err
		}
		if !linkTargetWithinRoot(root.HostPath, targetPath, entry.LinkTarget) {
			return fmt.Errorf("environment symlink escapes root: %s", entry.Path)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("restore environment symlink parent %s: %w", targetPath, err)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return fmt.Errorf("replace environment symlink %s: %w", targetPath, err)
		}
		if err := os.Symlink(entry.LinkTarget, targetPath); err != nil {
			return fmt.Errorf("restore environment symlink %s: %w", targetPath, err)
		}
	}
	for _, entry := range normalized.Entries {
		if entry.Type != environment.EntryTypeFile {
			continue
		}
		data, ok := fileData[entry.Path]
		if !ok {
			continue
		}
		targetPath, err := environmentTargetPath(root.HostPath, entry.Path)
		if err != nil {
			return err
		}
		if expected := strings.TrimSpace(entry.SHA256); expected != "" {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != expected {
				return fmt.Errorf("environment file sha mismatch: %s", entry.Path)
			}
		}
		if err := restoreRegularFile(bytes.NewReader(data), targetPath, entry.Mode); err != nil {
			return err
		}
	}
	return chownTreeIfPossible(root.HostPath)
}

func singleEnvironmentRoot(scope string) (checkpointRoot, error) {
	roots, err := environmentRoots(scope)
	if err != nil {
		return checkpointRoot{}, err
	}
	if len(roots) != 1 {
		return checkpointRoot{}, fmt.Errorf("environment scope %s must map to exactly one root", scope)
	}
	return roots[0], nil
}

func environmentTargetPath(rootPath string, relativePath string) (string, error) {
	relativePath = environmentPath(relativePath)
	if relativePath == "" {
		return "", fmt.Errorf("environment path is invalid")
	}
	targetPath := filepath.Join(rootPath, filepath.FromSlash(relativePath))
	if !pathWithinRoot(rootPath, targetPath) {
		return "", fmt.Errorf("environment path escapes root: %s", relativePath)
	}
	return targetPath, nil
}

func environmentPath(value string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return ""
	}
	cleaned = strings.ReplaceAll(cleaned, "\\", "/")
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = filepath.ToSlash(filepath.Clean(cleaned))
	if cleaned == "." || cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func digestFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
