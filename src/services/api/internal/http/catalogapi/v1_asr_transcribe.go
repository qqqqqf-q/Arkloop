package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	logger *slog.Logger,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		writeErr := func(status int, code, msg string, err error) {
			if logger != nil {
				logger.ErrorContext(r.Context(), msg, "trace_id", traceID, "err", err.Error())
			}
			httpkit.WriteError(w, status, code, msg, traceID, nil)
		}
		writeErrStr := func(status int, code, msg string) {
			if logger != nil {
				logger.ErrorContext(r.Context(), msg, "trace_id", traceID)
			}
			httpkit.WriteError(w, status, code, msg, traceID, nil)
		}

		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if asrCredRepo == nil || secretsRepo == nil {
			writeErrStr(nethttp.StatusServiceUnavailable, "database.not_configured", "asrCredRepo or secretsRepo nil")
			return
		}

		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
		if !ok {
			return
		}

		cred, err := asrCredRepo.GetDefault(r.Context(), actor.UserID)
		if err != nil {
			writeErr(nethttp.StatusInternalServerError, "internal.error", fmt.Sprintf("GetDefault: %v", err), err)
			return
		}
		if cred == nil {
			writeErrStr(nethttp.StatusUnprocessableEntity, "asr.no_default_credential", "no default ASR credential for this user")
			return
		}

		var apiKey string
		if cred.SecretID != nil {
			decrypted, err := secretsRepo.DecryptByID(r.Context(), *cred.SecretID)
			if err != nil || decrypted == nil {
				writeErr(nethttp.StatusInternalServerError, "internal.error", fmt.Sprintf("DecryptByID secret=%s: %v", *cred.SecretID, err), err)
				return
			}
			apiKey = *decrypted
		} else if cred.APIKeyLegacy != nil && *cred.APIKeyLegacy != "" {
			// SQLite migration fallback: secret_id nil, use legacy plaintext field
			apiKey = *cred.APIKeyLegacy
		} else {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "secret_id nil and no api_key_legacy fallback")
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeErrStr(nethttp.StatusUnprocessableEntity, "validation.error", "ParseMultipartForm: "+err.Error())
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeErrStr(nethttp.StatusUnprocessableEntity, "validation.error", "FormFile: "+err.Error())
			return
		}
		defer func() { _ = file.Close() }()

		limited := io.LimitReader(file, asrMaxUploadBytes+1)

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)

		fw, err := mw.CreateFormFile("file", header.Filename)
		if err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "CreateFormFile: "+err.Error())
			return
		}
		n, err := io.Copy(fw, limited)
		if err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "io.Copy: "+err.Error())
			return
		}
		if n > asrMaxUploadBytes {
			writeErrStr(nethttp.StatusRequestEntityTooLarge, "validation.error", fmt.Sprintf("file too large: %d > %d", n, asrMaxUploadBytes))
			return
		}
		if err := mw.WriteField("model", cred.Model); err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "WriteField model: "+err.Error())
			return
		}
		if err := mw.WriteField("response_format", "json"); err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "WriteField response_format: "+err.Error())
			return
		}
		if lang := r.FormValue("language"); lang != "" {
			if err := mw.WriteField("language", lang); err != nil {
				writeErrStr(nethttp.StatusInternalServerError, "internal.error", "WriteField language: "+err.Error())
				return
			}
		}
		if err := mw.Close(); err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "Close multipart writer: "+err.Error())
			return
		}

		upstreamURL := resolveAsrBaseURL(cred) + "/audio/transcriptions"
		policy := sharedoutbound.DefaultPolicy()
		if err := policy.ValidateRequestURL(upstreamURL); err != nil {
			writeErrStr(nethttp.StatusUnprocessableEntity, "asr.invalid_base_url", "invalid ASR base_url: "+err.Error())
			return
		}

		req, err := nethttp.NewRequest(nethttp.MethodPost, upstreamURL, &buf)
		if err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "NewRequest: "+err.Error())
			return
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

		resp, err := policy.NewHTTPClient(asrUpstreamTimeout).Do(req)
		if err != nil {
			var denied sharedoutbound.DeniedError
			if errors.As(err, &denied) {
				writeErrStr(nethttp.StatusUnprocessableEntity, "asr.invalid_base_url", "outbound request denied: "+denied.Reason)
				return
			}
			writeErrStr(nethttp.StatusBadGateway, "asr.upstream_error", "upstream request failed: "+err.Error())
			return
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(io.LimitReader(resp.Body, asrMaxResponseBytes))
		if err != nil {
			writeErrStr(nethttp.StatusInternalServerError, "internal.error", "ReadAll resp body: "+err.Error())
			return
		}

		if resp.StatusCode != nethttp.StatusOK {
			writeErrStr(nethttp.StatusBadGateway, "asr.upstream_error", fmt.Sprintf("upstream status %d: %s", resp.StatusCode, string(body)))
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
