package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/sandbox/internal/environment"
)

func TestBuildEnvironmentManifestIncludesDirFileAndSymlink(t *testing.T) {
	workspaceDir := t.TempDir()
	oldWorkspace := shellWorkspaceDir
	shellWorkspaceDir = workspaceDir
	defer func() { shellWorkspaceDir = oldWorkspace }()

	repoDir := filepath.Join(workspaceDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.Symlink("main.go", filepath.Join(repoDir, "link.go")); err != nil {
		t.Fatalf("symlink link.go: %v", err)
	}

	manifest, err := buildEnvironmentManifest(environment.ScopeWorkspace, nil)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if !containsManifestPath(manifest.Entries, environment.EntryTypeDir, "repo") {
		t.Fatalf("expected repo dir in manifest: %#v", manifest.Entries)
	}
	if !containsManifestPath(manifest.Entries, environment.EntryTypeFile, "repo/main.go") {
		t.Fatalf("expected repo/main.go in manifest: %#v", manifest.Entries)
	}
	if !containsManifestPath(manifest.Entries, environment.EntryTypeSymlink, "repo/link.go") {
		t.Fatalf("expected repo/link.go symlink in manifest: %#v", manifest.Entries)
	}
}

func TestReadEnvironmentPathsRejectsTraversal(t *testing.T) {
	workspaceDir := t.TempDir()
	oldWorkspace := shellWorkspaceDir
	shellWorkspaceDir = workspaceDir
	defer func() { shellWorkspaceDir = oldWorkspace }()

	if _, err := readEnvironmentPaths(environment.ScopeWorkspace, []string{"../escape"}); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestApplyEnvironmentResetsRootAndRestoresSelectedFiles(t *testing.T) {
	workspaceDir := t.TempDir()
	oldWorkspace := shellWorkspaceDir
	shellWorkspaceDir = workspaceDir
	defer func() { shellWorkspaceDir = oldWorkspace }()

	if err := os.WriteFile(filepath.Join(workspaceDir, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}
	data := []byte("package main\n")
	sum := sha256.Sum256(data)
	manifest := environment.Manifest{
		Version: environment.CurrentManifestVersion,
		Scope:   environment.ScopeWorkspace,
		Entries: []environment.ManifestEntry{
			{Path: "repo", Type: environment.EntryTypeDir, Mode: 0o755},
			{Path: "repo/main.go", Type: environment.EntryTypeFile, Mode: 0o644, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])},
		},
	}
	files := []environment.FilePayload{
		environment.EncodeFilePayload("repo/main.go", data, manifest.Entries[1]),
	}
	if err := applyEnvironment(environment.ScopeWorkspace, manifest, files, true); err != nil {
		t.Fatalf("apply environment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file to be removed, got err=%v", err)
	}
	restored, err := os.ReadFile(filepath.Join(workspaceDir, "repo", "main.go"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restored) != string(data) {
		t.Fatalf("unexpected restored content: %q", restored)
	}
}

func TestApplyEnvironmentRejectsEscapingSymlink(t *testing.T) {
	workspaceDir := t.TempDir()
	oldWorkspace := shellWorkspaceDir
	shellWorkspaceDir = workspaceDir
	defer func() { shellWorkspaceDir = oldWorkspace }()

	manifest := environment.Manifest{
		Version: environment.CurrentManifestVersion,
		Scope:   environment.ScopeWorkspace,
		Entries: []environment.ManifestEntry{{Path: "repo/link", Type: environment.EntryTypeSymlink, LinkTarget: "../../etc/passwd"}},
	}
	if err := applyEnvironment(environment.ScopeWorkspace, manifest, nil, true); err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
}

func containsManifestPath(entries []environment.ManifestEntry, entryType string, path string) bool {
	for _, entry := range entries {
		if entry.Type == entryType && entry.Path == path {
			return true
		}
	}
	return false
}
