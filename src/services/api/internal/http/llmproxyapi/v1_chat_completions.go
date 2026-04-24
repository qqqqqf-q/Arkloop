package llmproxyapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

// chatCompletionRequest mirrors the OpenAI chat completion request format.
type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      *bool         `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Stop        any           `json:"stop,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func chatCompletionsEntry(deps Deps) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		// 1. Extract and validate ACP session token.
		token, ok := extractBearerToken(r)
		if !ok {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.missing_token", "missing Authorization Bearer token", traceID, nil)
			return
		}

		if deps.TokenValidator == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "llm_proxy.not_configured", "LLM proxy token validation not configured", traceID, nil)
			return
		}

		validated, err := deps.TokenValidator.Validate(token)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", fmt.Sprintf("invalid session token: %s", err), traceID, nil)
			return
		}

		// 2. Parse request body.
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "request.read_failed", "failed to read request body", traceID, nil)
			return
		}

		var req chatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "request.invalid_json", "invalid JSON in request body", traceID, nil)
			return
		}

		if req.Model == "" {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "request.missing_model", "model field is required", traceID, nil)
			return
		}

		// 3. Validate model against token whitelist.
		if !validated.AllowsModel(req.Model) {
			httpkit.WriteError(w, nethttp.StatusForbidden, "llm_proxy.model_not_allowed",
				fmt.Sprintf("model %q is not allowed by session token (allowed: %v)", req.Model, validated.Models), traceID, nil)
			return
		}

		// 4. Resolve provider credentials.
		upstream, err := resolveUpstream(r.Context(), deps, req.Model)
		if err != nil {
			slog.Error("llm_proxy: resolve upstream failed", "error", err, "model", req.Model, "trace_id", traceID)
			httpkit.WriteError(w, nethttp.StatusBadGateway, "llm_proxy.no_upstream",
				fmt.Sprintf("no upstream provider configured for model %q", req.Model), traceID, nil)
			return
		}

		slog.Info("llm_proxy: forwarding request",
			"trace_id", traceID,
			"run_id", validated.RunID,
			"model", req.Model,
			"provider", upstream.provider,
			"stream", req.Stream != nil && *req.Stream,
		)

		// 5. Forward request to upstream provider.
		isStream := req.Stream != nil && *req.Stream
		if isStream {
			proxySSE(w, r.Context(), upstream, body, traceID)
		} else {
			proxyJSON(w, r.Context(), upstream, body, traceID)
		}
	}
}

type upstreamConfig struct {
	provider string
	baseURL  string
	apiKey   string
	model    string
}

func resolveUpstream(ctx context.Context, deps Deps, model string) (*upstreamConfig, error) {
	if deps.LlmRoutesRepo == nil || deps.LlmCredRepo == nil || deps.SecretsRepo == nil {
		return nil, fmt.Errorf("LLM repositories not configured")
	}

	// Fetch all active routes (joined with non-revoked credentials), ordered by priority DESC.
	routes, err := deps.LlmRoutesRepo.ListAllActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active routes: %w", err)
	}

	// Find the first route that matches the requested model.
	var credentialID *uuid.UUID
	for i := range routes {
		if routes[i].Model == model {
			id := routes[i].CredentialID
			credentialID = &id
			break
		}
	}
	if credentialID == nil {
		return nil, fmt.Errorf("no route found for model %q", model)
	}

	// Locate the credential among all active credentials.
	creds, err := deps.LlmCredRepo.ListAllActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active credentials: %w", err)
	}

	var matched *struct {
		provider string
		baseURL  *string
		secretID *uuid.UUID
	}
	for i := range creds {
		if creds[i].ID == *credentialID {
			matched = &struct {
				provider string
				baseURL  *string
				secretID *uuid.UUID
			}{
				provider: creds[i].Provider,
				baseURL:  creds[i].BaseURL,
				secretID: creds[i].SecretID,
			}
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("credential %s not found or revoked", *credentialID)
	}
	if matched.secretID == nil {
		return nil, fmt.Errorf("credential %s has no secret configured", *credentialID)
	}

	// Decrypt the API key.
	apiKeyPtr, err := deps.SecretsRepo.DecryptByID(ctx, *matched.secretID)
	if err != nil {
		return nil, fmt.Errorf("decrypt API key for credential %s: %w", *credentialID, err)
	}
	if apiKeyPtr == nil {
		return nil, fmt.Errorf("secret for credential %s not found", *credentialID)
	}

	baseURL := ""
	if matched.baseURL != nil {
		baseURL = *matched.baseURL
	}
	if baseURL == "" {
		switch strings.ToLower(matched.provider) {
		case "openai":
			baseURL = "https://api.openai.com/v1"
		case "anthropic":
			baseURL = "https://api.anthropic.com/v1"
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}

	return &upstreamConfig{
		provider: matched.provider,
		baseURL:  baseURL,
		apiKey:   *apiKeyPtr,
		model:    model,
	}, nil
}

// ---------- forwarding helpers ----------

var (
	jsonHTTPClient = &nethttp.Client{Timeout: 5 * time.Minute}
	sseHTTPClient  = &nethttp.Client{Timeout: 0} // no timeout for streaming
)

func proxyJSON(w nethttp.ResponseWriter, ctx context.Context, upstream *upstreamConfig, body []byte, traceID string) {
	resp, err := doUpstreamRequest(ctx, upstream, body, false)
	if err != nil {
		slog.Error("llm_proxy: upstream request failed", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusBadGateway, "llm_proxy.upstream_error",
			"upstream provider request failed", traceID, nil)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "llm_proxy.upstream_read_error",
			"failed to read upstream response", traceID, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func proxySSE(w nethttp.ResponseWriter, ctx context.Context, upstream *upstreamConfig, body []byte, traceID string) {
	resp, err := doUpstreamRequest(ctx, upstream, body, true)
	if err != nil {
		slog.Error("llm_proxy: upstream SSE request failed", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusBadGateway, "llm_proxy.upstream_error",
			"upstream provider request failed", traceID, nil)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Forward non-200 error responses as-is.
	if resp.StatusCode != nethttp.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(nethttp.StatusOK)

	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		slog.Warn("llm_proxy: response writer does not support flushing", "trace_id", traceID)
	}

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				slog.Warn("llm_proxy: upstream read error during SSE", "error", err, "trace_id", traceID)
			}
			return
		}
	}
}

func doUpstreamRequest(ctx context.Context, upstream *upstreamConfig, body []byte, stream bool) (*nethttp.Response, error) {
	url := strings.TrimRight(upstream.baseURL, "/") + "/chat/completions"

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	switch strings.ToLower(upstream.provider) {
	case "anthropic":
		req.Header.Set("x-api-key", upstream.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+upstream.apiKey)
	}

	client := jsonHTTPClient
	if stream {
		client = sseHTTPClient
	}
	return client.Do(req)
}

func extractBearerToken(r *nethttp.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", false
	}
	scheme, rest, ok := strings.Cut(auth, " ")
	if !ok || strings.TrimSpace(rest) == "" || strings.ToLower(scheme) != "bearer" {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
