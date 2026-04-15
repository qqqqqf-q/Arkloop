package bridgehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"arkloop/services/bridge/internal/audit"
	"arkloop/services/bridge/internal/docker"
	"arkloop/services/bridge/internal/model"
	"arkloop/services/bridge/internal/module"
	"arkloop/services/bridge/internal/openviking"
	"arkloop/services/bridge/internal/platform"

	"github.com/google/uuid"
)

// AppLogger is a minimal logging interface compatible with the bridge's
// JSONLogger (via an adapter) and the docker.Logger interface.
type AppLogger interface {
	Info(msg string, extra map[string]any)
	Error(msg string, extra map[string]any)
}

// Handler holds the dependencies needed by all API endpoints.
type Handler struct {
	registry   *module.Registry
	compose    *docker.Compose
	operations *docker.OperationStore
	auditLog   *audit.Logger
	appLogger  AppLogger
	modelDL    *model.Downloader
	version    string
	upgradeMu  sync.Mutex
	upgrading  bool
}

// NewHandler creates a Handler with all required dependencies.
func NewHandler(
	registry *module.Registry,
	compose *docker.Compose,
	operations *docker.OperationStore,
	auditLog *audit.Logger,
	logger AppLogger,
	modelDL *model.Downloader,
	version string,
) *Handler {
	return &Handler{
		registry:   registry,
		compose:    compose,
		operations: operations,
		auditLog:   auditLog,
		appLogger:  logger,
		modelDL:    modelDL,
		version:    version,
	}
}

// RegisterRoutes registers all API routes on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/platform/detect", h.platformDetect)
	mux.HandleFunc("GET /v1/modules", h.listModules)
	mux.HandleFunc("GET /v1/modules/{id}", h.getModule)
	mux.HandleFunc("POST /v1/modules/{id}/actions", h.moduleAction)
	mux.HandleFunc("POST /v1/modules/{id}/upgrade", h.moduleUpgrade)
	mux.HandleFunc("GET /v1/operations/{id}/stream", h.streamOperation)
	mux.HandleFunc("POST /v1/operations/{id}/cancel", h.cancelOperation)
	mux.HandleFunc("GET /v1/system/version", h.systemVersion)
	mux.HandleFunc("POST /v1/system/upgrade", h.systemUpgrade)
}

// --- Platform ----------------------------------------------------------

func (h *Handler) platformDetect(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, platform.Detect())
}

// --- Modules -----------------------------------------------------------

const dockerQueryTimeout = 3 * time.Second
const dockerBatchQueryTimeout = 10 * time.Second

func (h *Handler) listModules(w http.ResponseWriter, r *http.Request) {
	defs := h.registry.OptionalModules()
	infos := make([]module.ModuleInfo, 0, len(defs))
	serviceNames := make([]string, 0, len(defs))
	profileSet := make(map[string]struct{})

	for i := range defs {
		if defs[i].ComposeService != "" {
			serviceNames = append(serviceNames, defs[i].ComposeService)
			if defs[i].ComposeProfile != "" {
				profileSet[defs[i].ComposeProfile] = struct{}{}
			}
		}
	}

	profiles := make([]string, 0, len(profileSet))
	for p := range profileSet {
		profiles = append(profiles, p)
	}

	var statuses map[string]string
	batchFailed := false
	if len(serviceNames) > 0 {
		queryCtx, cancel := context.WithTimeout(r.Context(), dockerBatchQueryTimeout)
		defer cancel()

		var err error
		statuses, err = h.compose.ContainerStatuses(queryCtx, serviceNames, profiles)
		if err != nil {
			batchFailed = true
			h.appLogger.Error("batch container status query failed", map[string]any{
				"error": err.Error(),
			})
		}
	}

	for i := range defs {
		var status module.ModuleStatus
		if defs[i].ComposeService == "" {
			status = h.virtualModuleStatus(&defs[i])
		} else if batchFailed {
			status = module.StatusError
		} else if statuses != nil {
			status = mapRawStatus(statuses[defs[i].ComposeService])
		} else {
			status = h.moduleStatus(r.Context(), &defs[i])
		}
		infos = append(infos, h.moduleInfo(r.Context(), &defs[i], status))
	}

	writeJSON(w, http.StatusOK, infos)
}

func (h *Handler) getModule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	def, ok := h.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "module.not_found", fmt.Sprintf("module %q not found", id))
		return
	}

	status := h.moduleStatus(r.Context(), def)
	writeJSON(w, http.StatusOK, h.moduleInfo(r.Context(), def, status))
}

