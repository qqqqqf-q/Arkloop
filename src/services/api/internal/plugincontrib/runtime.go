package plugincontrib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/shared/pluginbinary"
	"arkloop/services/shared/pluginmanifest"
	"github.com/google/uuid"
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
	applyManifestRuntimeDefaults(manifest, statusMap)
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
	prepareActivityRecorderSources(ctx, pkg.PluginID, settings, statusMap)
	req.Enabled = enabled
	if enabled {
		startRuntimeDaemons(ctx, manifest, settings, statusMap)
		state, err = e.runtimeRepo.WithTx(tx).Upsert(ctx, data.PluginRuntimeState{
			AccountID:     req.AccountID,
			PackageID:     pkg.ID,
			PluginID:      pkg.PluginID,
			PluginVersion: pkg.Version,
			ProfileRef:    profileRef,
			WorkspaceRef:  workspaceRef,
			Status:        runtimeStateStatus(statusMap, manifest),
			StatusJSON:    runtimeStateJSON(statusMap),
		})
		if err != nil {
			return data.PluginRuntimeState{}, err
		}
	}
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
	settings := runtimeSettingsFromEnablement(ctx, e, req.AccountID, pkg.ID, profileRef, workspaceRef, manifest)
	prepareActivityRecorderSources(ctx, pkg.PluginID, settings, statusMap)
	checkRuntimeDaemons(ctx, manifest, settings, statusMap)
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

func (e *Enabler) ResumeEnabledRuntimes(ctx context.Context, accountID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return fmt.Errorf("account_id must not be empty")
	}
	packages, err := e.packagesRepo.ListActive(ctx, accountID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, pkg := range packages {
		manifest, _, err := decodeManifest(pkg.ManifestJSON)
		if err != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("%s: %w", pkg.PluginID, err))
			continue
		}
		if len(manifest.Runtime) == 0 {
			continue
		}
		if err := validatePluginHost(manifest); err != nil {
			continue
		}
		enablements, err := e.enablementsRepo.ListByPlugin(ctx, accountID, manifest.ID)
		if err != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("%s: %w", manifest.ID, err))
			continue
		}
		for _, enablement := range enablements {
			if !enablement.Enabled || enablement.PackageID != pkg.ID {
				continue
			}
			if err := e.resumeEnabledRuntime(ctx, pkg, manifest, enablement); err != nil {
				firstErr = errors.Join(firstErr, fmt.Errorf("%s: %w", manifest.ID, err))
			}
		}
	}
	return firstErr
}

func (e *Enabler) resumeEnabledRuntime(ctx context.Context, pkg data.PluginPackage, manifest Manifest, enablement data.PluginEnablement) error {
	settingsPayload := decodePluginJSONMap(enablement.SettingsJSON)
	settingsPayload = normalizeRuntimeSettings(manifest, settingsPayload)
	_, settings, err := normalizeSettings(settingsPayload, manifest)
	if err != nil {
		return err
	}
	runtimeState, _, err := e.applyRuntimeState(ctx, enablement.AccountID, pkg, manifest, enablement.ProfileRef, enablement.WorkspaceRef, true)
	if err != nil {
		return err
	}
	prepareActivityRecorderSources(ctx, pkg.PluginID, settings, runtimeState)
	startRuntimeDaemons(ctx, manifest, settings, runtimeState)
	_, err = e.runtimeRepo.Upsert(ctx, data.PluginRuntimeState{
		AccountID:     enablement.AccountID,
		PackageID:     pkg.ID,
		PluginID:      pkg.PluginID,
		PluginVersion: pkg.Version,
		ProfileRef:    enablement.ProfileRef,
		WorkspaceRef:  enablement.WorkspaceRef,
		Status:        runtimeStateStatus(runtimeState, manifest),
		StatusJSON:    runtimeStateJSON(runtimeState),
	})
	return err
}

func startRuntimeDaemons(ctx context.Context, manifest Manifest, settings, runtimeState map[string]any) {
	for _, runtimeConfig := range manifest.Runtime {
		for _, daemon := range runtimeDaemons(runtimeConfig) {
			if !runtimeConditionsMatch(settings, daemon.EnabledWhen) {
				markRuntimeDaemonDisabled(runtimeConfig, daemon, runtimeState)
				continue
			}
			startRuntimeDaemon(ctx, runtimeConfig, daemon, settings, runtimeState)
		}
	}
}

