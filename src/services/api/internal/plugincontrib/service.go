package plugincontrib

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"arkloop/services/api/internal/data"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	"arkloop/services/shared/objectstore"
	sharedpluginmanifest "arkloop/services/shared/pluginmanifest"
	"arkloop/services/shared/pluginregistry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SkillObjectStore interface {
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
}

type PluginStore interface {
	Root(pluginID, version string) (string, error)
	Write(ctx context.Context, pluginID, version, relPath string, data []byte) error
	Remove(ctx context.Context, pluginID, version string) error
}

type Installer struct {
	pool               data.DB
	packagesRepo       *data.PluginPackagesRepository
	enablementsRepo    *data.PluginEnablementsRepository
	runtimeRepo        *data.PluginRuntimeStateRepository
	mcpInstallsRepo    *data.ProfileMCPInstallsRepository
	skillPackagesRepo  *data.SkillPackagesRepository
	skillInstallsRepo  *data.ProfileSkillInstallsRepository
	workspaceSkillRepo *data.WorkspaceSkillEnablementsRepository
	profileRepo        *data.ProfileRegistriesRepository
	workspaceRepo      *data.WorkspaceRegistriesRepository
	skillStore         SkillObjectStore
	pluginStore        PluginStore
}

type Enabler struct {
	pool               data.DB
	packagesRepo       *data.PluginPackagesRepository
	enablementsRepo    *data.PluginEnablementsRepository
	runtimeRepo        *data.PluginRuntimeStateRepository
	mcpInstallsRepo    *data.ProfileMCPInstallsRepository
	workspaceMCPRepo   *data.WorkspaceMCPEnablementsRepository
	skillInstallsRepo  *data.ProfileSkillInstallsRepository
	workspaceSkillRepo *data.WorkspaceSkillEnablementsRepository
	profileRepo        *data.ProfileRegistriesRepository
	workspaceRepo      *data.WorkspaceRegistriesRepository
	pluginStore        PluginStore
}

type Services struct {
	Installer *Installer
	Enabler   *Enabler
}

type Deps struct {
	Pool               data.DB
	PackagesRepo       *data.PluginPackagesRepository
	EnablementsRepo    *data.PluginEnablementsRepository
	RuntimeRepo        *data.PluginRuntimeStateRepository
	MCPInstallsRepo    *data.ProfileMCPInstallsRepository
	WorkspaceMCPRepo   *data.WorkspaceMCPEnablementsRepository
	SkillPackagesRepo  *data.SkillPackagesRepository
	SkillInstallsRepo  *data.ProfileSkillInstallsRepository
	WorkspaceSkillRepo *data.WorkspaceSkillEnablementsRepository
	ProfileRepo        *data.ProfileRegistriesRepository
	WorkspaceRepo      *data.WorkspaceRegistriesRepository
	SkillStore         SkillObjectStore
	PluginStore        PluginStore
}

type InstallRequest struct {
	AccountID    uuid.UUID
	UserID       uuid.UUID
	ManifestJSON json.RawMessage
	ManifestPath string
	SourceKind   string
	SourceURI    string
}

type EnableRequest struct {
	AccountID    uuid.UUID
	UserID       uuid.UUID
	PluginID     string
	ProfileRef   string
	WorkspaceRef string
	Enabled      bool
	Settings     map[string]any
}

func NewServices(deps Deps) (*Services, error) {
	if deps.Pool == nil || deps.PackagesRepo == nil || deps.EnablementsRepo == nil || deps.RuntimeRepo == nil || deps.MCPInstallsRepo == nil || deps.WorkspaceMCPRepo == nil || deps.SkillPackagesRepo == nil || deps.SkillInstallsRepo == nil || deps.WorkspaceSkillRepo == nil || deps.ProfileRepo == nil || deps.WorkspaceRepo == nil || deps.SkillStore == nil || deps.PluginStore == nil {
		return nil, errors.New("plugincontrib dependencies are incomplete")
	}
	return &Services{
		Installer: &Installer{
			pool:               deps.Pool,
			packagesRepo:       deps.PackagesRepo,
			enablementsRepo:    deps.EnablementsRepo,
			runtimeRepo:        deps.RuntimeRepo,
			mcpInstallsRepo:    deps.MCPInstallsRepo,
			skillPackagesRepo:  deps.SkillPackagesRepo,
			skillInstallsRepo:  deps.SkillInstallsRepo,
			workspaceSkillRepo: deps.WorkspaceSkillRepo,
			profileRepo:        deps.ProfileRepo,
			workspaceRepo:      deps.WorkspaceRepo,
			skillStore:         deps.SkillStore,
			pluginStore:        deps.PluginStore,
		},
		Enabler: &Enabler{
			pool:               deps.Pool,
			packagesRepo:       deps.PackagesRepo,
			enablementsRepo:    deps.EnablementsRepo,
			runtimeRepo:        deps.RuntimeRepo,
			mcpInstallsRepo:    deps.MCPInstallsRepo,
			workspaceMCPRepo:   deps.WorkspaceMCPRepo,
			skillInstallsRepo:  deps.SkillInstallsRepo,
			workspaceSkillRepo: deps.WorkspaceSkillRepo,
			profileRepo:        deps.ProfileRepo,
			workspaceRepo:      deps.WorkspaceRepo,
			pluginStore:        deps.PluginStore,
		},
	}, nil
}