func (h *Handler) moduleInfo(ctx context.Context, def *module.ModuleDefinition, status module.ModuleStatus) module.ModuleInfo {
	info := def.ToModuleInfo(status)
	if status == module.StatusNotInstalled || status == module.StatusError || def.ID != "openviking" {
		return info
	}

	version := h.moduleVersion(ctx, def)
	if version != "" {
		info.Version = version
	}
	return info
}

func (h *Handler) moduleVersion(ctx context.Context, def *module.ModuleDefinition) string {
	if def.ComposeService == "" {
		return ""
	}

	queryCtx, cancel := context.WithTimeout(ctx, dockerQueryTimeout)
	defer cancel()

	image, err := h.compose.ContainerImage(queryCtx, def.ComposeService, def.ComposeProfile)
	if err != nil {
		h.appLogger.Error("container image query failed", map[string]any{
			"module": def.ID,
			"error":  err.Error(),
		})
		return ""
	}
	return imageRefVersion(image)
}

func imageRefVersion(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if digestSep := strings.Index(trimmed, "@"); digestSep >= 0 {
		trimmed = trimmed[:digestSep]
	}
	slash := strings.LastIndex(trimmed, "/")
	colon := strings.LastIndex(trimmed, ":")
	if colon <= slash {
		return ""
	}
	return strings.TrimPrefix(trimmed[colon+1:], "v")
}

// moduleStatus queries Docker for the live status of a module's compose service.
// For virtual modules (no compose service), it delegates to custom status checks.
func (h *Handler) moduleStatus(ctx context.Context, def *module.ModuleDefinition) module.ModuleStatus {
	if def.ComposeService == "" {
		return h.virtualModuleStatus(def)
	}

	queryCtx, cancel := context.WithTimeout(ctx, dockerQueryTimeout)
	defer cancel()

	raw, err := h.compose.ContainerStatus(queryCtx, def.ComposeService, def.ComposeProfile)
	if err != nil {
		h.appLogger.Error("container status query failed", map[string]any{
			"module": def.ID,
			"error":  err.Error(),
		})
		return module.StatusError
	}

	return mapRawStatus(raw)
}

// virtualModuleStatus checks file-based status for virtual modules.
func (h *Handler) virtualModuleStatus(def *module.ModuleDefinition) module.ModuleStatus {
	if def.ID == "prompt-guard" && h.modelDL != nil && h.modelDL.ModelFilesExist() {
		return module.StatusRunning
	}
	return module.StatusNotInstalled
}

// mapRawStatus converts the raw string from Compose.ContainerStatus to a
// typed ModuleStatus.
func mapRawStatus(raw string) module.ModuleStatus {
	switch raw {
	case "running":
		return module.StatusRunning
	case "stopped":
		return module.StatusStopped
	case "error":
		return module.StatusError
	default:
		return module.StatusNotInstalled
	}
}

// --- Module Upgrade ----------------------------------------------------

type upgradeModuleRequest struct {
	Image string `json:"image"`
}