func stopRuntimeDaemons(ctx context.Context, manifest Manifest, runtimeState map[string]any) {
	for _, runtimeConfig := range manifest.Runtime {
		for _, daemon := range runtimeDaemons(runtimeConfig) {
			key := daemonStateKey(runtimeConfig.ID, daemon)
			prefix := key + ".daemon."
			delete(runtimeState, prefix+"error")
			pid := daemonPID(runtimeState, key)
			if processRunning(pid) {
				_ = killDaemonProcess(pid)
			}
			removeDaemonPID(runtimeState, key)
			runtimeState[prefix+"status"] = "stopped"
			runtimeState[prefix+"stopped_at"] = time.Now().UTC().Format(time.RFC3339)
		}
	}
	_ = ctx
}

func checkRuntimeDaemons(ctx context.Context, manifest Manifest, settings map[string]any, runtimeState map[string]any) {
	for _, runtimeConfig := range manifest.Runtime {
		for _, daemon := range runtimeDaemons(runtimeConfig) {
			if !runtimeConditionsMatch(settings, daemon.EnabledWhen) {
				markRuntimeDaemonDisabled(runtimeConfig, daemon, runtimeState)
				continue
			}
			key := daemonStateKey(runtimeConfig.ID, daemon)
			prefix := key + ".daemon."
			delete(runtimeState, prefix+"error")
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			healthy := runtimeDaemonHealthyOnce(checkCtx, runtimeConfig, daemon, runtimeState)
			cancel()
			if healthy {
				runtimeState[prefix+"status"] = "running"
			} else if pid := daemonPID(runtimeState, key); processRunning(pid) {
				if strings.TrimSpace(daemon.HealthURL) == "" {
					runtimeState[prefix+"status"] = "running"
				} else {
					runtimeState[prefix+"status"] = "unknown"
				}
			} else {
				removeDaemonPID(runtimeState, key)
				runtimeState[prefix+"status"] = "stopped"
			}
			runtimeState[prefix+"checked_at"] = time.Now().UTC().Format(time.RFC3339)
		}
	}
}

func runtimeSettingsFromEnablement(ctx context.Context, e *Enabler, accountID uuid.UUID, packageID uuid.UUID, profileRef, workspaceRef string, manifest Manifest) map[string]any {
	settings := map[string]any{}
	if e != nil && e.enablementsRepo != nil {
		if enablement, err := e.enablementsRepo.Get(ctx, accountID, packageID, profileRef, workspaceRef); err == nil && enablement != nil {
			settings = decodePluginJSONMap(enablement.SettingsJSON)
		}
	}
	settings = normalizeRuntimeSettings(manifest, settings)
	_, normalized, err := normalizeSettings(settings, manifest)
	if err != nil {
		return settings
	}
	return normalized
}

func markRuntimeDaemonDisabled(runtimeConfig pluginmanifest.RuntimeConfig, daemon pluginmanifest.RuntimeDaemonConfig, runtimeState map[string]any) {
	key := daemonStateKey(runtimeConfig.ID, daemon)
	prefix := key + ".daemon."
	pid := daemonPID(runtimeState, key)
	if processRunning(pid) {
		_ = killDaemonProcess(pid)
	}
	removeDaemonPID(runtimeState, key)
	delete(runtimeState, prefix+"error")
	runtimeState[prefix+"status"] = "disabled"
	runtimeState[prefix+"checked_at"] = time.Now().UTC().Format(time.RFC3339)
}

