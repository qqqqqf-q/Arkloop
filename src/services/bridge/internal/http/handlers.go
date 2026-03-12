package bridgehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"arkloop/services/bridge/internal/audit"
	"arkloop/services/bridge/internal/docker"
	"arkloop/services/bridge/internal/module"
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
}

// NewHandler creates a Handler with all required dependencies.
func NewHandler(
	registry *module.Registry,
	compose *docker.Compose,
	operations *docker.OperationStore,
	auditLog *audit.Logger,
	logger AppLogger,
) *Handler {
	return &Handler{
		registry:   registry,
		compose:    compose,
		operations: operations,
		auditLog:   auditLog,
		appLogger:  logger,
	}
}

// RegisterRoutes registers all API routes on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/platform/detect", h.platformDetect)
	mux.HandleFunc("GET /v1/modules", h.listModules)
	mux.HandleFunc("GET /v1/modules/{id}", h.getModule)
	mux.HandleFunc("POST /v1/modules/{id}/actions", h.moduleAction)
	mux.HandleFunc("GET /v1/operations/{id}/stream", h.streamOperation)
}

// --- Platform ----------------------------------------------------------

func (h *Handler) platformDetect(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, platform.Detect())
}

// --- Modules -----------------------------------------------------------

const dockerQueryTimeout = 3 * time.Second

func (h *Handler) listModules(w http.ResponseWriter, r *http.Request) {
	defs := h.registry.OptionalModules()
	infos := make([]module.ModuleInfo, 0, len(defs))

	for i := range defs {
		status := h.moduleStatus(r.Context(), &defs[i])
		infos = append(infos, defs[i].ToModuleInfo(status))
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
	writeJSON(w, http.StatusOK, def.ToModuleInfo(status))
}

// moduleStatus queries Docker for the live status of a module's compose service.
func (h *Handler) moduleStatus(ctx context.Context, def *module.ModuleDefinition) module.ModuleStatus {
	if def.ComposeService == "" {
		return module.StatusNotInstalled
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
		op, err = h.compose.Install(opCtx, def.ComposeService, def.ComposeProfile)
	case module.ActionStart:
		op, err = h.compose.Start(opCtx, def.ComposeService)
	case module.ActionStop:
		op, err = h.compose.Stop(opCtx, def.ComposeService)
	case module.ActionRestart:
		op, err = h.compose.Restart(opCtx, def.ComposeService)
	case module.ActionConfigureConnection, module.ActionBootstrapDefaults:
		// Placeholder: return a synthetic operation ID for future implementation.
		placeholderID := uuid.New().String()
		writeJSON(w, http.StatusAccepted, actionResponse{OperationID: placeholderID})
		return
	}

	if err != nil {
		h.appLogger.Error("action failed", map[string]any{
			"module": id,
			"action": req.Action,
			"error":  err.Error(),
		})
		writeError(w, http.StatusInternalServerError, "action.failed", err.Error())
		return
	}

	h.operations.Add(op)
	writeJSON(w, http.StatusAccepted, actionResponse{OperationID: op.ID})
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
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEventLog, line)
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
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEventLog, line)
			}

			statusPayload := statusJSON(op)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEventStatus, statusPayload)
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