func (h *Handler) moduleUpgrade(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	def, ok := h.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "module.not_found", fmt.Sprintf("module %q not found", id))
		return
	}
	if def.ComposeService == "" {
		writeError(w, http.StatusBadRequest, "module.no_service",
			fmt.Sprintf("module %q has no compose service", id))
		return
	}

	var req upgradeModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request.invalid", "invalid JSON body")
		return
	}
	if req.Image == "" {
		writeError(w, http.StatusBadRequest, "request.invalid", "image is required")
		return
	}

	h.auditLog.LogAction("module_upgrade", id, map[string]string{"image": req.Image})

	op := docker.NewOperation(id, "upgrade")
	op.Status = docker.OperationRunning
	h.operations.Add(op)

	opCtx := context.WithoutCancel(r.Context())
	go func() {
		var opErr error
		defer func() { op.Complete(opErr) }()
		dockerBin, err := docker.ResolveBinary()
		if err != nil {
			op.AppendLog("ERROR: " + err.Error())
			opErr = err
			return
		}

		// 1. 拉取新镜像
		op.AppendLog(fmt.Sprintf("Pulling image %s...", req.Image))
		pullCmd := exec.CommandContext(opCtx, dockerBin, "pull", req.Image)
		if out, err := pullCmd.CombinedOutput(); err != nil {
			op.AppendLog(string(out))
			op.AppendLog("ERROR: docker pull failed: " + err.Error())
			opErr = err
			return
		}
		op.AppendLog("Image pulled successfully")

		// 2. 停止当前服务
		op.AppendLog(fmt.Sprintf("Stopping service %s...", def.ComposeService))
		stopOp, err := h.compose.Stop(opCtx, def.ComposeService)
		if err != nil {
			op.AppendLog("ERROR stopping service: " + err.Error())
			opErr = err
			return
		}
		if waitErr := stopOp.Wait(); waitErr != nil {
			relayLogs(op, stopOp)
			op.AppendLog("ERROR: stop failed: " + waitErr.Error())
			opErr = waitErr
			return
		}
		relayLogs(op, stopOp)
		op.AppendLog("Service stopped")

		// 3. 重启服务
		// 使用 docker-compose override 文件来指定新镜像，避免修改 compose.yaml
		op.AppendLog(fmt.Sprintf("Starting service %s with new image...", def.ComposeService))
		projectDir := h.compose.ProjectDir()
		overrideContent := fmt.Sprintf("services:\n  %s:\n    image: %s\n", def.ComposeService, req.Image)
		overrideFile := filepath.Join(projectDir, "compose.override.yaml")
		if err := os.WriteFile(overrideFile, []byte(overrideContent), 0644); err != nil {
			op.AppendLog("ERROR: failed to write override file: " + err.Error())
			opErr = err
			return
		}
		defer func() { _ = os.Remove(overrideFile) }()

		args := []string{"compose", "-f", "compose.yaml", "-f", "compose.override.yaml", "up", "-d"}
		if def.ComposeProfile != "" {
			args = append(args, "--profile", def.ComposeProfile)
		}
		args = append(args, def.ComposeService)
		upCmd := exec.CommandContext(opCtx, dockerBin, args...)
		upCmd.Dir = projectDir
		if out, err := upCmd.CombinedOutput(); err != nil {
			op.AppendLog(string(out))
			op.AppendLog("ERROR: docker compose up failed: " + err.Error())
			opErr = err
			return
		}
		op.AppendLog("Module upgraded successfully")
	}()

	writeJSON(w, http.StatusAccepted, actionResponse{OperationID: op.ID})
}

// --- Actions -----------------------------------------------------------

type actionRequest struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params,omitempty"`
}

type actionResponse struct {
	OperationID string `json:"operation_id"`
}

var validActions = map[module.ModuleAction]struct{}{
	module.ActionInstall:             {},
	module.ActionStart:               {},
	module.ActionStop:                {},
	module.ActionRestart:             {},
	module.ActionConfigure:           {},
	module.ActionConfigureConnection: {},
	module.ActionBootstrapDefaults:   {},
}

func (h *Handler) moduleAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	def, ok := h.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "module.not_found", fmt.Sprintf("module %q not found", id))
		return
	}

	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request.invalid", "invalid JSON body")
		return
	}

	action := module.ModuleAction(req.Action)
	if _, ok := validActions[action]; !ok {
		writeError(w, http.StatusBadRequest, "action.invalid", fmt.Sprintf("unsupported action %q", req.Action))
		return
	}

	h.auditLog.LogAction(req.Action, id, toStringMap(req.Params))

	var (
		op  *docker.Operation
		err error
	)

	// Use a detached context so the docker compose process survives after
	// the HTTP response is written (the request context would cancel it).
	opCtx := context.WithoutCancel(r.Context())

	switch action {
	case module.ActionInstall:
		if def.ComposeService == "" && def.Virtual {
			op, err = h.handleVirtualInstall(opCtx, id, req.Params)
		} else if def.ComposeService == "" {
			writeError(w, http.StatusBadRequest, "module.no_service",
				fmt.Sprintf("module %q has no compose service", id))
			return
		} else {
			op, err = h.compose.Install(opCtx, def.ComposeService, def.ComposeProfile)
		}
	case module.ActionStart, module.ActionStop, module.ActionRestart:
		if def.ComposeService == "" {
			writeError(w, http.StatusBadRequest, "module.virtual",
				fmt.Sprintf("module %q is virtual and has no compose service", id))
			return
		}
	}

	if op == nil && err == nil {
		switch action {
		case module.ActionStart:
			op, err = h.compose.Start(opCtx, def.ComposeService)
		case module.ActionStop:
			op, err = h.compose.Stop(opCtx, def.ComposeService)
		case module.ActionRestart:
			op, err = h.compose.Restart(opCtx, def.ComposeService)
		case module.ActionConfigure:
			op, err = h.handleConfigure(opCtx, id, def.ComposeService, req.Params)
		case module.ActionConfigureConnection, module.ActionBootstrapDefaults:
			placeholderID := uuid.New().String()
			writeJSON(w, http.StatusAccepted, actionResponse{OperationID: placeholderID})
			return
		}
	}

	if err != nil {
		if strings.Contains(err.Error(), "already has an active operation") {
			writeError(w, http.StatusConflict, "module.busy", err.Error())
			return
		}
		h.appLogger.Error("action failed", map[string]any{
			"module": id,
			"action": req.Action,
			"error":  err.Error(),
		})
		writeError(w, http.StatusInternalServerError, "action.failed", err.Error())
		return
	}

	// 未匹配的 action 类型会导致 op 保持 nil
	if op == nil {
		writeError(w, http.StatusBadRequest, "action.unimplemented",
			fmt.Sprintf("action %q is not implemented for module %q", req.Action, id))
		return
	}

	h.operations.Add(op)
	writeJSON(w, http.StatusAccepted, actionResponse{OperationID: op.ID})
}