func startRuntimeDaemon(ctx context.Context, runtimeConfig pluginmanifest.RuntimeConfig, daemon pluginmanifest.RuntimeDaemonConfig, settings, runtimeState map[string]any) {
	key := daemonStateKey(runtimeConfig.ID, daemon)
	prefix := key + ".daemon."
	delete(runtimeState, prefix+"error")
	if runtimeDaemonHealthyOnce(ctx, runtimeConfig, daemon, runtimeState) {
		runtimeState[prefix+"status"] = "running"
		return
	}
	if pid := daemonPID(runtimeState, key); processRunning(pid) {
		if strings.TrimSpace(daemon.HealthURL) == "" {
			runtimeState[prefix+"status"] = "running"
		} else {
			runtimeState[prefix+"status"] = "unknown"
		}
		return
	}
	removeDaemonPID(runtimeState, key)
	command, args, env, workingDir, err := renderDaemonLaunch(runtimeConfig, daemon, settings, runtimeState)
	if err != nil {
		runtimeState[prefix+"status"] = "error"
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	cmd := exec.CommandContext(detachedContext(ctx), command, args...)
	configureDaemonCommand(cmd)
	cmd.Env = append(os.Environ(), env...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	logFile, logPath, logErr := openDaemonLog(runtimeState, key)
	if logErr != nil {
		runtimeState[prefix+"log_error"] = logErr.Error()
	} else {
		runtimeState[prefix+"log_path"] = logPath
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		runtimeState[prefix+"status"] = "error"
		runtimeState[prefix+"error"] = err.Error()
		return
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	runtimeState[prefix+"pid"] = cmd.Process.Pid
	runtimeState[prefix+"started_at"] = time.Now().UTC().Format(time.RFC3339)
	runtimeState[prefix+"status"] = "starting"
	writeDaemonPID(runtimeState, key, cmd.Process.Pid)
	go func() {
		_ = cmd.Wait()
	}()
}

func renderDaemonLaunch(runtimeConfig pluginmanifest.RuntimeConfig, daemon pluginmanifest.RuntimeDaemonConfig, settings, runtimeState map[string]any) (string, []string, []string, string, error) {
	command, err := renderSettingString(daemon.Command, settings, runtimeState, true)
	if err != nil {
		return "", nil, nil, "", err
	}
	command = resolveDaemonCommandPath(command)
	args := make([]string, 0, len(daemon.Args))
	for _, arg := range daemon.Args {
		rendered, err := renderSettingString(arg, settings, runtimeState, true)
		if err != nil {
			return "", nil, nil, "", err
		}
		args = append(args, rendered)
	}
	for _, condition := range daemon.ArgsWhen {
		if !daemonArgsConditionMatches(settings, condition) {
			continue
		}
		for _, arg := range condition.Args {
			rendered, err := renderSettingString(arg, settings, runtimeState, true)
			if err != nil {
				return "", nil, nil, "", err
			}
			args = append(args, rendered)
		}
	}
	env := make([]string, 0, len(daemon.Env))
	for key, value := range daemon.Env {
		rendered, err := renderSettingString(value, settings, runtimeState, true)
		if err != nil {
			return "", nil, nil, "", err
		}
		env = append(env, key+"="+rendered)
	}
	workingDir := ""
	if strings.TrimSpace(daemon.WorkingDir) != "" {
		workingDir, err = renderSettingString(daemon.WorkingDir, settings, runtimeState, true)
		if err != nil {
			return "", nil, nil, "", err
		}
		if !filepath.IsAbs(workingDir) {
			workingDir = filepath.Join(stringFromPluginMap(runtimeState, "plugin_data"), workingDir)
		}
	}
	_ = runtimeConfig
	return command, args, env, workingDir, nil
}

func daemonArgsConditionMatches(settings map[string]any, condition pluginmanifest.RuntimeArgsWhen) bool {
	key := strings.TrimSpace(condition.Setting)
	if key == "" {
		return false
	}
	actual, ok := settings[key]
	if !ok {
		return false
	}
	return fmt.Sprint(actual) == fmt.Sprint(condition.Equals)
}

func runtimeConditionsMatch(settings map[string]any, conditions []pluginmanifest.RuntimeCondition) bool {
	for _, condition := range conditions {
		key := strings.TrimSpace(condition.Setting)
		if key == "" {
			return false
		}
		actual, ok := settings[key]
		if !ok || fmt.Sprint(actual) != fmt.Sprint(condition.Equals) {
			return false
		}
	}
	return true
}

func resolveDaemonCommandPath(command string) string {
	command = strings.TrimSpace(command)
	if command == "" || !filepath.IsAbs(command) {
		return command
	}
	if _, err := os.Stat(command); err == nil {
		return command
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(command), ".exe") {
		if _, err := os.Stat(command + ".exe"); err == nil {
			return command + ".exe"
		}
	}
	return command
}

func openDaemonLog(runtimeState map[string]any, runtimeID string) (*os.File, string, error) {
	pluginData := stringFromPluginMap(runtimeState, "plugin_data")
	if pluginData == "" || strings.TrimSpace(runtimeID) == "" {
		return nil, "", fmt.Errorf("plugin_data missing")
	}
	logDir := filepath.Join(pluginData, "runtime", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", err
	}
	logPath := filepath.Join(logDir, safeDaemonLogName(runtimeID)+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, "", err
	}
	return file, logPath, nil
}

func safeDaemonLogName(value string) string {
	value = strings.TrimSpace(value)
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, value)
}

func runtimeDaemonHealthyOnce(ctx context.Context, runtimeConfig pluginmanifest.RuntimeConfig, daemon pluginmanifest.RuntimeDaemonConfig, runtimeState map[string]any) bool {
	if strings.TrimSpace(daemon.HealthURL) == "" {
		return false
	}
	url, err := renderSettingString(daemon.HealthURL, nil, runtimeState, true)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	expected := daemon.HealthStatus
	if expected == 0 {
		expected = http.StatusOK
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	_ = runtimeConfig
	return resp.StatusCode == expected
}

func runtimeDaemons(runtimeConfig pluginmanifest.RuntimeConfig) []pluginmanifest.RuntimeDaemonConfig {
	out := make([]pluginmanifest.RuntimeDaemonConfig, 0, len(runtimeConfig.Daemons)+1)
	if runtimeConfig.Daemon != nil {
		out = append(out, *runtimeConfig.Daemon)
	}
	out = append(out, runtimeConfig.Daemons...)
	return out
}

func daemonStateKey(runtimeID string, daemon pluginmanifest.RuntimeDaemonConfig) string {
	if id := strings.TrimSpace(daemon.ID); id != "" {
		return runtimeID + "." + id
	}
	return runtimeID
}

func daemonPID(runtimeState map[string]any, runtimeID string) int {
	if raw := stringFromPluginMap(runtimeState, runtimeID+".daemon.pid"); raw != "" {
		if pid, err := strconv.Atoi(raw); err == nil && pid > 0 {
			return pid
		}
	}
	pidPath := daemonPIDPath(runtimeState, runtimeID)
	if pidPath == "" {
		return 0
	}
	payload, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil || pid <= 0 {
		return 0
	}
	runtimeState[runtimeID+".daemon.pid"] = pid
	return pid
}

func writeDaemonPID(runtimeState map[string]any, runtimeID string, pid int) {
	pidPath := daemonPIDPath(runtimeState, runtimeID)
	if pidPath == "" || pid <= 0 {
		return
	}
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644)
}

func removeDaemonPID(runtimeState map[string]any, runtimeID string) {
	if pidPath := daemonPIDPath(runtimeState, runtimeID); pidPath != "" {
		_ = os.Remove(pidPath)
	}
	delete(runtimeState, runtimeID+".daemon.pid")
}

func daemonPIDPath(runtimeState map[string]any, runtimeID string) string {
	pluginData := stringFromPluginMap(runtimeState, "plugin_data")
	if pluginData == "" || runtimeID == "" {
		return ""
	}
	return filepath.Join(pluginData, "runtime", runtimeID+".pid")
}

func detachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (e *Enabler) detectRuntimeState(ctx context.Context, pkg data.PluginPackage, manifest Manifest) (map[string]any, string, error) {
	pluginData, err := e.pluginStore.Root(pkg.PluginID, pkg.Version)
	if err != nil {
		return nil, "", err
	}
	statusMap := map[string]any{"plugin_data": pluginData}
	applyManifestRuntimeDefaults(manifest, statusMap)
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
		if strings.Contains(key, ".permissions.") || strings.Contains(key, ".daemon.") {
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
