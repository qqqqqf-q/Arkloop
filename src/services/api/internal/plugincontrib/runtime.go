package plugincontrib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/shared/pluginbinary"
	"arkloop/services/shared/pluginmanifest"
	"github.com/jackc/pgx/v5"
)

const runtimeInstallDownloadTimeout = 5 * time.Minute
const runtimeCheckTimeout = 15 * time.Second
const cuaPluginID = "arkloop.plugins.cua"
const cuaRuntimeID = "cua-driver"

func (e *Enabler) InstallRuntime(ctx context.Context, req EnableRequest) (data.PluginRuntimeState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, manifest, profileRef, workspaceRef, err := e.resolveScope(ctx, req)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	if err := validatePluginHost(manifest); err != nil {
		return data.PluginRuntimeState{}, err
	}
	if len(manifest.Runtime) == 0 {
		return data.PluginRuntimeState{}, fmt.Errorf("plugin has no runtime")
	}
	pluginData, err := e.pluginStore.Root(pkg.PluginID, pkg.Version)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	statusMap := map[string]any{"plugin_data": pluginData}
	overall := "installed"
	for _, runtimeConfig := range manifest.Runtime {
		if err := e.installRuntimeBinary(ctx, pkg, runtimeConfig); err != nil {
			overall = "error"
			statusMap[runtimeConfig.ID+".error"] = err.Error()
			continue
		}
		result := pluginbinary.DetectRuntime(ctx, runtimeConfig, pluginbinary.DetectOptions{
			InstallRoot: pluginData,
			Resolver: pluginmanifest.PlaceholderContext{
				PluginData: pluginData,
				Platform:   runtime.GOOS,
				Arch:       normalizedArch(),
			},
		})
		statusMap[runtimeConfig.ID+".status"] = string(result.Status)
		if strings.TrimSpace(result.Path) != "" {
			statusMap[runtimeConfig.ID+".path"] = result.Path
			statusMap[runtimeConfig.ID+".command"] = result.Path
		}
		if strings.TrimSpace(result.HelperAppPath) != "" {
			statusMap[runtimeConfig.ID+".helper_app_path"] = result.HelperAppPath
			statusMap[runtimeConfig.ID+".helperAppPath"] = result.HelperAppPath
		}
		if strings.TrimSpace(result.HelperAppName) != "" {
			statusMap[runtimeConfig.ID+".helper_app_name"] = result.HelperAppName
			statusMap[runtimeConfig.ID+".helperAppName"] = result.HelperAppName
		}
		if strings.TrimSpace(result.HelperAppBundleID) != "" {
			statusMap[runtimeConfig.ID+".helper_app_bundle_id"] = result.HelperAppBundleID
			statusMap[runtimeConfig.ID+".helperAppBundleID"] = result.HelperAppBundleID
		}
		if strings.TrimSpace(result.Version) != "" {
			statusMap[runtimeConfig.ID+".version"] = result.Version
		}
		if strings.TrimSpace(result.Error) != "" {
			statusMap[runtimeConfig.ID+".error"] = result.Error
		}
		if result.Status != pluginbinary.StatusInstalled && overall != "error" {
			overall = string(result.Status)
		}
	}
	statusJSON, err := json.Marshal(statusMap)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := e.runtimeRepo.WithTx(tx).Upsert(ctx, data.PluginRuntimeState{
		AccountID:     req.AccountID,
		PackageID:     pkg.ID,
		PluginID:      pkg.PluginID,
		PluginVersion: pkg.Version,
		ProfileRef:    profileRef,
		WorkspaceRef:  workspaceRef,
		Status:        overall,
		StatusJSON:    statusJSON,
	})
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	settings := map[string]any{}
	enabled := false
	if enablement, getErr := e.enablementsRepo.Get(ctx, req.AccountID, pkg.ID, profileRef, workspaceRef); getErr == nil && enablement != nil {
		settings = decodePluginJSONMap(enablement.SettingsJSON)
		enabled = enablement.Enabled
	}
	_, settings, err = normalizeSettings(settings, manifest)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	req.Enabled = enabled
	if err := e.syncDerivedResources(ctx, tx, req, manifest, profileRef, workspaceRef, settings, statusMap, false); err != nil {
		return data.PluginRuntimeState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return data.PluginRuntimeState{}, err
	}
	return state, nil
}

func (e *Enabler) CheckRuntime(ctx context.Context, req EnableRequest) (data.PluginRuntimeState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, manifest, profileRef, workspaceRef, err := e.resolveScope(ctx, req)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	if err := validatePluginHost(manifest); err != nil {
		return data.PluginRuntimeState{}, err
	}
	statusMap, overall, err := e.detectRuntimeState(ctx, pkg, manifest)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	current, err := e.runtimeRepo.Get(ctx, req.AccountID, pkg.ID, profileRef, workspaceRef)
	if err != nil {
		return data.PluginRuntimeState{}, err
	}
	preserveRuntimeCheckStatus(statusMap, current)
	if strings.TrimSpace(pkg.PluginID) == cuaPluginID {
		checkCUAPermissions(ctx, statusMap)
	}
	return e.runtimeRepo.Upsert(ctx, data.PluginRuntimeState{
		AccountID:     req.AccountID,
		PackageID:     pkg.ID,
		PluginID:      pkg.PluginID,
		PluginVersion: pkg.Version,
		ProfileRef:    profileRef,
		WorkspaceRef:  workspaceRef,
		Status:        overall,
		StatusJSON:    runtimeStateJSON(statusMap),
	})
}