// --- Configure ---------------------------------------------------------

const healthCheckTimeout = 30 * time.Second

func (h *Handler) handleConfigure(ctx context.Context, moduleID, composeService string, params map[string]any) (*docker.Operation, error) {
	if moduleID != "openviking" {
		return nil, fmt.Errorf("configure action only supported for openviking module")
	}

	// Parse params into ConfigureParams.
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	var cp openviking.ConfigureParams
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, fmt.Errorf("invalid configure params: %w", err)
	}

	configPath := filepath.Join(h.compose.ProjectDir(), "config", "openviking", "ov.conf")
	healthURL := fmt.Sprintf("http://localhost:%s", envOrDefault("ARKLOOP_OPENVIKING_PORT", "19010"))

	op := docker.NewOperation(moduleID, "configure")
	op.Status = docker.OperationRunning

	go func() {
		var opErr error
		defer func() { op.Complete(opErr) }()

		// 1. Render config.
		op.AppendLog("Rendering OpenViking configuration...")
		data, err := openviking.RenderConfig(configPath, cp)
		if err != nil {
			op.AppendLog("ERROR: " + err.Error())
			opErr = err
			return
		}

		// 2. Write config.
		if err := openviking.WriteConfig(configPath, data); err != nil {
			op.AppendLog("ERROR: " + err.Error())
			opErr = err
			return
		}
		op.AppendLog("Configuration written to " + configPath)

		// 3. Restart container.
		op.AppendLog("Restarting OpenViking container...")
		restartOp, err := h.compose.Restart(ctx, composeService)
		if err != nil {
			op.AppendLog("ERROR restarting: " + err.Error())
			opErr = err
			return
		}
		// Wait for the restart operation to finish and relay its logs.
		if waitErr := restartOp.Wait(); waitErr != nil {
			for _, line := range restartOp.Lines(0) {
				op.AppendLog(line)
			}
			op.AppendLog("ERROR: restart failed: " + waitErr.Error())
			opErr = waitErr
			return
		}
		for _, line := range restartOp.Lines(0) {
			op.AppendLog(line)
		}
		op.AppendLog("Container restarted")

		// 4. Health check.
		op.AppendLog("Waiting for OpenViking health check...")
		if err := openviking.WaitHealthy(ctx, healthURL, healthCheckTimeout); err != nil {
			op.AppendLog("ERROR: " + err.Error())
			opErr = err
			return
		}
		op.AppendLog("OpenViking health check passed")

		op.AppendLog("OpenViking configured successfully")
	}()

	return op, nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// --- Cancel ------------------------------------------------------------

func (h *Handler) cancelOperation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	op, ok := h.operations.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "operation.not_found", "operation not found")
		return
	}
	op.Cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// --- SSE streaming -----------------------------------------------------

const (
	sseFlushInterval = 200 * time.Millisecond
	sseEventLog      = "log"
	sseEventStatus   = "status"
)

