//go:build desktop

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
	"github.com/google/uuid"
)

func desktopSkillResolver(db data.DesktopDB) pipeline.SkillResolver {
	if db == nil {
		return nil
	}
	repo := data.NewSkillsRepository(db)
	return func(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error) {
		return repo.ResolveEnabledSkills(ctx, accountID, profileRef, workspaceRef)
	}
}

func desktopSkillLayoutResolver(useVM bool) pipeline.SkillLayoutResolver {
	return func(_ context.Context, rc *pipeline.RunContext) (skillstore.PathLayout, error) {
		return desktopSkillLayout(useVM, rc.Run.ID)
	}
}

func desktopSkillStoreRoot() (string, error) {
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "skills"), nil
}

func desktopSkillLayout(useVM bool, runID uuid.UUID) (skillstore.PathLayout, error) {
	if useVM {
		return skillstore.DefaultPathLayout(), nil
	}
	storeRoot, err := desktopSkillStoreRoot()
	if err != nil {
		return skillstore.PathLayout{}, err
	}
	indexRoot, err := desktopSkillRuntimeRoot(runID)
	if err != nil {
		return skillstore.PathLayout{}, err
	}
	return skillstore.PathLayout{
		MountRoot: storeRoot,
		IndexPath: filepath.Join(indexRoot, "enabled-skills.json"),
	}, nil
}

func desktopSkillPreparer(useVM bool) pipeline.SkillPreparer {
	if useVM {
		return nil
	}
	return prepareDesktopHostSkills
}

func desktopExternalSkillDirs(db data.DesktopDB) pipeline.ExternalSkillDirsResolver {
	return func(ctx context.Context) []string {
		var dirs []string
		if envDirs := strings.TrimSpace(os.Getenv("ARKLOOP_EXTERNAL_SKILL_DIRS")); envDirs != "" {
			dirs = append(dirs, strings.Split(envDirs, string(os.PathListSeparator))...)
		}
		dirs = append(dirs, loadExternalSkillDirsFromDB(ctx, db)...)
		dirs = append(dirs, skillstore.WellKnownSkillDirs()...)
		return dirs
	}
}

func loadExternalSkillDirsFromDB(ctx context.Context, db data.DesktopDB) []string {
	if db == nil {
		return nil
	}
	var value string
	err := db.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = $1`, "skills.external_dirs").Scan(&value)
	if err != nil {
		return nil
	}
	var dirs []string
	if err := json.Unmarshal([]byte(value), &dirs); err != nil {
		return nil
	}
	return dirs
}

func desktopSkillRuntimeRoot(runID uuid.UUID) (string, error) {
	if runID == uuid.Nil {
		return "", fmt.Errorf("run_id must not be empty")
	}
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "runtime", "skills", runID.String()), nil
}

func cleanupDesktopSkillRuntime(runID uuid.UUID) error {
	root, err := desktopSkillRuntimeRoot(runID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove desktop skill runtime: %w", err)
	}
	return nil
}

func ensureSkillExtracted(ctx context.Context, store objectstore.Store, skill skillstore.ResolvedSkill, storeRoot string) error {
	targetRoot := filepath.Join(storeRoot, strings.TrimSpace(skill.SkillKey)+"@"+strings.TrimSpace(skill.Version))
	hashFile := filepath.Join(targetRoot, ".content_hash")

	if skill.ContentHash != "" {
		if existing, err := os.ReadFile(hashFile); err == nil {
			if strings.TrimSpace(string(existing)) == skill.ContentHash {
				return nil
			}
		}
	}

	bundleRef := strings.TrimSpace(skill.BundleRef)
	if bundleRef == "" {
		return fmt.Errorf("skill %s@%s bundle_ref is empty", skill.SkillKey, skill.Version)
	}
	encoded, err := store.Get(ctx, bundleRef)
	if err != nil {
		return fmt.Errorf("load skill bundle %s@%s: %w", skill.SkillKey, skill.Version, err)
	}
	bundle, err := skillstore.DecodeBundle(encoded)
	if err != nil {
		return fmt.Errorf("decode skill bundle %s@%s: %w", skill.SkillKey, skill.Version, err)
	}
	if err := writeDesktopSkillBundle(targetRoot, bundle); err != nil {
		return err
	}

	if skill.ContentHash != "" {
		_ = atomicWriteDesktopFile(hashFile, []byte(skill.ContentHash), 0o644)
	}
	return nil
}

func prepareDesktopHostSkills(ctx context.Context, skills []skillstore.ResolvedSkill, layout skillstore.PathLayout) error {
	store, err := openDesktopSkillStore(ctx)
	if err != nil {
		return err
	}
	layout = skillstore.NormalizePathLayout(layout)

	storeRoot := layout.MountRoot
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return fmt.Errorf("create desktop skill store: %w", err)
	}

	for _, item := range skills {
		if err := ensureSkillExtracted(ctx, store, item, storeRoot); err != nil {
			return err
		}
	}

	indexJSON, err := skillstore.BuildIndex(skills)
	if err != nil {
		return fmt.Errorf("build desktop skill index: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.IndexPath), 0o755); err != nil {
		return fmt.Errorf("create desktop skill index dir: %w", err)
	}
	if err := atomicWriteDesktopFile(layout.IndexPath, indexJSON, 0o644); err != nil {
		return fmt.Errorf("write desktop skill index: %w", err)
	}
	return nil
}

func openDesktopSkillStore(ctx context.Context) (objectstore.Store, error) {
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return nil, err
	}
	return objectstore.NewFilesystemOpener(desktop.StorageRoot(dataDir)).Open(ctx, objectstore.SkillStoreBucket)
}

func writeDesktopSkillBundle(root string, bundle skillstore.BundleImage) error {
	if err := os.RemoveAll(root); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset desktop skill dir %s: %w", root, err)
	}
	for _, file := range bundle.Files {
		targetPath, err := desktopSkillTargetPath(root, file.Path)
		if err != nil {
			return err
		}
		if file.IsDir {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create desktop skill dir %s: %w", targetPath, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create desktop skill parent %s: %w", targetPath, err)
		}
		mode := os.FileMode(file.Mode)
		if mode == 0 {
			mode = 0o644
		}
		if err := atomicWriteDesktopFile(targetPath, file.Data, mode); err != nil {
			return fmt.Errorf("write desktop skill file %s: %w", targetPath, err)
		}
	}
	return nil
}

func desktopSkillTargetPath(root, relativePath string) (string, error) {
	root = filepath.Clean(root)
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	target = filepath.Clean(target)
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", fmt.Errorf("desktop skill path escapes root: %s", relativePath)
	}
	return target, nil
}

func atomicWriteDesktopFile(targetPath string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".arkloop-skill-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, targetPath)
}
