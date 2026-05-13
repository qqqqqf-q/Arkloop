package plugincontrib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"arkloop/services/api/internal/data"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	"github.com/google/uuid"
)

func BuiltinPluginsRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("ARKLOOP_BUILTIN_PLUGINS_ROOT")); root != "" {
		return root, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{
		filepath.Join(cwd, "src", "plugins"),
		filepath.Join(cwd, "..", "..", "plugins"),
		filepath.Join(cwd, "..", "..", "..", "plugins"),
	} {
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("builtin plugins root not found")
}

func (i *Installer) SeedBuiltinCUA(ctx context.Context, accountID, userID uuid.UUID) error {
	root, err := BuiltinPluginsRoot()
	if err != nil {
		return err
	}
	return i.SeedBuiltin(ctx, accountID, userID, filepath.Join(root, "cua", "manifest.yaml"))
}

func (i *Installer) SeedBuiltinCUAForAccounts(ctx context.Context) error {
	root, err := BuiltinPluginsRoot()
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(root, "cua", "manifest.yaml")
	rows, err := i.pool.Query(ctx, `
		SELECT DISTINCT ON (a.id) a.id, m.user_id
		  FROM accounts a
		  JOIN account_memberships m ON m.account_id = a.id
		 WHERE a.deleted_at IS NULL
		 ORDER BY a.id, CASE WHEN m.role = 'account_admin' THEN 0 ELSE 1 END, m.created_at ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var accountID uuid.UUID
		var userID uuid.UUID
		if err := rows.Scan(&accountID, &userID); err != nil {
			return err
		}
		if err := i.SeedBuiltin(ctx, accountID, userID, manifestPath); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (i *Installer) SeedBuiltin(ctx context.Context, accountID, userID uuid.UUID, manifestPath string) error {
	if accountID == uuid.Nil || userID == uuid.Nil {
		return fmt.Errorf("account_id and user_id must not be empty")
	}
	payload, _, pluginRoot, cleanup, err := loadManifestPayload(ctx, InstallRequest{ManifestPath: manifestPath})
	if err != nil {
		return err
	}
	defer cleanup()
	manifest, _, err := decodeManifest(payload)
	if err != nil {
		return err
	}
	if err := validatePluginHost(manifest); err != nil {
		return nil
	}
	if err := hydrateManifestContext(&manifest, pluginRoot); err != nil {
		return err
	}
	normalizedPayload, err := manifest.ToManifestJSON()
	if err != nil {
		return err
	}
	existing, err := i.packagesRepo.GetLatestActive(ctx, accountID, manifest.ID)
	if err != nil {
		return err
	}
	var pkg data.PluginPackage
	if existing == nil || !sameManifestPayload(existing.ManifestJSON, normalizedPayload) {
		pkg, err = i.Install(ctx, InstallRequest{
			AccountID:    accountID,
			UserID:       userID,
			ManifestPath: manifestPath,
			SourceKind:   "builtin",
			SourceURI:    manifestPath,
		})
		if err != nil {
			return err
		}
	} else {
		pkg = *existing
	}
	profileRef := sharedenvironmentref.BuildProfileRef(accountID, &userID)
	workspaceRef, err := ensureDefaultWorkspace(ctx, i.profileRepo, i.workspaceRepo, accountID, userID, profileRef)
	if err != nil {
		return err
	}
	current, err := i.enablementsRepo.Get(ctx, accountID, pkg.ID, profileRef, workspaceRef)
	if err != nil {
		return err
	}
	if current == nil {
		if _, err := i.enablementsRepo.Upsert(ctx, data.PluginEnablement{
			AccountID:       accountID,
			PackageID:       pkg.ID,
			PluginID:        pkg.PluginID,
			PluginVersion:   pkg.Version,
			ProfileRef:      profileRef,
			WorkspaceRef:    workspaceRef,
			Enabled:         false,
			EnabledByUserID: userID,
			SettingsJSON:    json.RawMessage("{}"),
		}); err != nil {
			return err
		}
	}
	runtimeState, err := i.runtimeRepo.Get(ctx, accountID, pkg.ID, profileRef, workspaceRef)
	if err != nil || runtimeState != nil {
		return err
	}
	statusMap, err := pluginDataRuntimeState(i.pluginStore, pkg.PluginID, pkg.Version)
	if err != nil {
		return err
	}
	_, err = i.runtimeRepo.Upsert(ctx, data.PluginRuntimeState{
		AccountID:     accountID,
		PackageID:     pkg.ID,
		PluginID:      pkg.PluginID,
		PluginVersion: pkg.Version,
		ProfileRef:    profileRef,
		WorkspaceRef:  workspaceRef,
		Status:        "not_installed",
		StatusJSON:    runtimeStateJSON(statusMap),
	})
	return err
}

func sameManifestPayload(left, right []byte) bool {
	var compactLeft bytes.Buffer
	var compactRight bytes.Buffer
	if json.Compact(&compactLeft, left) != nil || json.Compact(&compactRight, right) != nil {
		return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
	}
	return bytes.Equal(compactLeft.Bytes(), compactRight.Bytes())
}
