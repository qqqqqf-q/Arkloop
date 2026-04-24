package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"encoding/json"
	"io"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type githubSkillImportRequest struct {
	RepositoryURL string `json:"repository_url"`
	Ref           string `json:"ref"`
	CandidatePath string `json:"candidate_path"`
}

type skillImportCandidate struct {
	Path        string `json:"path"`
	SkillKey    string `json:"skill_key"`
	Version     string `json:"version"`
	DisplayName string `json:"display_name"`
}

func githubSkillImportEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	packagesRepo *data.SkillPackagesRepository,
	store skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if packagesRepo == nil || store == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
			return
		}
		var req githubSkillImportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid JSON body", traceID, nil)
			return
		}
		item, candidates, err := importSkillFromGitHub(r.Context(), store, packagesRepo, actor.AccountID, req.RepositoryURL, req.Ref, req.CandidatePath, "")
		if err != nil {
			writeSkillImportErrorWithCandidates(w, traceID, err, candidates)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, map[string]any{"skill": toSkillPackageResponse(item)})
	}
}

func uploadSkillImportEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	packagesRepo *data.SkillPackagesRepository,
	store skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if packagesRepo == nil || store == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
			return
		}
		r.Body = nethttp.MaxBytesReader(w, r.Body, 64<<20)
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid multipart body", traceID, nil)
			return
		}
		files := r.MultipartForm.File["files"]
		relativePaths := r.MultipartForm.Value["relative_paths"]
		if len(files) == 0 {
			file, header, err := r.FormFile("file")
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "files are required", traceID, nil)
				return
			}
			defer func() { _ = file.Close() }()
			payload, readErr := io.ReadAll(io.LimitReader(file, 64<<20))
			if readErr != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "read file failed", traceID, nil)
				return
			}
			item, candidates, importErr := importSkillFromUploadData(r.Context(), store, packagesRepo, actor.AccountID, header.Filename, payload)
			if importErr != nil {
				writeSkillImportErrorWithCandidates(w, traceID, importErr, candidates)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSkillPackageResponse(item))
			return
		}
		entries := make(map[string][]byte, len(files))
		for index, header := range files {
			file, err := header.Open()
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "read file failed", traceID, nil)
				return
			}
			payload, readErr := io.ReadAll(io.LimitReader(file, 8<<20))
			if closeErr := file.Close(); closeErr != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "read file failed", traceID, nil)
				return
			}
			if readErr != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "read file failed", traceID, nil)
				return
			}
			relativePath := header.Filename
			if index < len(relativePaths) && strings.TrimSpace(relativePaths[index]) != "" {
				relativePath = relativePaths[index]
			}
			entries[relativePath] = payload
		}
		item, candidates, err := importSkillFromUploadEntries(r.Context(), store, packagesRepo, actor.AccountID, entries)
		if err != nil {
			writeSkillImportErrorWithCandidates(w, traceID, err, candidates)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSkillPackageResponse(item))
	}
}

func writeSkillImportErrorWithCandidates(w nethttp.ResponseWriter, traceID string, err error, candidates []skillImportCandidate) {
	var importErr skillImportError
	if ok := errorAs(err, &importErr); ok {
		var details map[string]any
		if len(candidates) > 0 {
			details = map[string]any{"candidates": candidates}
		}
		httpkit.WriteError(w, importErr.status, importErr.code, importErr.msg, traceID, details)
		return
	}
	writeSkillImportError(w, traceID, err)
}

func writeSkillImportError(w nethttp.ResponseWriter, traceID string, err error) {
	var importErr skillImportError
	if ok := errorAs(err, &importErr); ok {
		httpkit.WriteError(w, importErr.status, importErr.code, importErr.msg, traceID, nil)
		return
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "not found") {
		httpkit.WriteError(w, nethttp.StatusNotFound, "skills.import_not_found", "skill not found", traceID, nil)
		return
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "multiple") {
		httpkit.WriteError(w, nethttp.StatusConflict, "skills.import_ambiguous", "multiple skill packages found", traceID, nil)
		return
	}
	httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
}