func (h *Handler) streamOperation(w http.ResponseWriter, r *http.Request) {
	opID := r.PathValue("id")
	op, ok := h.operations.Get(opID)
	if !ok {
		writeError(w, http.StatusNotFound, "operation.not_found", fmt.Sprintf("operation %q not found", opID))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "sse.unsupported", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	offset := 0
	ticker := time.NewTicker(sseFlushInterval)
	defer ticker.Stop()

	for {
		// Drain any new log lines.
		lines := op.Lines(offset)
		for _, line := range lines {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEventLog, line)
		}
		offset += len(lines)

		if len(lines) > 0 {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
			return
		case <-op.Done():
			// Drain final lines after completion.
			final := op.Lines(offset)
			for _, line := range final {
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEventLog, line)
			}

			statusPayload := statusJSON(op)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEventStatus, statusPayload)
			flusher.Flush()
			return
		case <-ticker.C:
			// Continue polling loop.
		}
	}
}

// statusJSON produces the final SSE status payload from a completed operation.
func statusJSON(op *docker.Operation) string {
	waitErr := op.Wait()
	if waitErr != nil {
		data, _ := json.Marshal(map[string]string{
			"status": string(docker.OperationFailed),
			"error":  waitErr.Error(),
		})
		return string(data)
	}
	data, _ := json.Marshal(map[string]string{
		"status": string(docker.OperationCompleted),
	})
	return string(data)
}

// --- System ------------------------------------------------------------

func (h *Handler) systemVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     h.version,
		"compose_dir": h.compose.ProjectDir(),
	})
}

type upgradeRequest struct {
	Mode          string `json:"mode"`           // "prod" or "dev", default "dev"
	TargetVersion string `json:"target_version"` // optional, for prod mode
}

// versionPattern validates target_version to prevent .env injection.
var versionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func (h *Handler) systemUpgrade(w http.ResponseWriter, r *http.Request) {
	var req upgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request.invalid", "invalid JSON body")
		return
	}
	if req.Mode == "" {
		req.Mode = "dev"
	}
	if req.Mode != "prod" && req.Mode != "dev" {
		writeError(w, http.StatusBadRequest, "request.invalid", "mode must be 'prod' or 'dev'")
		return
	}
	if req.TargetVersion != "" && !versionPattern.MatchString(req.TargetVersion) {
		writeError(w, http.StatusBadRequest, "request.invalid", "target_version contains invalid characters")
		return
	}

	// Prevent concurrent upgrades.
	h.upgradeMu.Lock()
	if h.upgrading {
		h.upgradeMu.Unlock()
		writeError(w, http.StatusConflict, "upgrade.in_progress", "a system upgrade is already in progress")
		return
	}
	h.upgrading = true
	h.upgradeMu.Unlock()

	h.auditLog.LogAction("system_upgrade", "system", map[string]string{
		"mode":           req.Mode,
		"target_version": req.TargetVersion,
	})

	opCtx := context.WithoutCancel(r.Context())

	profiles := readProfilesFromState(h.compose.ProjectDir())

	op := docker.NewOperation("system", "upgrade")
	op.Status = docker.OperationRunning
	h.operations.Add(op)

	go func() {
		defer func() {
			h.upgradeMu.Lock()
			h.upgrading = false
			h.upgradeMu.Unlock()
		}()
		h.runUpgrade(opCtx, op, req, profiles)
	}()

	writeJSON(w, http.StatusAccepted, actionResponse{OperationID: op.ID})
}