func (i *Installer) Install(ctx context.Context, req InstallRequest) (data.PluginPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	payload, sourceURI, pluginRoot, cleanup, err := loadManifestPayload(ctx, req)
	if err != nil {
		return data.PluginPackage{}, err
	}
	defer cleanup()
	manifest, _, err := decodeManifest(payload)
	if err != nil {
		return data.PluginPackage{}, err
	}
	if err := validatePluginHost(manifest); err != nil {
		return data.PluginPackage{}, err
	}
	if err := hydrateManifestContext(&manifest, pluginRoot); err != nil {
		return data.PluginPackage{}, err
	}
	if err := persistPluginAssets(ctx, i.pluginStore, manifest, pluginRoot); err != nil {
		return data.PluginPackage{}, err
	}
	normalizedPayload, err := sharedpluginmanifest.ToManifestJSON(manifest)
	if err != nil {
		return data.PluginPackage{}, err
	}
	settingsSchema := manifest.SettingsSchema
	if settingsSchema == nil {
		settingsSchema = map[string]any{}
	}
	settingsSchemaPayload, err := json.Marshal(settingsSchema)
	if err != nil {
		return data.PluginPackage{}, err
	}
	profileRef := sharedenvironmentref.BuildProfileRef(req.AccountID, &req.UserID)
	if err := i.profileRepo.Ensure(ctx, profileRef, req.AccountID, req.UserID); err != nil {
		return data.PluginPackage{}, err
	}
	workspaceRef, err := ensureDefaultWorkspace(ctx, i.profileRepo, i.workspaceRepo, req.AccountID, req.UserID, profileRef)
	if err != nil {
		return data.PluginPackage{}, err
	}
	tx, err := i.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.PluginPackage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txPackages := i.packagesRepo.WithTx(tx)
	pkg, err := txPackages.Upsert(ctx, data.PluginPackageInput{
		AccountID:          req.AccountID,
		PluginID:           manifest.ID,
		Version:            manifest.Version,
		DisplayName:        manifest.Name,
		Description:        optionalString(manifest.Description),
		ManifestJSON:       json.RawMessage(normalizedPayload),
		SettingsSchemaJSON: settingsSchemaPayload,
		SourceKind:         firstNonEmpty(req.SourceKind, "manifest"),
		SourceURI:          optionalString(firstNonEmpty(req.SourceURI, sourceURI)),
	})
	if err != nil {
		return data.PluginPackage{}, err
	}
	txEnablements := i.enablementsRepo.WithTx(tx)
	txRuntime := i.runtimeRepo.WithTx(tx)
	priorEnablements, err := txEnablements.ListByPlugin(ctx, req.AccountID, manifest.ID)
	if err != nil {
		return data.PluginPackage{}, err
	}
	priorRuntimeStates, err := txRuntime.ListByPlugin(ctx, req.AccountID, manifest.ID)
	if err != nil {
		return data.PluginPackage{}, err
	}
	if err := migratePluginEnablements(ctx, txEnablements, req.AccountID, pkg, priorEnablements); err != nil {
		return data.PluginPackage{}, err
	}
	if err := migratePluginRuntimeStates(ctx, txRuntime, i.pluginStore, req.AccountID, pkg, priorRuntimeStates); err != nil {
		return data.PluginPackage{}, err
	}
	if err := txEnablements.DeleteOtherPackagesForPlugin(ctx, req.AccountID, manifest.ID, pkg.ID); err != nil {
		return data.PluginPackage{}, err
	}
	if err := txRuntime.DeleteOtherPackagesForPlugin(ctx, req.AccountID, manifest.ID, pkg.ID); err != nil {
		return data.PluginPackage{}, err
	}
	if err := txPackages.DeactivateOtherVersions(ctx, req.AccountID, manifest.ID, pkg.ID); err != nil {
		return data.PluginPackage{}, err
	}
	txMCP := i.mcpInstallsRepo.WithTx(tx)
	txSkillPackages := i.skillPackagesRepo.WithTx(tx)
	txSkills := i.skillInstallsRepo.WithTx(tx)
	for _, skill := range manifest.Skills {
		if err := i.ensureSkillPackage(ctx, txSkillPackages, req.AccountID, manifest, skill, pluginRoot); err != nil {
			return data.PluginPackage{}, err
		}
	}
	_, defaultSettings, err := normalizeSettings(nil, manifest)
	if err != nil {
		return data.PluginPackage{}, err
	}
	runtimeState, err := i.installerRuntimeState(ctx, pkg, profileRef, workspaceRef)
	if err != nil {
		return data.PluginPackage{}, err
	}
	if _, err := syncProfileMCPInstalls(ctx, txMCP, req.AccountID, profileRef, manifest, defaultSettings, runtimeState, false); err != nil {
		return data.PluginPackage{}, err
	}
	if err := installProfileSkills(ctx, txSkills, req.AccountID, req.UserID, profileRef, manifest); err != nil {
		return data.PluginPackage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return data.PluginPackage{}, err
	}
	return pkg, nil
}

