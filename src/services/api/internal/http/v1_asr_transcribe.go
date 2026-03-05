package http

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	nethttp "net/http"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

const groqDefaultBaseURL = "https://api.groq.com/openai/v1"
const openaiDefaultBaseURL = "https://api.openai.com/v1"

const (
	asrMaxUploadBytes   = 25 << 20 // 25 MiB
	asrUpstreamTimeout  = 120 * time.Second
	asrMaxResponseBytes = 2 << 20 // 2 MiB
)

var asrHTTPClient = &nethttp.Client{
	Timeout: asrUpstreamTimeout,
}

func asrTranscribeEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	asrCredRepo *data.AsrCredentialsRepository,
	secretsRepo *data.SecretsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if asrCredRepo == nil || secretsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		cred, err := asrCredRepo.GetDefault(r.Context(), actor.OrgID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if cred == nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "asr.no_default_credential", "no default ASR credential configured", traceID, nil)
			return
		}

		if cred.SecretID == nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		apiKey, err := secretsRepo.DecryptByID(r.Context(), *cred.SecretID)
		if err != nil || apiKey == nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid multipart form", traceID, nil)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "audio file is required", traceID, nil)
			return
		}
		defer file.Close()

		limited := io.LimitReader(file, asrMaxUploadBytes+1)

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)

		fw, err := mw.CreateFormFile("file", header.Filename)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		n, err := io.Copy(fw, limited)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if n > asrMaxUploadBytes {
			WriteError(w, nethttp.StatusRequestEntityTooLarge, "validation.error", "file too large", traceID, nil)
			return
		}
		if err := mw.WriteField("model", cred.Model); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if err := mw.WriteField("response_format", "json"); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if lang := r.FormValue("language"); lang != "" {
			if err := mw.WriteField("language", lang); err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
		mw.Close()

		upstreamURL := resolveAsrBaseURL(cred) + "/audio/transcriptions"

		req, err := nethttp.NewRequestWithContext(r.Context(), nethttp.MethodPost, upstreamURL, &buf)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", *apiKey))

		resp, err := asrHTTPClient.Do(req)
		if err != nil {
			WriteError(w, nethttp.StatusBadGateway, "asr.upstream_error", "upstream ASR request failed", traceID, nil)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, asrMaxResponseBytes))
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if resp.StatusCode != nethttp.StatusOK {
			WriteError(w, nethttp.StatusBadGateway, "asr.upstream_error", "upstream ASR error", traceID, nil)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(body)
	}
}

func resolveAsrBaseURL(cred *data.AsrCredential) string {
	if cred.BaseURL != nil && *cred.BaseURL != "" {
		return *cred.BaseURL
	}
	if cred.Provider == "groq" {
		return groqDefaultBaseURL
	}
	return openaiDefaultBaseURL
}
