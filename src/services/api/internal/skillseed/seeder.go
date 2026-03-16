package skillseed

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/skillstore"
	"arkloop/services/shared/workspaceblob"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

const (
	defaultSyncInterval = 30 * time.Second
	advisoryLockKey     = int64(88112002)
)

type objectStore interface {
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
}

type fileSkill struct {
	dir         string
	definition  skillstore.SkillDefinition
	files       map[string][]byte // relative path → content
	contentHash string
}

// Seeder synchronises platform skill files from src/skills/ into the
// database and object storage. Direction is one-way: files → DB.
type Seeder struct {
	root    string
	pool    *pgxpool.Pool
	repo    *data.SkillPackagesRepository
	store   objectStore
	logger  *observability.JSONLogger
	trigger chan struct{}
}

func NewSeeder(root string, pool *pgxpool.Pool, repo *data.SkillPackagesRepository, store objectStore, logger *observability.JSONLogger) *Seeder {
	return &Seeder{
		root:    root,
		pool:    pool,
		repo:    repo,
		store:   store,
		logger:  logger,
		trigger: make(chan struct{}, 1),
	}
}

// NewSeederDirect creates a Seeder without a pgxpool.Pool. It can only be used
// with SyncOnceDirect (no advisory-lock election). Intended for desktop mode
// where there is always a single process and PostgreSQL is not available.
func NewSeederDirect(root string, repo *data.SkillPackagesRepository, store objectStore, logger *observability.JSONLogger) *Seeder {
	return &Seeder{
		root:    root,
		repo:    repo,
		store:   store,
		logger:  logger,
		trigger: make(chan struct{}, 1),
	}
}

// SyncNow performs a single leader-elected sync cycle.
func (s *Seeder) SyncNow(ctx context.Context) error {
	_, err := s.syncIfLeader(ctx)
	return err
}

// SyncOnceDirect runs a single sync cycle without acquiring a PostgreSQL
// advisory lock. Safe to call in desktop mode (single process, no PG).
func (s *Seeder) SyncOnceDirect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.repo == nil || s.root == "" {
		return nil
	}
	return s.syncOnce(ctx)
}

// Run starts the background sync loop: bootstrap once, then tick every 30s
// or on Trigger().
func (s *Seeder) Run(ctx context.Context) {
	if err := s.SyncNow(ctx); err != nil {
		s.logError("skill_seed_bootstrap_failed", err, nil)
	}

	ticker := time.NewTicker(defaultSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.trigger:
		}
		if _, err := s.syncIfLeader(ctx); err != nil {
			s.logError("skill_seed_failed", err, nil)
		}
	}
}

// Trigger requests a non-blocking sync cycle.
func (s *Seeder) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// syncIfLeader acquires a PostgreSQL advisory lock and runs syncOnce.
func (s *Seeder) syncIfLeader(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.pool == nil || s.repo == nil || s.root == "" {
		return false, nil
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&acquired); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	return true, s.syncOnce(ctx)
}

func (s *Seeder) syncOnce(ctx context.Context) error {
	files, err := s.loadFileSkills()
	if err != nil {
		return err
	}
	dbHashes, err := s.loadDBHashes(ctx)
	if err != nil {
		return err
	}

	keySet := make(map[string]struct{})
	for k := range files {
		keySet[k] = struct{}{}
	}
	for k := range dbHashes {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var synced, skipped, deactivated int
	for _, key := range keys {
		fs, hasFile := files[key]
		dbHash, hasDB := dbHashes[key]

		switch {
		case hasFile && hasDB && fs.contentHash == dbHash:
			skipped++

		case hasFile:
			manifest, err := s.buildAndUploadBundle(ctx, fs)
			if err != nil {
				s.logWarn("skill_seed_upload_failed", map[string]any{
					"skill_key": key,
					"error":     err.Error(),
				})
				continue
			}
			if _, err := s.repo.UpsertPlatformSkill(ctx, manifest, fs.contentHash); err != nil {
				s.logWarn("skill_seed_upsert_failed", map[string]any{
					"skill_key": key,
					"error":     err.Error(),
				})
				continue
			}
			synced++

		case hasDB:
			if _, err := s.repo.DeactivatePlatformSkill(ctx, key); err != nil {
				s.logWarn("skill_seed_deactivate_failed", map[string]any{
					"skill_key": key,
					"error":     err.Error(),
				})
				continue
			}
			deactivated++
		}
	}

	if synced > 0 || deactivated > 0 {
		s.logInfo("skill_seed_complete", map[string]any{
			"synced":      synced,
			"skipped":     skipped,
			"deactivated": deactivated,
		})
	}
	return nil
}

// ---------------------------------------------------------------------------
// File-system scanning
// ---------------------------------------------------------------------------

func (s *Seeder) loadFileSkills() (map[string]fileSkill, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read skills root %s: %w", s.root, err)
	}

	skills := make(map[string]fileSkill)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, entry.Name())
		defData, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
		if err != nil {
			continue
		}

		var def skillstore.SkillDefinition
		if err := yaml.Unmarshal(defData, &def); err != nil {
			s.logWarn("skill_seed_parse_failed", map[string]any{
				"dir":   entry.Name(),
				"error": err.Error(),
			})
			continue
		}
		if def.SkillKey == "" {
			s.logWarn("skill_seed_missing_key", map[string]any{"dir": entry.Name()})
			continue
		}

		files := make(map[string][]byte)
		if walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = content
			return nil
		}); walkErr != nil {
			s.logWarn("skill_seed_read_failed", map[string]any{
				"dir":   entry.Name(),
				"error": walkErr.Error(),
			})
			continue
		}

		skills[def.SkillKey] = fileSkill{
			dir:         dir,
			definition:  def,
			files:       files,
			contentHash: computeContentHash(files),
		}
	}
	return skills, nil
}

