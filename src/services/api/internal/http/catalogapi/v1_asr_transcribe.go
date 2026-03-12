package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	nethttp "net/http"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedoutbound "arkloop/services/shared/outboundurl"
)

const groqDefaultBaseURL = "https://api.groq.com/openai/v1"
const openaiDefaultBaseURL = "https://api.openai.com/v1"

const (
	asrMaxUploadBytes   = 25 << 20 // 25 MiB
	asrUpstreamTimeout  = 120 * time.Second
	asrMaxResponseBytes = 2 << 20 // 2 MiB
)

func asrTranscribeEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	asrCredRepo *data.AsrCredentialsRepository,
	secretsRepo *data.SecretsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if asrCredRepo == nil || secretsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		cred, err := asrCredRepo.GetDefault(r.Context(), actor.AccountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if cred == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "asr.no_default_credential", "no default ASR credential configured", traceID, nil)
			return
		}

		if cred.SecretID == nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		apiKey, err := secretsRepo.DecryptByID(r.Context(), *cred.SecretID)
		if err != nil || apiKey == nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid multipart form", traceID, nil)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "audio file is required", traceID, nil)
			return
		}
		defer file.Close()

		limited := io.LimitReader(file, asrMaxUploadBytes+1)

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)

		fw, err := mw.CreateFormFile("file", header.Filename)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		n, err := io.Copy(fw, limited)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if n > asrMaxUploadBytes {
			httpkit.WriteError(w, nethttp.StatusRequestEntityTooLarge, "validation.error", "file too large", traceID, nil)
			return
		}
		if err := mw.WriteField("model", cred.Model); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if err := mw.WriteField("response_format", "json"); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if lang := r.FormValue("language"); lang != "" {
			if err := mw.WriteField("language", lang); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
		mw.Close()

		upstreamURL := resolveAsrBaseURL(cred) + "/audio/transcriptions"
		if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(upstreamURL); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "asr.invalid_base_url", "invalid ASR base_url configured", traceID, nil)
			return
		}

		req, err := nethttp.NewRequestWithContext(r.Context(), nethttp.MethodPost, upstreamURL, &buf)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", *apiKey))

		resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(asrUpstreamTimeout).Do(req)
		if err != nil {
			var denied sharedoutbound.DeniedError
			if errors.As(err, &denied) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "asr.invalid_base_url", "invalid ASR base_url configured", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusBadGateway, "asr.upstream_error", "upstream ASR request failed", traceID, nil)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, asrMaxResponseBytes))
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if resp.StatusCode != nethttp.StatusOK {
			httpkit.WriteError(w, nethttp.StatusBadGateway, "asr.upstream_error", "upstream ASR error", traceID, nil)
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
