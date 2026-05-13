package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"arkloop/services/plugin-registry/internal/data"
	"arkloop/services/plugin-registry/internal/manifest"
	"arkloop/services/plugin-registry/internal/storage"
	"arkloop/services/shared/objectstore"
)

const maxManifestBytes = 1 << 20

type Handler struct {
	repo       data.Repository
	bundles    storage.BundleStore
	adminToken string
}

func NewHandler(repo data.Repository, bundles storage.BundleStore, adminToken string) (*Handler, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository must not be nil")
	}
	return &Handler{
		repo:       repo,
		bundles:    bundles,
		adminToken: strings.TrimSpace(adminToken),
	}, nil
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/api/v1/plugins", h.plugins)
	mux.HandleFunc("/api/v1/plugins/", h.pluginPath)
	mux.HandleFunc("/api/v1/admin/plugins", h.adminPlugins)
}

func (h *Handler) plugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	filter := data.SearchFilter{
		Query:    r.URL.Query().Get("q"),
		Host:     r.URL.Query().Get("host"),
		Platform: r.URL.Query().Get("platform"),
	}
	plugins, err := h.repo.SearchPlugins(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal.error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": plugins})
}

func (h *Handler) pluginPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/"))
	switch {
	case len(parts) == 1:
		h.getPlugin(w, r, parts[0])
	case len(parts) == 3 && parts[1] == "versions":
		h.getVersion(w, r, parts[0], parts[2])
	case len(parts) == 4 && parts[1] == "versions" && parts[3] == "manifest":
		h.getManifest(w, r, parts[0], parts[2])
	case len(parts) == 4 && parts[1] == "versions" && parts[3] == "bundle":
		h.getBundle(w, r, parts[0], parts[2])
	default:
		writeError(w, http.StatusNotFound, "plugins.not_found", "not found")
	}
}

func (h *Handler) getPlugin(w http.ResponseWriter, r *http.Request, pluginID string) {
	plugin, versions, err := h.repo.GetPlugin(r.Context(), pluginID)
	if errors.Is(err, data.ErrNotFound) {
		writeError(w, http.StatusNotFound, "plugins.not_found", "plugin not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal.error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plugin":   plugin,
		"versions": versions,
	})
}

func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request, pluginID string, versionID string) {
	version, ok := h.loadVersion(w, r, pluginID, versionID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (h *Handler) getManifest(w http.ResponseWriter, r *http.Request, pluginID string, versionID string) {
	version, ok := h.loadVersion(w, r, pluginID, versionID)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(version.Manifest)
}

func (h *Handler) getBundle(w http.ResponseWriter, r *http.Request, pluginID string, versionID string) {
	version, ok := h.loadVersion(w, r, pluginID, versionID)
	if !ok {
		return
	}
	if version.BundleObjectKey == "" || h.bundles == nil {
		writeError(w, http.StatusNotFound, "plugins.bundle_not_found", "bundle not found")
		return
	}
	bundle, contentType, err := h.bundles.GetBundle(r.Context(), version.BundleObjectKey)
	if objectstore.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "plugins.bundle_not_found", "bundle not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal.error", "internal error")
		return
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = storage.BundleContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bundle)
}

func (h *Handler) loadVersion(w http.ResponseWriter, r *http.Request, pluginID string, versionID string) (data.Version, bool) {
	version, err := h.repo.GetVersion(r.Context(), pluginID, versionID)
	if errors.Is(err, data.ErrNotFound) {
		writeError(w, http.StatusNotFound, "plugins.not_found", "plugin version not found")
		return data.Version{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal.error", "internal error")
		return data.Version{}, false
	}
	return version, true
}

func (h *Handler) adminPlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "auth.unauthorized", "unauthorized")
		return
	}
	parsed, bundle, err := readUpload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "plugins.invalid_upload", err.Error())
		return
	}
	input := data.CreateVersionInput{
		Manifest:     parsed.Manifest,
		ManifestJSON: parsed.JSON,
		ManifestYAML: string(parsed.YAML),
	}
	if len(bundle) > 0 {
		if h.bundles == nil {
			writeError(w, http.StatusServiceUnavailable, "plugins.bundle_storage_unavailable", "bundle storage unavailable")
			return
		}
		key := bundleKey(parsed.Manifest.ID, parsed.Manifest.Version)
		sum := sha256.Sum256(bundle)
		input.BundleObjectKey = key
		input.BundleSHA256 = hex.EncodeToString(sum[:])
		input.BundleSizeBytes = int64(len(bundle))
		if err := h.bundles.PutBundle(r.Context(), key, bundle, input.BundleSHA256); err != nil {
			writeError(w, http.StatusInternalServerError, "internal.error", "internal error")
			return
		}
	}
	version, err := h.repo.CreateVersion(r.Context(), input)
	if errors.Is(err, data.ErrConflict) {
		writeError(w, http.StatusConflict, "plugins.version_exists", "plugin version exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal.error", "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (h *Handler) authorized(r *http.Request) bool {
	if h.adminToken == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.adminToken)) == 1
}

func readUpload(r *http.Request) (manifest.Parsed, []byte, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return readMultipartUpload(r)
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxManifestBytes+1))
	if err != nil {
		return manifest.Parsed{}, nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(raw) > maxManifestBytes {
		return manifest.Parsed{}, nil, fmt.Errorf("manifest is too large")
	}
	parsed, err := manifest.Parse(raw)
	if err != nil {
		return manifest.Parsed{}, nil, err
	}
	return parsed, nil, nil
}

func readMultipartUpload(r *http.Request) (manifest.Parsed, []byte, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return manifest.Parsed{}, nil, fmt.Errorf("read multipart: %w", err)
	}
	var manifestData []byte
	var bundleData []byte
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return manifest.Parsed{}, nil, fmt.Errorf("read multipart part: %w", err)
		}
		switch part.FormName() {
		case "manifest":
			manifestData, err = io.ReadAll(io.LimitReader(part, maxManifestBytes+1))
			if err != nil {
				return manifest.Parsed{}, nil, fmt.Errorf("read manifest: %w", err)
			}
			if len(manifestData) > maxManifestBytes {
				return manifest.Parsed{}, nil, fmt.Errorf("manifest is too large")
			}
		case "bundle":
			bundleData, err = io.ReadAll(io.LimitReader(part, manifest.MaxBundleBytes+1))
			if err != nil {
				return manifest.Parsed{}, nil, fmt.Errorf("read bundle: %w", err)
			}
			if int64(len(bundleData)) > manifest.MaxBundleBytes {
				return manifest.Parsed{}, nil, fmt.Errorf("bundle is too large")
			}
		}
	}
	if len(manifestData) == 0 {
		if len(bundleData) == 0 {
			return manifest.Parsed{}, nil, fmt.Errorf("manifest is required")
		}
		manifestData, err = manifest.ExtractManifestFromBundle(bundleData)
		if err != nil {
			return manifest.Parsed{}, nil, err
		}
	} else if len(bundleData) > 0 {
		if err := manifest.ValidateBundle(bundleData); err != nil {
			return manifest.Parsed{}, nil, err
		}
	}
	parsed, err := manifest.Parse(manifestData)
	if err != nil {
		return manifest.Parsed{}, nil, err
	}
	return parsed, bundleData, nil
}

func bundleKey(pluginID string, version string) string {
	return "plugins/" + pluginID + "/versions/" + version + "/bundle.tar.gz"
}

func splitPath(path string) []string {
	raw := strings.Split(strings.Trim(path, "/"), "/")
	parts := raw[:0]
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]string{
		"code":    code,
		"message": message,
	})
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "http.method_not_allowed", "method not allowed")
}
