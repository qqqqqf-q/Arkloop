package plugincontrib

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/pluginmanifest"
	"arkloop/services/shared/skillstore"
	"arkloop/services/shared/workspaceblob"
	"github.com/google/uuid"
)

func hydrateManifestContext(manifest *Manifest, pluginRoot string) error {
	if manifest == nil {
		return nil
	}
	if strings.TrimSpace(pluginRoot) == "" {
		for _, context := range manifest.Context {
			if strings.TrimSpace(context.Path) != "" && strings.TrimSpace(context.Content) == "" {
				return fmt.Errorf("plugin context %q requires a bundle source", context.Path)
			}
		}
		return nil
	}
	for index := range manifest.Context {
		context := &manifest.Context[index]
		if strings.TrimSpace(context.Content) != "" || strings.TrimSpace(context.Path) == "" {
			continue
		}
		path, err := safePluginPath(pluginRoot, context.Path)
		if err != nil {
			return fmt.Errorf("read plugin context %q: %w", context.Path, err)
		}
		data, err := readPluginFile(pluginRoot, path)
		if err != nil {
			return fmt.Errorf("read plugin context %q: %w", context.Path, err)
		}
		context.Content = strings.TrimSpace(string(data))
	}
	return nil
}

func persistPluginAssets(ctx context.Context, store PluginStore, manifest Manifest, pluginRoot string) error {
	if store == nil {
		return fmt.Errorf("plugin store is not configured")
	}
	if strings.TrimSpace(pluginRoot) == "" {
		if manifestNeedsPluginRoot(manifest) {
			return fmt.Errorf("plugin bundle source is required")
		}
		return nil
	}
	return filepath.WalkDir(pluginRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin bundle contains symlink %q", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(pluginRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := readPluginFile(pluginRoot, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info, statErr := entry.Info(); statErr == nil {
			mode = info.Mode().Perm()
		}
		if mode == 0 {
			mode = 0o644
		}
		if modeStore, ok := store.(interface {
			WriteWithMode(context.Context, string, string, string, []byte, os.FileMode) error
		}); ok {
			return modeStore.WriteWithMode(ctx, manifest.ID, manifest.Version, rel, data, mode)
		}
		return store.Write(ctx, manifest.ID, manifest.Version, rel, data)
	})
}

func manifestNeedsPluginRoot(manifest Manifest) bool {
	for _, skill := range manifest.Skills {
		if strings.TrimSpace(firstNonEmpty(skill.Bundle, skill.Path)) != "" {
			return true
		}
	}
	for _, context := range manifest.Context {
		if strings.TrimSpace(context.Path) != "" && strings.TrimSpace(context.Content) == "" {
			return true
		}
	}
	return false
}

func (i *Installer) ensureSkillPackage(ctx context.Context, repo *data.SkillPackagesRepository, accountID uuid.UUID, manifest Manifest, skill pluginmanifest.SkillConfig, pluginRoot string) error {
	if strings.TrimSpace(pluginRoot) == "" {
		return fmt.Errorf("plugin skill %q requires a bundle source", skill.SkillKey)
	}
	bundlePath := firstNonEmpty(skill.Bundle, skill.Path)
	if bundlePath == "" {
		return fmt.Errorf("plugin skill %q bundle path must not be empty", skill.SkillKey)
	}
	sourcePath, err := safePluginPath(pluginRoot, bundlePath)
	if err != nil {
		return err
	}
	bundleData, err := buildPluginSkillBundle(manifest, skill, pluginRoot, sourcePath)
	if err != nil {
		return err
	}
	bundle, err := skillstore.DecodeBundle(bundleData)
	if err != nil {
		return fmt.Errorf("decode plugin skill bundle %q: %w", skill.SkillKey, err)
	}
	packageManifest, err := skillstore.ValidateManifest(skillstore.PackageManifest{
		SkillKey:        bundle.Definition.SkillKey,
		Version:         bundle.Definition.Version,
		DisplayName:     bundle.Definition.DisplayName,
		Description:     bundle.Definition.Description,
		InstructionPath: bundle.Definition.InstructionPath,
	})
	if err != nil {
		return fmt.Errorf("validate plugin skill %q: %w", skill.SkillKey, err)
	}
	if err := skillstore.ValidateBundleAgainstManifest(packageManifest, bundle); err != nil {
		return fmt.Errorf("validate plugin skill bundle %q: %w", skill.SkillKey, err)
	}
	existing, err := repo.Get(ctx, accountID, packageManifest.SkillKey, packageManifest.Version)
	if err != nil {
		return err
	}
	if existing != nil {
		if !isPluginOwnedSkillPackage(existing, manifest) {
			return fmt.Errorf("plugin skill %q@%q conflicts with an existing skill package", packageManifest.SkillKey, packageManifest.Version)
		}
		return nil
	}
	manifestBytes, err := json.Marshal(packageManifest)
	if err != nil {
		return err
	}
	if err := i.skillStore.PutObject(ctx, packageManifest.ManifestKey, manifestBytes, objectstore.PutOptions{ContentType: "application/json"}); err != nil {
		return fmt.Errorf("write plugin skill manifest %q: %w", skill.SkillKey, err)
	}
	if err := i.skillStore.PutObject(ctx, packageManifest.BundleKey, bundleData, objectstore.PutOptions{ContentType: "application/zstd"}); err != nil {
		return fmt.Errorf("write plugin skill bundle %q: %w", skill.SkillKey, err)
	}
	if _, err := repo.Create(ctx, accountID, packageManifest); err != nil {
		return err
	}
	if err := markPluginSkillPackage(ctx, repo, accountID, packageManifest, manifest); err != nil {
		return err
	}
	return nil
}