func (i *Installer) Uninstall(ctx context.Context, accountID uuid.UUID, pluginID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, err := i.packagesRepo.GetLatestActive(ctx, accountID, pluginID)
	if err != nil || pkg == nil {
		return err
	}
	manifest, _, err := decodeManifest(pkg.ManifestJSON)
	if err != nil {
		return err
	}
	tx, err := i.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txSkills := i.workspaceSkillRepo.WithTx(tx)
	enablements, err := i.enablementsRepo.ListByPlugin(ctx, accountID, manifest.ID)
	if err != nil {
		return err
	}
	for _, enablement := range enablements {
		if !enablement.Enabled {
			continue
		}
		for _, skill := range manifest.Skills {
			if err := txSkills.Set(ctx, accountID, enablement.WorkspaceRef, enablement.EnabledByUserID, skill.SkillKey, skill.Version, false); err != nil {
				return err
			}
		}
	}
	if err := i.enablementsRepo.WithTx(tx).DeleteByPlugin(ctx, accountID, manifest.ID); err != nil {
		return err
	}
	if err := i.mcpInstallsRepo.WithTx(tx).DeleteByOwnerPlugin(ctx, accountID, manifest.ID); err != nil {
		return err
	}
	if err := i.skillInstallsRepo.WithTx(tx).DeleteByOwnerPlugin(ctx, accountID, manifest.ID); err != nil {
		return err
	}
	if err := i.packagesRepo.WithTx(tx).DeleteByPluginID(ctx, accountID, manifest.ID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return i.pluginStore.Remove(ctx, pkg.PluginID, pkg.Version)
}

func (i *Installer) installerRuntimeState(ctx context.Context, pkg data.PluginPackage, profileRef, workspaceRef string) (map[string]any, error) {
	state, err := pluginDataRuntimeState(i.pluginStore, pkg.PluginID, pkg.Version)
	if err != nil {
		return nil, err
	}
	current, err := i.runtimeRepo.Get(ctx, pkg.AccountID, pkg.ID, profileRef, workspaceRef)
	if err != nil {
		return nil, err
	}
	if current != nil {
		for key, value := range decodePluginJSONMap(current.StatusJSON) {
			state[key] = value
		}
		if strings.TrimSpace(fmt.Sprint(state["plugin_data"])) == "" {
			dataDir, err := i.pluginStore.Root(pkg.PluginID, pkg.Version)
			if err != nil {
				return nil, err
			}
			state["plugin_data"] = dataDir
		}
	}
	return state, nil
}

func migratePluginEnablements(ctx context.Context, repo *data.PluginEnablementsRepository, accountID uuid.UUID, pkg data.PluginPackage, prior []data.PluginEnablement) error {
	for _, item := range prior {
		if item.PackageID == pkg.ID {
			continue
		}
		existing, err := repo.Get(ctx, accountID, pkg.ID, item.ProfileRef, item.WorkspaceRef)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if _, err := repo.Upsert(ctx, data.PluginEnablement{
			AccountID:       accountID,
			PackageID:       pkg.ID,
			PluginID:        pkg.PluginID,
			PluginVersion:   pkg.Version,
			ProfileRef:      item.ProfileRef,
			WorkspaceRef:    item.WorkspaceRef,
			Enabled:         item.Enabled,
			EnabledByUserID: item.EnabledByUserID,
			SettingsJSON:    item.SettingsJSON,
		}); err != nil {
			return err
		}
	}
	return nil
}

func migratePluginRuntimeStates(ctx context.Context, repo *data.PluginRuntimeStateRepository, store PluginStore, accountID uuid.UUID, pkg data.PluginPackage, prior []data.PluginRuntimeState) error {
	seenScopes := make(map[string]struct{})
	for _, item := range prior {
		if item.PackageID == pkg.ID {
			continue
		}
		scopeKey := item.ProfileRef + "\x00" + item.WorkspaceRef
		if _, ok := seenScopes[scopeKey]; ok {
			continue
		}
		seenScopes[scopeKey] = struct{}{}
		state, err := pluginDataRuntimeState(store, pkg.PluginID, pkg.Version)
		if err != nil {
			return err
		}
		if !preserveRuntimeCheckStatus(state, &item) {
			continue
		}
		existing, err := repo.Get(ctx, accountID, pkg.ID, item.ProfileRef, item.WorkspaceRef)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if _, err := repo.Upsert(ctx, data.PluginRuntimeState{
			AccountID:     accountID,
			PackageID:     pkg.ID,
			PluginID:      pkg.PluginID,
			PluginVersion: pkg.Version,
			ProfileRef:    item.ProfileRef,
			WorkspaceRef:  item.WorkspaceRef,
			Status:        "not_installed",
			StatusJSON:    runtimeStateJSON(state),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Enabler) SetEnabled(ctx context.Context, req EnableRequest) (data.PluginEnablement, error) {
	return e.apply(ctx, req, true)
}

func (e *Enabler) UpdateSettings(ctx context.Context, req EnableRequest) (data.PluginEnablement, error) {
	current, err := e.resolveExistingEnablement(ctx, req)
	if err != nil {
		return data.PluginEnablement{}, err
	}
	req.Enabled = current.Enabled
	merged := decodePluginJSONMap(current.SettingsJSON)
	for key, value := range req.Settings {
		merged[key] = value
	}
	req.Settings = merged
	return e.apply(ctx, req, false)
}

func (e *Enabler) RuntimeStatus(ctx context.Context, accountID, userID uuid.UUID, pluginID, profileRef, workspaceRef string) (*data.PluginRuntimeState, error) {
	pkg, manifest, resolvedProfileRef, resolvedWorkspaceRef, err := e.resolveScope(ctx, EnableRequest{
		AccountID:    accountID,
		UserID:       userID,
		PluginID:     pluginID,
		ProfileRef:   profileRef,
		WorkspaceRef: workspaceRef,
	})
	if err != nil {
		return nil, err
	}
	if err := validatePluginHost(manifest); err != nil {
		return nil, err
	}
	if len(manifest.Runtime) > 0 {
		current, err := e.runtimeRepo.Get(ctx, accountID, pkg.ID, resolvedProfileRef, resolvedWorkspaceRef)
		if err != nil {
			return nil, err
		}
		statusMap, overall, detectErr := e.detectRuntimeState(ctx, pkg, manifest)
		if detectErr != nil {
			return nil, detectErr
		}
		preserveRuntimeCheckStatus(statusMap, current)
		state, err := e.runtimeRepo.Upsert(ctx, data.PluginRuntimeState{
			AccountID:     accountID,
			PackageID:     pkg.ID,
			PluginID:      pkg.PluginID,
			PluginVersion: pkg.Version,
			ProfileRef:    resolvedProfileRef,
			WorkspaceRef:  resolvedWorkspaceRef,
			Status:        overall,
			StatusJSON:    runtimeStateJSON(statusMap),
		})
		if err != nil {
			return nil, err
		}
		return &state, nil
	}
	return e.runtimeRepo.Get(ctx, accountID, pkg.ID, resolvedProfileRef, resolvedWorkspaceRef)
}

func (e *Enabler) GetEnablement(ctx context.Context, req EnableRequest) (*data.PluginEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, _, profileRef, workspaceRef, err := e.resolveScope(ctx, req)
	if err != nil {
		return nil, err
	}
	return e.enablementsRepo.Get(ctx, req.AccountID, pkg.ID, profileRef, workspaceRef)
}

func (e *Enabler) apply(ctx context.Context, req EnableRequest, toggle bool) (data.PluginEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, manifest, profileRef, workspaceRef, err := e.resolveScope(ctx, req)
	if err != nil {
		return data.PluginEnablement{}, err
	}
	currentEnablement, err := e.enablementsRepo.Get(ctx, req.AccountID, pkg.ID, profileRef, workspaceRef)
	if err != nil {
		return data.PluginEnablement{}, err
	}
	if err := validatePluginHost(manifest); err != nil {
		if req.Enabled || (!toggle && currentEnablement != nil && currentEnablement.Enabled) {
			return data.PluginEnablement{}, err
		}
	}
	mergedSettings := map[string]any{}
	if currentEnablement != nil {
		for key, value := range decodePluginJSONMap(currentEnablement.SettingsJSON) {
			mergedSettings[key] = value
		}
	}
	for key, value := range req.Settings {
		mergedSettings[key] = value
	}
	mergedSettings = normalizeRuntimeSettings(manifest, mergedSettings)
	settingsPayload, settings, err := normalizeSettings(mergedSettings, manifest)
	if err != nil {
		return data.PluginEnablement{}, err
	}
	runtimeState, currentRuntime, err := e.applyRuntimeState(ctx, req.AccountID, pkg, manifest, profileRef, workspaceRef, req.Enabled)
	if err != nil {
		return data.PluginEnablement{}, err
	}
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.PluginEnablement{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	enablement, err := e.enablementsRepo.WithTx(tx).Upsert(ctx, data.PluginEnablement{
		AccountID:       req.AccountID,
		PackageID:       pkg.ID,
		PluginID:        pkg.PluginID,
		PluginVersion:   pkg.Version,
		ProfileRef:      profileRef,
		WorkspaceRef:    workspaceRef,
		Enabled:         req.Enabled,
		EnabledByUserID: req.UserID,
		SettingsJSON:    settingsPayload,
	})
	if err != nil {
		return data.PluginEnablement{}, err
	}
	if err := e.syncDerivedResources(ctx, tx, req, manifest, profileRef, workspaceRef, settings, runtimeState, toggle); err != nil {
		return data.PluginEnablement{}, err
	}
	if currentRuntime == nil || (req.Enabled && len(manifest.Runtime) > 0) {
		if _, err := e.runtimeRepo.WithTx(tx).Upsert(ctx, data.PluginRuntimeState{
			AccountID:     req.AccountID,
			PackageID:     pkg.ID,
			PluginID:      pkg.PluginID,
			PluginVersion: pkg.Version,
			ProfileRef:    profileRef,
			WorkspaceRef:  workspaceRef,
			Status:        runtimeStateStatus(runtimeState, manifest),
			StatusJSON:    runtimeStateJSON(runtimeState),
		}); err != nil {
			return data.PluginEnablement{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return data.PluginEnablement{}, err
	}
	return enablement, nil
}

func (e *Enabler) applyRuntimeState(ctx context.Context, accountID uuid.UUID, pkg data.PluginPackage, manifest Manifest, profileRef, workspaceRef string, requireReady bool) (map[string]any, *data.PluginRuntimeState, error) {
	runtimeState, err := pluginDataRuntimeState(e.pluginStore, pkg.PluginID, pkg.Version)
	if err != nil {
		return nil, nil, err
	}
	currentRuntime, err := e.runtimeRepo.Get(ctx, accountID, pkg.ID, profileRef, workspaceRef)
	if err != nil {
		return nil, nil, err
	}
	if currentRuntime != nil {
		for key, value := range decodePluginJSONMap(currentRuntime.StatusJSON) {
			runtimeState[key] = value
		}
	}
	if requireReady && len(manifest.Runtime) > 0 {
		detectedState, overall, err := e.detectRuntimeState(ctx, pkg, manifest)
		if err != nil {
			return nil, nil, err
		}
		for key, value := range detectedState {
			runtimeState[key] = value
		}
		if overall != "installed" {
			return nil, nil, fmt.Errorf("plugin runtime is not installed: %s", overall)
		}
	}
	return runtimeState, currentRuntime, nil
}

func (e *Enabler) resolveExistingEnablement(ctx context.Context, req EnableRequest) (data.PluginEnablement, error) {
	pkg, _, profileRef, workspaceRef, err := e.resolveScope(ctx, req)
	if err != nil {
		return data.PluginEnablement{}, err
	}
	current, err := e.enablementsRepo.Get(ctx, req.AccountID, pkg.ID, profileRef, workspaceRef)
	if err != nil || current == nil {
		return data.PluginEnablement{}, err
	}
	return *current, nil
}

func (e *Enabler) resolveScope(ctx context.Context, req EnableRequest) (data.PluginPackage, Manifest, string, string, error) {
	pkg, err := e.packagesRepo.GetLatestActive(ctx, req.AccountID, req.PluginID)
	if err != nil {
		return data.PluginPackage{}, Manifest{}, "", "", err
	}
	if pkg == nil {
		return data.PluginPackage{}, Manifest{}, "", "", fmt.Errorf("plugin package not found")
	}
	manifest, _, err := decodeManifest(pkg.ManifestJSON)
	if err != nil {
		return data.PluginPackage{}, Manifest{}, "", "", err
	}
	profileRef := strings.TrimSpace(req.ProfileRef)
	if profileRef == "" {
		profileRef = sharedenvironmentref.BuildProfileRef(req.AccountID, &req.UserID)
	}
	workspaceRef := strings.TrimSpace(req.WorkspaceRef)
	if workspaceRef == "" {
		created, err := ensureDefaultWorkspace(ctx, e.profileRepo, e.workspaceRepo, req.AccountID, req.UserID, profileRef)
		if err != nil {
			return data.PluginPackage{}, Manifest{}, "", "", err
		}
		workspaceRef = created
	}
	workspace, err := e.workspaceRepo.Get(ctx, workspaceRef)
	if err != nil {
		return data.PluginPackage{}, Manifest{}, "", "", err
	}
	if workspace == nil || workspace.AccountID != req.AccountID {
		return data.PluginPackage{}, Manifest{}, "", "", fmt.Errorf("workspace not found")
	}
	return *pkg, manifest, profileRef, workspaceRef, nil
}

func (e *Enabler) syncDerivedResources(ctx context.Context, tx pgx.Tx, req EnableRequest, manifest Manifest, profileRef, workspaceRef string, settings map[string]any, runtimeState map[string]any, toggle bool) error {
	settings = normalizeRuntimeSettings(manifest, settings)
	if err := validatePluginHooks(manifest, settings, runtimeState, req.Enabled); err != nil {
		return err
	}
	txMCP := e.mcpInstallsRepo.WithTx(tx)
	installsByKey, err := syncProfileMCPInstalls(ctx, txMCP, req.AccountID, profileRef, manifest, settings, runtimeState, req.Enabled)
	if err != nil {
		return err
	}
	if err := installProfileSkills(ctx, e.skillInstallsRepo.WithTx(tx), req.AccountID, req.UserID, profileRef, manifest); err != nil {
		return err
	}
	for _, server := range manifest.MCPServers {
		install := installsByKey[pluginInstallKey(manifest.ID, server.ServerID)]
		if toggle {
			if err := e.workspaceMCPRepo.WithTx(tx).Set(ctx, req.AccountID, profileRef, workspaceRef, install.ID, &req.UserID, req.Enabled); err != nil {
				return err
			}
		}
	}
	if toggle {
		txSkills := e.workspaceSkillRepo.WithTx(tx)
		if req.Enabled {
			for _, skill := range manifest.Skills {
				if err := txSkills.Set(ctx, req.AccountID, workspaceRef, req.UserID, skill.SkillKey, skill.Version, true); err != nil {
					return err
				}
			}
		} else {
			keepEnabled, err := e.workspaceHasEnabledPluginReference(ctx, tx, req.AccountID, manifest.ID, workspaceRef)
			if err != nil {
				return err
			}
			if !keepEnabled {
				for _, skill := range manifest.Skills {
					if err := txSkills.Set(ctx, req.AccountID, workspaceRef, req.UserID, skill.SkillKey, skill.Version, false); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func normalizeRuntimeSettings(manifest Manifest, settings map[string]any) map[string]any {
	if strings.TrimSpace(manifest.ID) != cuaPluginID {
		return settings
	}
	out := make(map[string]any, len(settings))
	for key, value := range settings {
		if strings.TrimSpace(key) == "auto_update_enabled" {
			continue
		}
		out[key] = value
	}
	return out
}

func (e *Enabler) workspaceHasEnabledPluginReference(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, pluginID, workspaceRef string) (bool, error) {
	enablements, err := e.enablementsRepo.WithTx(tx).ListByPlugin(ctx, accountID, pluginID)
	if err != nil {
		return false, err
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	for _, enablement := range enablements {
		if enablement.Enabled && strings.TrimSpace(enablement.WorkspaceRef) == workspaceRef {
			return true, nil
		}
	}
	return false, nil
}

func syncProfileMCPInstalls(ctx context.Context, repo *data.ProfileMCPInstallsRepository, accountID uuid.UUID, profileRef string, manifest Manifest, settings map[string]any, runtimeState map[string]any, strictPlaceholders bool) (map[string]data.ProfileMCPInstall, error) {
	existing, err := repo.ListByProfile(ctx, accountID, profileRef)
	if err != nil {
		return nil, err
	}
	installsByKey := make(map[string]data.ProfileMCPInstall, len(existing))
	for _, install := range existing {
		if install.OwnerPluginID == nil || strings.TrimSpace(*install.OwnerPluginID) != manifest.ID {
			continue
		}
		installsByKey[install.InstallKey] = install
	}
	expected := make(map[string]struct{}, len(manifest.MCPServers))
	for _, server := range manifest.MCPServers {
		installKey := pluginInstallKey(manifest.ID, server.ServerID)
		expected[installKey] = struct{}{}
		install, ok := installsByKey[installKey]
		if !ok {
			created, err := buildMCPInstall(accountID, profileRef, manifest, server, settings, runtimeState, strictPlaceholders)
			if err != nil {
				return nil, err
			}
			install, err = repo.Create(ctx, created)
			if err != nil {
				return nil, err
			}
			installsByKey[installKey] = install
			continue
		}
		patchSpec, err := renderLaunchSpec(server.LaunchSpec, settings, runtimeState, strictPlaceholders)
		if err != nil {
			return nil, err
		}
		displayName := server.DisplayName
		transport := server.Transport
		hostRequirement := pluginHostRequirement(manifest, server)
		updated, err := repo.Patch(ctx, accountID, install.ID, data.MCPInstallPatch{
			DisplayName:        &displayName,
			Transport:          &transport,
			LaunchSpecJSON:     &patchSpec,
			HostRequirement:    &hostRequirement,
			OwnerPluginVersion: &manifest.Version,
		})
		if err != nil {
			return nil, err
		}
		if updated != nil {
			installsByKey[installKey] = *updated
		}
	}
	for _, install := range existing {
		if install.OwnerPluginID == nil || strings.TrimSpace(*install.OwnerPluginID) != manifest.ID {
			continue
		}
		if _, ok := expected[install.InstallKey]; ok {
			continue
		}
		if err := repo.Delete(ctx, accountID, install.ID); err != nil {
			return nil, err
		}
		delete(installsByKey, install.InstallKey)
	}
	return installsByKey, nil
}

func installProfileSkills(ctx context.Context, repo *data.ProfileSkillInstallsRepository, accountID, userID uuid.UUID, profileRef string, manifest Manifest) error {
	for _, skill := range manifest.Skills {
		if err := repo.InstallWithOwnerPlugin(ctx, profileRef, accountID, userID, skill.SkillKey, skill.Version, &manifest.ID, &manifest.Version); err != nil {
			return err
		}
	}
	return nil
}

func buildMCPInstall(accountID uuid.UUID, profileRef string, manifest Manifest, server ManifestMCPServer, settings map[string]any, runtimeState map[string]any, strictPlaceholders bool) (data.ProfileMCPInstall, error) {
	sourceKind := "plugin"
	hostRequirement := pluginHostRequirement(manifest, server)
	launchSpec, err := renderLaunchSpec(server.LaunchSpec, settings, runtimeState, strictPlaceholders)
	if err != nil {
		return data.ProfileMCPInstall{}, err
	}
	return data.ProfileMCPInstall{
		AccountID:          accountID,
		ProfileRef:         profileRef,
		InstallKey:         pluginInstallKey(manifest.ID, server.ServerID),
		DisplayName:        server.DisplayName,
		SourceKind:         sourceKind,
		SourceURI:          optionalString(server.SourceURI),
		SyncMode:           data.MCPSyncModeNone,
		Transport:          server.Transport,
		LaunchSpecJSON:     launchSpec,
		HostRequirement:    hostRequirement,
		DiscoveryStatus:    data.MCPDiscoveryStatusNeedsCheck,
		OwnerPluginID:      &manifest.ID,
		OwnerPluginVersion: &manifest.Version,
	}, nil
}

func pluginHostRequirement(manifest Manifest, server ManifestMCPServer) string {
	hostRequirement := server.HostRequirement
	if hostRequirement == "" {
		hostRequirement = manifest.HostRequirement
	}
	if hostRequirement == "" {
		if server.Transport == "stdio" {
			hostRequirement = data.MCPHostRequirementCloudWorker
		} else {
			hostRequirement = data.MCPHostRequirementRemoteHTTP
		}
	}
	return hostRequirement
}

func renderLaunchSpec(spec map[string]any, settings map[string]any, runtimeState map[string]any, strictPlaceholders bool) (json.RawMessage, error) {
	rendered, err := renderValue(spec, settings, runtimeState, strictPlaceholders)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(rendered)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func renderValue(value any, settings map[string]any, runtimeState map[string]any, strictPlaceholders bool) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			rendered, err := renderValue(child, settings, runtimeState, strictPlaceholders)
			if err != nil {
				return nil, err
			}
			out[key] = rendered
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			rendered, err := renderValue(child, settings, runtimeState, strictPlaceholders)
			if err != nil {
				return nil, err
			}
			out = append(out, rendered)
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, child := range typed {
			rendered, err := renderSettingString(child, settings, runtimeState, strictPlaceholders)
			if err != nil {
				return nil, err
			}
			out = append(out, rendered)
		}
		return out, nil
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, child := range typed {
			rendered, err := renderSettingString(child, settings, runtimeState, strictPlaceholders)
			if err != nil {
				return nil, err
			}
			out[key] = rendered
		}
		return out, nil
	case string:
		return renderSettingString(typed, settings, runtimeState, strictPlaceholders)
	default:
		return value, nil
	}
}

func renderSettingString(value string, settings map[string]any, runtimeState map[string]any, strictPlaceholders bool) (string, error) {
	resolved, err := sharedpluginmanifest.ResolveString(value, sharedpluginmanifest.PlaceholderContext{
		Settings:          stringSettings(settings),
		RuntimePaths:      runtimePathSettings(runtimeState),
		RuntimeProperties: runtimePropertySettings(runtimeState),
		PluginData:        stringFromPluginMap(runtimeState, "plugin_data"),
		Platform:          runtime.GOOS,
		Arch:              normalizedArch(),
	})
	if err == nil {
		return resolved, nil
	}
	if strictPlaceholders {
		return "", err
	}
	return value, nil
}

func validatePluginHooks(manifest Manifest, settings map[string]any, runtimeState map[string]any, strictPlaceholders bool) error {
	for _, hook := range manifest.Hooks {
		renderedSpec, err := renderValue(hook.LaunchSpec, settings, runtimeState, strictPlaceholders)
		if err != nil {
			return fmt.Errorf("plugin hook %q launch_spec: %w", hookID(hook), err)
		}
		spec, _ := renderedSpec.(map[string]any)
		hookType, err := renderSettingString(firstNonEmpty(hook.Type, stringFromAnyMap(spec, "type")), settings, runtimeState, strictPlaceholders)
		if err != nil {
			return fmt.Errorf("plugin hook %q type: %w", hookID(hook), err)
		}
		command := hook.Command
		if len(command) == 0 {
			command = stringSliceFromAny(spec["command"])
		}
		commandValue, err := renderValue(command, settings, runtimeState, strictPlaceholders)
		if err != nil {
			return fmt.Errorf("plugin hook %q command: %w", hookID(hook), err)
		}
		argsValue, err := renderValue(hook.Args, settings, runtimeState, strictPlaceholders)
		if err != nil {
			return fmt.Errorf("plugin hook %q args: %w", hookID(hook), err)
		}
		_ = argsValue
		urlValue := firstNonEmpty(hook.URL, stringFromAnyMap(spec, "url"))
		url, err := renderSettingString(urlValue, settings, runtimeState, strictPlaceholders)
		if err != nil {
			return fmt.Errorf("plugin hook %q url: %w", hookID(hook), err)
		}
		if _, err := renderValue(hook.Headers, settings, runtimeState, strictPlaceholders); err != nil {
			return fmt.Errorf("plugin hook %q headers: %w", hookID(hook), err)
		}
		if strings.TrimSpace(hookType) == "" {
			switch {
			case len(stringSliceFromAny(commandValue)) > 0:
				hookType = "command"
			case strings.TrimSpace(url) != "":
				hookType = "http"
			}
		}
		switch strings.TrimSpace(hookType) {
		case "command":
			if len(stringSliceFromAny(commandValue)) == 0 || strings.TrimSpace(stringSliceFromAny(commandValue)[0]) == "" {
				return fmt.Errorf("plugin hook %q command must not be empty", hookID(hook))
			}
		case "http":
			if strings.TrimSpace(url) == "" {
				return fmt.Errorf("plugin hook %q url must not be empty", hookID(hook))
			}
		default:
			return fmt.Errorf("plugin hook %q type %q is unsupported", hookID(hook), hookType)
		}
	}
	return nil
}

func pluginDataRuntimeState(store PluginStore, pluginID, version string) (map[string]any, error) {
	if store == nil {
		return nil, fmt.Errorf("plugin store is not configured")
	}
	root, err := store.Root(pluginID, version)
	if err != nil {
		return nil, err
	}
	return map[string]any{"plugin_data": root}, nil
}

func runtimeStateJSON(values map[string]any) json.RawMessage {
	payload, err := json.Marshal(values)
	if err != nil || len(payload) == 0 {
		return json.RawMessage("{}")
	}
	return payload
}

func runtimeStateStatus(values map[string]any, manifest Manifest) string {
	if len(manifest.Runtime) == 0 {
		return "not_required"
	}
	overall := "installed"
	for _, runtimeConfig := range manifest.Runtime {
		status := strings.TrimSpace(fmt.Sprint(values[runtimeConfig.ID+".status"]))
		if status == "" {
			return "not_installed"
		}
		if status != "installed" {
			overall = status
		}
	}
	return overall
}

func hookID(hook sharedpluginmanifest.HookConfig) string {
	if strings.TrimSpace(hook.ID) != "" {
		return strings.TrimSpace(hook.ID)
	}
	return strings.TrimSpace(hook.Event)
}

func stringFromAnyMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		return []string{text}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func runtimePathSettings(runtimeState map[string]any) map[string]string {
	if len(runtimeState) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, value := range runtimeState {
		key = strings.TrimSpace(key)
		if strings.HasSuffix(key, ".path") {
			runtimeID := strings.TrimSuffix(key, ".path")
			if runtimeID != "" {
				out[runtimeID] = fmt.Sprint(value)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func runtimePropertySettings(runtimeState map[string]any) map[string]map[string]string {
	if len(runtimeState) == 0 {
		return nil
	}
	out := make(map[string]map[string]string)
	for key, value := range runtimeState {
		key = strings.TrimSpace(key)
		index := strings.LastIndex(key, ".")
		if index <= 0 || index == len(key)-1 {
			continue
		}
		runtimeID := key[:index]
		field := key[index+1:]
		if runtimeID == "" || field == "" {
			continue
		}
		if out[runtimeID] == nil {
			out[runtimeID] = map[string]string{}
		}
		out[runtimeID][field] = fmt.Sprint(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringFromPluginMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(values[key]))
}

func ensureDefaultWorkspace(ctx context.Context, profileRepo *data.ProfileRegistriesRepository, workspaceRepo *data.WorkspaceRegistriesRepository, accountID, userID uuid.UUID, profileRef string) (string, error) {
	if err := profileRepo.Ensure(ctx, profileRef, accountID, userID); err != nil {
		return "", err
	}
	profile, err := profileRepo.Get(ctx, profileRef)
	if err != nil {
		return "", err
	}
	if profile != nil && profile.DefaultWorkspaceRef != nil && strings.TrimSpace(*profile.DefaultWorkspaceRef) != "" {
		workspaceRef := strings.TrimSpace(*profile.DefaultWorkspaceRef)
		if err := workspaceRepo.Ensure(ctx, workspaceRef, accountID, userID, nil); err != nil {
			return "", err
		}
		return workspaceRef, nil
	}
	workspaceRef := "wsref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := workspaceRepo.Ensure(ctx, workspaceRef, accountID, userID, nil); err != nil {
		return "", err
	}
	if err := profileRepo.SetDefaultWorkspaceRef(ctx, profileRef, workspaceRef); err != nil {
		return "", err
	}
	return workspaceRef, nil
}

func loadManifestPayload(ctx context.Context, req InstallRequest) ([]byte, string, string, func(), error) {
	if len(req.ManifestJSON) > 0 {
		return req.ManifestJSON, "", "", func() {}, nil
	}
	manifestPath := strings.TrimSpace(req.ManifestPath)
	if manifestPath != "" {
		return loadManifestPath(manifestPath)
	}
	sourceKind := strings.TrimSpace(req.SourceKind)
	sourceURI := strings.TrimSpace(req.SourceURI)
	if (sourceKind == "url" || sourceKind == "manifest_url") && sourceURI != "" {
		if !isPluginSourceURL(sourceURI) {
			return nil, "", "", func() {}, fmt.Errorf("plugin manifest url is invalid")
		}
		payload, err := fetchPluginManifestURL(ctx, sourceURI)
		return payload, sourceURI, "", func() {}, err
	}
	if sourceKind == "registry" && sourceURI != "" {
		return loadRegistryManifestPayload(ctx, sourceURI)
	}
	return nil, "", "", func() {}, fmt.Errorf("manifest is required")
}

func loadManifestPath(manifestPath string) ([]byte, string, string, func(), error) {
	info, err := os.Stat(manifestPath)
	if err != nil {
		return nil, "", "", func() {}, err
	}
	if info.IsDir() {
		for _, name := range []string{"manifest.yaml", "manifest.yml", ".codex-plugin/plugin.json", "plugin.json", "manifest.json"} {
			candidate := filepath.Join(manifestPath, name)
			if data, err := os.ReadFile(candidate); err == nil {
				return data, candidate, manifestPath, func() {}, nil
			}
		}
		return nil, "", "", func() {}, fmt.Errorf("plugin manifest not found")
	}
	if isPluginBundlePath(manifestPath) {
		bundle, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, "", "", func() {}, err
		}
		manifestData, root, err := extractRegistryBundle(bundle)
		if err != nil {
			return nil, "", "", func() {}, err
		}
		return manifestData, manifestPath, root, func() { _ = os.RemoveAll(root) }, nil
	}
	data, err := os.ReadFile(manifestPath)
	return data, manifestPath, filepath.Dir(manifestPath), func() {}, err
}

func loadRegistryManifestPayload(ctx context.Context, source string) ([]byte, string, string, func(), error) {
	if isPluginSourceURL(source) {
		return nil, "", "", func() {}, fmt.Errorf("registry source must be a plugin id")
	}
	registryURL := strings.TrimSpace(os.Getenv("ARKLOOP_PLUGIN_REGISTRY_URL"))
	client := pluginregistry.Client{BaseURL: registryURL}
	version, err := client.GetLatestVersion(ctx, source)
	if err != nil {
		return nil, "", "", func() {}, err
	}
	bundle, bundleErr := client.GetBundle(ctx, version.PluginID, version.Version)
	if bundleErr == nil && len(bundle) > 0 {
		if err := verifyRegistryBundle(bundle, version.BundleSHA256); err != nil {
			return nil, "", "", func() {}, err
		}
		manifestData, root, err := extractRegistryBundle(bundle)
		if err != nil {
			return nil, "", "", func() {}, err
		}
		return manifestData, registrySourceURI(registryURL, version.PluginID, version.Version), root, func() { _ = os.RemoveAll(root) }, nil
	}
	if len(version.Manifest) > 0 {
		return append([]byte(nil), version.Manifest...), registrySourceURI(registryURL, version.PluginID, version.Version), "", func() {}, nil
	}
	manifestData, err := client.GetManifest(ctx, version.PluginID, version.Version)
	return manifestData, registrySourceURI(registryURL, version.PluginID, version.Version), "", func() {}, err
}

func fetchPluginManifestURL(ctx context.Context, source string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("build plugin manifest request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch plugin manifest http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest: %w", err)
	}
	return data, nil
}

func verifyRegistryBundle(bundle []byte, expected string) error {
	expected = strings.TrimSpace(strings.ToLower(expected))
	if expected == "" {
		return nil
	}
	sum := sha256.Sum256(bundle)
	if got := hex.EncodeToString(sum[:]); got != expected {
		return fmt.Errorf("plugin registry bundle sha256 mismatch")
	}
	return nil
}

func extractRegistryBundle(bundle []byte) ([]byte, string, error) {
	root, err := os.MkdirTemp("", "arkloop-plugin-registry-*")
	if err != nil {
		return nil, "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		return nil, "", fmt.Errorf("open plugin bundle: %w", err)
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	var manifestData []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read plugin bundle: %w", err)
		}
		name, err := registryBundlePath(header.Name)
		if err != nil {
			return nil, "", err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
				return nil, "", err
			}
		case tar.TypeReg:
			data, err := io.ReadAll(reader)
			if err != nil {
				return nil, "", fmt.Errorf("read plugin bundle file %q: %w", name, err)
			}
			target := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, "", err
			}
			mode := header.FileInfo().Mode().Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := os.WriteFile(target, data, mode); err != nil {
				return nil, "", err
			}
			if isPluginManifestBundlePath(name) {
				manifestData = data
			}
		default:
			return nil, "", fmt.Errorf("plugin bundle contains unsupported entry %q", name)
		}
	}
	if len(manifestData) == 0 {
		return nil, "", fmt.Errorf("plugin bundle manifest not found")
	}
	cleanup = false
	return manifestData, root, nil
}

func isPluginManifestBundlePath(name string) bool {
	switch strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/") {
	case "manifest.yaml", "manifest.yml", "manifest.json", "plugin.json", ".codex-plugin/plugin.json":
		return true
	default:
		return false
	}
}

func registryBundlePath(name string) (string, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("plugin bundle path must be relative")
	}
	name = strings.Trim(raw, "/")
	if name == "" || name == "." {
		return "", fmt.Errorf("plugin bundle path is invalid")
	}
	cleaned := filepath.ToSlash(filepath.Clean(name))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("plugin bundle path escapes root")
	}
	return cleaned, nil
}

func registrySourceURI(baseURL, pluginID, version string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/v1/plugins/" + url.PathEscape(pluginID) + "/versions/" + url.PathEscape(version)
}

func isPluginSourceURL(source string) bool {
	parsed, err := url.Parse(strings.TrimSpace(source))
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func isPluginBundlePath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".tgz")
}

func normalizeSettings(settings map[string]any, manifest Manifest) (json.RawMessage, map[string]any, error) {
	rules := pluginSettingRules(manifest)
	out := make(map[string]any, len(rules))
	for key, rule := range rules {
		if rule.defaultValue != nil {
			normalized, err := normalizeSettingValue(key, rule.defaultValue, rule)
			if err != nil {
				return nil, nil, err
			}
			out[key] = normalized
		}
	}
	for key, value := range settings {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		rule, ok := rules[key]
		if len(rules) > 0 && !ok {
			return nil, nil, fmt.Errorf("plugin setting %q is not defined", key)
		}
		if ok {
			normalized, err := normalizeSettingValue(key, value, rule)
			if err != nil {
				return nil, nil, err
			}
			out[key] = normalized
			continue
		}
		out[key] = value
	}
	for key, rule := range rules {
		if rule.required {
			if _, ok := out[key]; !ok {
				return nil, nil, fmt.Errorf("plugin setting %q is required", key)
			}
		}
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return payload, out, nil
}

type pluginSettingRule struct {
	settingType  string
	defaultValue any
	required     bool
	options      map[string]struct{}
}

func pluginSettingRules(manifest Manifest) map[string]pluginSettingRule {
	rules := make(map[string]pluginSettingRule)
	for _, setting := range manifest.Settings {
		key := strings.TrimSpace(setting.Key)
		if key == "" {
			continue
		}
		rule := pluginSettingRule{
			settingType:  strings.TrimSpace(setting.Type),
			defaultValue: setting.Default,
			required:     setting.Required,
		}
		if len(setting.Options) > 0 {
			rule.options = make(map[string]struct{}, len(setting.Options))
			for _, option := range setting.Options {
				rule.options[strings.TrimSpace(option)] = struct{}{}
			}
		}
		rules[key] = rule
	}
	for key, raw := range manifest.SettingsSchema {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		rule := rules[key]
		if spec, ok := raw.(map[string]any); ok {
			if text, ok := spec["type"].(string); ok && strings.TrimSpace(text) != "" {
				rule.settingType = strings.TrimSpace(text)
			}
			if value, ok := spec["default"]; ok {
				rule.defaultValue = value
			}
			if value, ok := spec["required"].(bool); ok {
				rule.required = value
			}
			if options := settingOptions(spec["options"]); len(options) > 0 {
				rule.options = options
			}
		}
		rules[key] = rule
	}
	return rules
}

func settingOptions(value any) map[string]struct{} {
	switch typed := value.(type) {
	case []any:
		out := make(map[string]struct{}, len(typed))
		for _, item := range typed {
			option := strings.TrimSpace(fmt.Sprint(item))
			if option != "" {
				out[option] = struct{}{}
			}
		}
		return out
	case []string:
		out := make(map[string]struct{}, len(typed))
		for _, item := range typed {
			option := strings.TrimSpace(item)
			if option != "" {
				out[option] = struct{}{}
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeSettingValue(key string, value any, rule pluginSettingRule) (any, error) {
	switch rule.settingType {
	case "", "string", "text", "password", "path", "url", "select":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("plugin setting %q must be a string", key)
		}
		if len(rule.options) > 0 {
			if _, ok := rule.options[text]; !ok {
				return nil, fmt.Errorf("plugin setting %q value is not allowed", key)
			}
		}
		return text, nil
	case "boolean":
		value, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("plugin setting %q must be a boolean", key)
		}
		return value, nil
	case "number":
		switch typed := value.(type) {
		case int, int64, float64, float32:
			return typed, nil
		default:
			return nil, fmt.Errorf("plugin setting %q must be a number", key)
		}
	case "integer":
		switch typed := value.(type) {
		case int, int64:
			return typed, nil
		case float64:
			if typed == float64(int64(typed)) {
				return typed, nil
			}
		}
		return nil, fmt.Errorf("plugin setting %q must be an integer", key)
	case "array":
		if _, ok := value.([]any); !ok {
			return nil, fmt.Errorf("plugin setting %q must be an array", key)
		}
		return value, nil
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("plugin setting %q must be an object", key)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("plugin setting %q type is invalid", key)
	}
}

func pluginInstallKey(pluginID, serverID string) string {
	return sharedpluginmanifest.PluginInstallKey(pluginID, serverID)
}

func stringSettings(settings map[string]any) map[string]string {
	if len(settings) == 0 {
		return nil
	}
	out := make(map[string]string, len(settings))
	for key, value := range settings {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = fmt.Sprint(value)
		}
	}
	return out
}

func validatePluginHost(manifest Manifest) error {
	if pluginHostModeDesktop {
		return nil
	}
	if err := validatePluginHostRequirement(manifest.HostRequirement); err != nil {
		return err
	}
	for _, server := range manifest.MCPServers {
		if err := validatePluginHostRequirement(server.HostRequirement); err != nil {
			return err
		}
	}
	return nil
}

func validatePluginHostRequirement(requirement string) error {
	requirement = strings.TrimSpace(requirement)
	switch requirement {
	case data.MCPHostRequirementDesktopLocal, data.MCPHostRequirementDesktopSidecar:
		return fmt.Errorf("plugin host_requirement %q is only available in desktop mode", requirement)
	default:
		return nil
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