func computeContentHash(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write(files[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---------------------------------------------------------------------------
// Database helpers
// ---------------------------------------------------------------------------

func (s *Seeder) loadDBHashes(ctx context.Context) (map[string]string, error) {
	return s.repo.ListPlatformSkillHashes(ctx)
}

// ---------------------------------------------------------------------------
// Bundle building and upload
// ---------------------------------------------------------------------------

func (s *Seeder) buildAndUploadBundle(ctx context.Context, skill fileSkill) (skillstore.PackageManifest, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	dirs := make(map[string]struct{})

	paths := make([]string, 0, len(skill.files))
	for p := range skill.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, rel := range paths {
		for _, dir := range parentDirs(rel) {
			if _, ok := dirs[dir]; ok {
				continue
			}
			dirs[dir] = struct{}{}
			if err := tw.WriteHeader(&tar.Header{
				Name:     dir,
				Typeflag: tar.TypeDir,
				Mode:     0o755,
			}); err != nil {
				return skillstore.PackageManifest{}, err
			}
		}

		content := skill.files[rel]
		if err := tw.WriteHeader(&tar.Header{
			Name: rel,
			Mode: fileMode(rel),
			Size: int64(len(content)),
		}); err != nil {
			return skillstore.PackageManifest{}, err
		}
		if _, err := tw.Write(content); err != nil {
			return skillstore.PackageManifest{}, err
		}
	}
	if err := tw.Close(); err != nil {
		return skillstore.PackageManifest{}, err
	}

	bundleData, err := workspaceblob.Encode(buf.Bytes())
	if err != nil {
		return skillstore.PackageManifest{}, fmt.Errorf("compress bundle: %w", err)
	}

	manifest, err := skillstore.ValidateManifest(skillstore.PackageManifest{
		SkillKey:        skill.definition.SkillKey,
		Version:         skill.definition.Version,
		DisplayName:     skill.definition.DisplayName,
		Description:     skill.definition.Description,
		InstructionPath: skill.definition.InstructionPath,
	})
	if err != nil {
		return skillstore.PackageManifest{}, fmt.Errorf("validate manifest: %w", err)
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return skillstore.PackageManifest{}, err
	}
	if err := s.store.PutObject(ctx, manifest.ManifestKey, manifestJSON, objectstore.PutOptions{
		ContentType: "application/json",
	}); err != nil {
		return skillstore.PackageManifest{}, fmt.Errorf("upload manifest: %w", err)
	}

	if err := s.store.PutObject(ctx, manifest.BundleKey, bundleData, objectstore.PutOptions{
		ContentType: "application/zstd",
	}); err != nil {
		return skillstore.PackageManifest{}, fmt.Errorf("upload bundle: %w", err)
	}

	return manifest, nil
}

// ---------------------------------------------------------------------------
// Tar helpers (matching skill_import.go pattern)
// ---------------------------------------------------------------------------

func parentDirs(p string) []string {
	dir := path.Dir(p)
	if dir == "." || dir == "" {
		return nil
	}
	parts := strings.Split(dir, "/")
	out := make([]string, 0, len(parts))
	current := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		out = append(out, current)
	}
	return out
}

func fileMode(name string) int64 {
	if strings.HasSuffix(name, ".sh") {
		return 0o755
	}
	return 0o644
}

// ---------------------------------------------------------------------------
// Root discovery (same strategy as personas.BuiltinPersonasRoot)
// ---------------------------------------------------------------------------

// BuiltinSkillsRoot returns the absolute path to the platform skills directory.
func BuiltinSkillsRoot() (string, error) {
	if envRoot := os.Getenv("ARKLOOP_SKILLS_ROOT"); envRoot != "" {
		return envRoot, nil
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate skills root directory")
	}
	dir := filepath.Dir(filename)
	for {
		if filepath.Base(dir) == "src" {
			return filepath.Join(dir, "skills"), nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return "", fmt.Errorf("src directory not found, cannot locate skills root directory")
}

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

func (s *Seeder) logInfo(msg string, extra map[string]any) {
	if s.logger == nil {
		return
	}
	s.logger.Info(msg, observability.LogFields{}, extra)
}

func (s *Seeder) logWarn(msg string, extra map[string]any) {
	if s.logger == nil {
		return
	}
	s.logger.Warn(msg, observability.LogFields{}, extra)
}

func (s *Seeder) logError(msg string, err error, extra map[string]any) {
	if s.logger == nil {
		return
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["error"] = err.Error()
	s.logger.Error(msg, observability.LogFields{}, extra)
}
