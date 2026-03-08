package environment

import (
	"path"
	"strings"
)

var workspaceRootFilePatterns = []string{
	".gitignore",
	".gitattributes",
	".editorconfig",
	".env",
	".env.example",
	"README*",
	"Makefile",
	"Dockerfile*",
	"compose.yaml",
	"docker-compose*.yml",
	"go.work",
	"go.mod",
	"go.sum",
	"package.json",
	"pnpm-lock.yaml",
	"pnpm-workspace.yaml",
	"tsconfig.json",
	"tsconfig.*.json",
	"vite.config.*",
	"pyproject.toml",
	"requirements*.txt",
	"Cargo.toml",
	"Cargo.lock",
	"Gemfile",
	"composer.json",
}

var profileExactFiles = map[string]struct{}{
	".bashrc":    {},
	".profile":   {},
	".zshrc":     {},
	".gitconfig": {},
}

var profilePrefixFiles = []string{
	".ssh/",
	".config/",
	".local/bin/",
	".arkloop/",
}

func BuildHydrateManifest(scope string, source Manifest, options PrepareOptions) Manifest {
	selected := Manifest{
		Version:      source.Version,
		Scope:        source.Scope,
		Ref:          source.Ref,
		Revision:     source.Revision,
		BaseRevision: source.BaseRevision,
		CreatedAt:    source.CreatedAt,
	}
	for _, entry := range source.Entries {
		switch entry.Type {
		case EntryTypeDir, EntryTypeSymlink:
			selected.Entries = append(selected.Entries, entry)
		case EntryTypeFile:
			if shouldHydrateFile(scope, entry.Path, options) {
				selected.Entries = append(selected.Entries, entry)
			}
		}
	}
	return NormalizeManifest(selected)
}

func shouldHydrateFile(scope, relativePath string, options PrepareOptions) bool {
	relativePath = normalizeRelativePath(relativePath)
	if relativePath == "" {
		return false
	}
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return shouldHydrateProfileFile(relativePath)
	case ScopeWorkspace:
		if options.WorkspaceMode == WorkspaceHydrationFull {
			return true
		}
		return shouldHydrateWorkspaceFile(relativePath, options.WorkspaceCwd)
	default:
		return false
	}
}

func shouldHydrateProfileFile(relativePath string) bool {
	if _, ok := profileExactFiles[relativePath]; ok {
		return true
	}
	for _, prefix := range profilePrefixFiles {
		if strings.HasPrefix(relativePath, prefix) {
			return true
		}
	}
	return false
}

func shouldHydrateWorkspaceFile(relativePath string, cwd string) bool {
	if withinWorkspaceCwd(relativePath, cwd) {
		return true
	}
	if matchesWorkspaceRootPattern(relativePath) {
		return true
	}
	return isWorkspaceGitMetadata(relativePath)
}

func withinWorkspaceCwd(relativePath string, cwd string) bool {
	if relativePath == "" {
		return false
	}
	prefix := workspaceRelativePrefix(cwd)
	if prefix == "" {
		return true
	}
	return relativePath == prefix || strings.HasPrefix(relativePath, prefix+"/")
}

func workspaceRelativePrefix(cwd string) string {
	cleaned := strings.TrimSpace(cwd)
	if cleaned == "" || cleaned == "/workspace" {
		return ""
	}
	if cleaned == "/workspace/" {
		return ""
	}
	if !strings.HasPrefix(cleaned, "/workspace/") {
		return ""
	}
	return normalizeRelativePath(strings.TrimPrefix(cleaned, "/workspace/"))
}

func matchesWorkspaceRootPattern(relativePath string) bool {
	if strings.Contains(relativePath, "/") {
		return false
	}
	for _, pattern := range workspaceRootFilePatterns {
		if ok, _ := path.Match(pattern, relativePath); ok {
			return true
		}
	}
	return false
}

func isWorkspaceGitMetadata(relativePath string) bool {
	switch relativePath {
	case ".git/HEAD", ".git/config", ".git/index", ".git/packed-refs":
		return true
	}
	return strings.HasPrefix(relativePath, ".git/refs/")
}