func (h *Handler) runUpgrade(ctx context.Context, op *docker.Operation, req upgradeRequest, profiles []string) {
	var opErr error
	defer func() { op.Complete(opErr) }()

	projectDir := h.compose.ProjectDir()

	// Determine extra compose files for prod mode.
	var extraFiles []string
	if req.Mode == "prod" {
		prodFile := filepath.Join(projectDir, "compose.prod.yaml")
		if _, err := os.Stat(prodFile); err == nil {
			extraFiles = append(extraFiles, prodFile)
		}
	}

	// 1. Set target version if prod mode.
	if req.Mode == "prod" && req.TargetVersion != "" {
		op.AppendLog(fmt.Sprintf("Setting target version: %s", req.TargetVersion))
		if err := setEnvValue(filepath.Join(projectDir, ".env"), "ARKLOOP_VERSION", req.TargetVersion); err != nil {
			op.AppendLog("ERROR: " + err.Error())
			opErr = err
			return
		}
	}

	// 2. Pull images (prod) or skip (dev builds at up).
	if req.Mode == "prod" {
		op.AppendLog("Pulling latest images...")
		pullOp, err := h.compose.Pull(ctx, nil, profiles, extraFiles...)
		if err != nil {
			op.AppendLog("ERROR pulling images: " + err.Error())
			opErr = err
			return
		}
		if waitErr := pullOp.Wait(); waitErr != nil {
			relayLogs(op, pullOp)
			op.AppendLog("ERROR: image pull failed: " + waitErr.Error())
			opErr = waitErr
			return
		}
		relayLogs(op, pullOp)
		op.AppendLog("Images pulled successfully")
	}

	// 3. Run migrations.
	op.AppendLog("Running database migrations...")
	migrateOp, err := h.compose.RunMigrate(ctx, profiles, extraFiles...)
	if err != nil {
		op.AppendLog("ERROR running migrations: " + err.Error())
		opErr = err
		return
	}
	if waitErr := migrateOp.Wait(); waitErr != nil {
		relayLogs(op, migrateOp)
		op.AppendLog("ERROR: migration failed: " + waitErr.Error())
		opErr = waitErr
		return
	}
	relayLogs(op, migrateOp)
	op.AppendLog("Migrations completed")

	// 4. Recreate services (exclude bridge to avoid self-termination).
	if req.Mode == "prod" {
		op.AppendLog("Recreating services with new images...")
	} else {
		op.AppendLog("Rebuilding and recreating services...")
	}
	// Filter out "bridge" profile to prevent the bridge from killing itself
	// during the upgrade. The bridge can be restarted separately afterwards.
	nonBridgeProfiles := make([]string, 0, len(profiles))
	for _, p := range profiles {
		if p != "bridge" {
			nonBridgeProfiles = append(nonBridgeProfiles, p)
		}
	}
	upOp, err := h.compose.UpAll(ctx, nil, nonBridgeProfiles, req.Mode == "dev", extraFiles...)
	if err != nil {
		op.AppendLog("ERROR recreating services: " + err.Error())
		opErr = err
		return
	}
	if waitErr := upOp.Wait(); waitErr != nil {
		relayLogs(op, upOp)
		op.AppendLog("ERROR: service recreation failed: " + waitErr.Error())
		opErr = waitErr
		return
	}
	relayLogs(op, upOp)
	op.AppendLog("Services recreated")

	// 5. Health check.
	op.AppendLog("Waiting for system health check...")
	apiURL := fmt.Sprintf("http://localhost:%s/healthz", envOrDefault("ARKLOOP_API_PORT", "19001"))
	if err := waitForHTTP(ctx, apiURL, 120*time.Second); err != nil {
		op.AppendLog("WARNING: health check did not pass: " + err.Error())
	} else {
		op.AppendLog("System health check passed")
	}

	op.AppendLog("System upgrade completed successfully")
}

// readProfilesFromState reads COMPOSE_PROFILES from the install state file.
func readProfilesFromState(projectDir string) []string {
	stateFile := filepath.Join(projectDir, "install", ".state.env")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "COMPOSE_PROFILES=") {
			val := strings.TrimPrefix(line, "COMPOSE_PROFILES=")
			val = strings.Trim(val, "\"'")
			if val == "" {
				return nil
			}
			return strings.Split(val, ",")
		}
	}
	return nil
}

// setEnvValue updates or adds a key=value pair in a .env file.
func setEnvValue(envFile, key, value string) error {
	data, err := os.ReadFile(envFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")), 0644)
}

// waitForHTTP polls a URL until it responds with 200 OK or the timeout expires.
func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("health check timed out after %v", timeout)
}

// relayLogs copies all log lines from a sub-operation to the parent operation.
func relayLogs(parent, child *docker.Operation) {
	for _, line := range child.Lines(0) {
		parent.AppendLog(line)
	}
}

// --- Helpers -----------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}

// toStringMap converts map[string]any to map[string]string for audit logging.
func toStringMap(m map[string]any) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

// handleVirtualInstall routes install actions for virtual modules (no compose
// service) to the appropriate installer. Currently supports prompt-guard.
func (h *Handler) handleVirtualInstall(ctx context.Context, moduleID string, params map[string]any) (*docker.Operation, error) {
	switch moduleID {
	case "prompt-guard":
		if h.modelDL == nil {
			return nil, fmt.Errorf("model downloader not configured")
		}
		variant := "22m"
		if v, ok := params["variant"]; ok {
			if s, ok := v.(string); ok && s != "" {
				variant = s
			}
		}
		return h.modelDL.Install(ctx, variant)
	default:
		return nil, fmt.Errorf("module %q is virtual but has no custom installer", moduleID)
	}
}