func (e *Enabler) detectRuntimeState(ctx context.Context, pkg data.PluginPackage, manifest Manifest) (map[string]any, string, error) {
	pluginData, err := e.pluginStore.Root(pkg.PluginID, pkg.Version)
	if err != nil {
		return nil, "", err
	}
	statusMap := map[string]any{"plugin_data": pluginData}
	overall := "installed"
	for _, runtimeConfig := range manifest.Runtime {
		result := pluginbinary.DetectRuntime(ctx, runtimeConfig, pluginbinary.DetectOptions{
			InstallRoot: pluginData,
			Resolver: pluginmanifest.PlaceholderContext{
				PluginData: pluginData,
				Platform:   runtime.GOOS,
				Arch:       normalizedArch(),
			},
		})
		statusMap[runtimeConfig.ID+".status"] = string(result.Status)
		if strings.TrimSpace(result.Path) != "" {
			statusMap[runtimeConfig.ID+".path"] = result.Path
			statusMap[runtimeConfig.ID+".command"] = result.Path
		}
		if strings.TrimSpace(result.HelperAppPath) != "" {
			statusMap[runtimeConfig.ID+".helper_app_path"] = result.HelperAppPath
			statusMap[runtimeConfig.ID+".helperAppPath"] = result.HelperAppPath
		}
		if strings.TrimSpace(result.HelperAppName) != "" {
			statusMap[runtimeConfig.ID+".helper_app_name"] = result.HelperAppName
			statusMap[runtimeConfig.ID+".helperAppName"] = result.HelperAppName
		}
		if strings.TrimSpace(result.HelperAppBundleID) != "" {
			statusMap[runtimeConfig.ID+".helper_app_bundle_id"] = result.HelperAppBundleID
			statusMap[runtimeConfig.ID+".helperAppBundleID"] = result.HelperAppBundleID
		}
		if strings.TrimSpace(result.Version) != "" {
			statusMap[runtimeConfig.ID+".version"] = result.Version
		}
		if strings.TrimSpace(result.Error) != "" {
			statusMap[runtimeConfig.ID+".error"] = result.Error
		}
		if result.Status != pluginbinary.StatusInstalled && overall != "error" {
			overall = string(result.Status)
		}
	}
	return statusMap, overall, nil
}

func preserveRuntimeCheckStatus(next map[string]any, current *data.PluginRuntimeState) bool {
	if current == nil || len(current.StatusJSON) == 0 {
		return false
	}
	var previous map[string]any
	if err := json.Unmarshal(current.StatusJSON, &previous); err != nil {
		return false
	}
	copied := false
	for key, value := range previous {
		if strings.Contains(key, ".permissions.") {
			next[key] = value
			copied = true
		}
	}
	return copied
}

func checkCUAPermissions(ctx context.Context, statusMap map[string]any) {
	prefix := cuaRuntimeID + ".permissions."
	command := strings.TrimSpace(fmt.Sprint(statusMap[cuaRuntimeID+".command"]))
	statusMap[prefix+"checked_at"] = time.Now().UTC().Format(time.RFC3339)
	delete(statusMap, prefix+"error")
	if command == "" {
		statusMap[prefix+"error"] = "runtime command missing"
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, runtimeCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, command, "diagnose")
	cmd.Env = append(os.Environ(), "CUA_DRIVER_AUTO_UPDATE_ENABLED=0")
	output, err := cmd.CombinedOutput()
	raw := strings.TrimSpace(string(output))
	accessibility, accessibilityOK := parseCUAPermission(raw, "accessibility")
	screenRecording, screenRecordingOK := parseCUAPermission(raw, "screen recording")
	if accessibilityOK {
		statusMap[prefix+"accessibility"] = accessibility
	}
	if screenRecordingOK {
		statusMap[prefix+"screen_recording"] = screenRecording
	}
	if checkCtx.Err() != nil {
		statusMap[prefix+"error"] = checkCtx.Err().Error()
		return
	}
	if err != nil {
		statusMap[prefix+"error"] = err.Error()
	}
}

func parseCUAPermission(output, name string) (bool, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, line := range strings.Split(output, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if !strings.Contains(line, name) {
			continue
		}
		switch {
		case strings.Contains(line, "granted") && !strings.Contains(line, "not granted"), strings.Contains(line, ":") && strings.HasSuffix(line, "true"):
			return true, true
		case strings.Contains(line, "denied"), strings.Contains(line, "missing"), strings.Contains(line, "not granted"), strings.Contains(line, ":") && strings.HasSuffix(line, "false"):
			return false, true
		}
	}
	return false, false
}

func (e *Enabler) installRuntimeBinary(ctx context.Context, pkg data.PluginPackage, runtimeConfig pluginmanifest.RuntimeConfig) error {
	binary, ok := selectRuntimeBinary(runtimeConfig.Binary, runtime.GOOS, normalizedArch())
	if !ok {
		return nil
	}
	client := sharedoutbound.DefaultPolicy().NewHTTPClient(runtimeInstallDownloadTimeout)
	return pluginbinary.DownloadAndExtract(ctx, client, e.pluginStore, pkg.PluginID, pkg.Version, pluginbinary.DownloadConfig{
		URL:        binary.URL,
		SHA256:     binary.SHA256,
		TargetDir:  "runtime",
		TargetPath: binary.Path,
	})
}

func selectRuntimeBinary(binaries []pluginmanifest.RuntimeBinaryConfig, platform, arch string) (pluginmanifest.RuntimeBinaryConfig, bool) {
	key := platform + "-" + arch
	for _, binary := range binaries {
		if strings.TrimSpace(binary.URL) == "" {
			continue
		}
		if strings.TrimSpace(binary.Platform) == key {
			return binary, true
		}
	}
	return pluginmanifest.RuntimeBinaryConfig{}, false
}

func normalizedArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "amd64":
		return "amd64"
	default:
		return runtime.GOARCH
	}
}

func decodePluginJSONMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