func isPluginOwnedSkillPackage(existing *data.SkillPackage, manifest Manifest) bool {
	if existing == nil || existing.RegistrySourceKind == nil || existing.RegistrySourceURL == nil {
		return false
	}
	return strings.TrimSpace(*existing.RegistrySourceKind) == "plugin" && strings.TrimSpace(*existing.RegistrySourceURL) == manifest.ID
}

func markPluginSkillPackage(ctx context.Context, repo *data.SkillPackagesRepository, accountID uuid.UUID, packageManifest skillstore.PackageManifest, manifest Manifest) error {
	return repo.UpdateRegistryMetadata(ctx, accountID, packageManifest.SkillKey, packageManifest.Version, data.SkillPackageRegistryMetadata{
		RegistryProvider:   "arkloop-plugin",
		RegistrySlug:       manifest.ID,
		RegistryVersion:    manifest.Version,
		RegistrySourceKind: "plugin",
		RegistrySourceURL:  manifest.ID,
		ScanStatus:         "unknown",
	})
}

func buildPluginSkillBundle(manifest Manifest, skill pluginmanifest.SkillConfig, pluginRoot, sourcePath string) ([]byte, error) {
	if err := ensureLocalPluginPath(pluginRoot, sourcePath); err != nil {
		return nil, fmt.Errorf("read plugin skill %q: %w", skill.SkillKey, err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read plugin skill %q: %w", skill.SkillKey, err)
	}
	files := map[string][]byte{}
	if info.IsDir() {
		if err := filepath.WalkDir(sourcePath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("plugin skill %q contains symlink %q", skill.SkillKey, path)
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(sourcePath, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			data, err := readPluginFile(pluginRoot, path)
			if err != nil {
				return err
			}
			files[rel] = data
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk plugin skill %q: %w", skill.SkillKey, err)
		}
	} else {
		data, err := readPluginFile(pluginRoot, sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read plugin skill %q: %w", skill.SkillKey, err)
		}
		files[filepath.Base(sourcePath)] = data
	}
	if _, ok := files[skillstore.InstructionPathDefault]; !ok {
		return nil, fmt.Errorf("plugin skill %q missing SKILL.md", skill.SkillKey)
	}
	if _, ok := files["skill.yaml"]; !ok {
		files["skill.yaml"] = []byte(pluginSkillYAML(manifest, skill, files[skillstore.InstructionPathDefault]))
	}
	return encodeSkillBundle(files)
}

func pluginSkillYAML(manifest Manifest, skill pluginmanifest.SkillConfig, instruction []byte) string {
	displayName := headingFromMarkdown(instruction)
	if displayName == "" {
		displayName = skill.SkillKey
	}
	description := strings.TrimSpace(manifest.Description)
	return fmt.Sprintf("skill_key: %s\nversion: %q\ndisplay_name: %q\ndescription: %q\ninstruction_path: SKILL.md\n", skill.SkillKey, skill.Version, displayName, description)
}

func headingFromMarkdown(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func encodeSkillBundle(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	paths := make([]string, 0, len(files))
	for path := range files {
		path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
		if path != "" && path != "." && !strings.HasPrefix(path, "../") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		data := files[path]
		if err := writer.WriteHeader(&tar.Header{Name: path, Mode: fileMode(path), Size: int64(len(data))}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	encoded, err := workspaceblob.Encode(buffer.Bytes())
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func fileMode(path string) int64 {
	if strings.HasSuffix(path, ".sh") {
		return 0o755
	}
	return 0o644
}

func safePluginPath(root, rel string) (string, error) {
	root = filepath.Clean(root)
	raw := strings.TrimSpace(rel)
	if raw == "" {
		return "", fmt.Errorf("plugin path must not be empty")
	}
	slashPath := strings.ReplaceAll(raw, "\\", "/")
	if filepath.IsAbs(raw) || strings.HasPrefix(slashPath, "/") || isWindowsAbsolutePath(slashPath) {
		return "", fmt.Errorf("plugin path %q must be relative", rel)
	}
	if hasParentPathSegment(slashPath) {
		return "", fmt.Errorf("plugin path %q escapes bundle root", rel)
	}
	cleaned := pathpkg.Clean(slashPath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("plugin path %q escapes bundle root", rel)
	}
	return filepath.Join(root, filepath.FromSlash(cleaned)), nil
}

func readPluginFile(root, path string) ([]byte, error) {
	if err := ensureLocalPluginPath(root, path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func ensureLocalPluginPath(root, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("plugin path %q must not be a symlink", path)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	pathReal, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootReal, pathReal)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return fmt.Errorf("plugin path %q escapes bundle root", path)
	}
	return nil
}

func isWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/'
}

func hasParentPathSegment(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
