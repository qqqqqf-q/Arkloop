//go:build !desktop

package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/api/internal/http/httpkit"
	"arkloop/services/shared/outboundurl"
)

func TestLlmProvidersGeminiCanonicalizesModelIdentifiers(t *testing.T) {
	t.Setenv(outboundurl.AllowLoopbackHTTPEnv, "true")
	env := setupLlmProvidersTestEnv(t)

	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "g-canonical-123" {
			t.Fatalf("unexpected x-goog-api-key: %q", got)
		}
		httpkit.WriteJSON(w, "", nethttp.StatusOK, map[string]any{
			"models": []map[string]any{
				{
					"name":                       "models/gemini-2.0-flash",
					"displayName":                "Gemini 2.0 Flash",
					"supportedGenerationMethods": []string{"generateContent"},
				},
				{
					"name":                       "models/gemini-2.5-pro",
					"displayName":                "Gemini 2.5 Pro",
					"supportedGenerationMethods": []string{"generateContent"},
				},
			},
		})
	}))
	defer upstream.Close()

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":     "gemini-canonical",
		"provider": "gemini",
		"api_key":  "g-canonical-123",
		"base_url": upstream.URL + "/v1beta",
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())

	createModelResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers/"+provider.ID+"/models", map[string]any{
		"model":    "models/gemini-2.0-flash",
		"priority": 1,
	}, authHeader(env.adminToken))
	if createModelResp.Code != nethttp.StatusCreated {
		t.Fatalf("create gemini model: %d %s", createModelResp.Code, createModelResp.Body.String())
	}
	model := decodeJSONBody[llmProviderModelResponse](t, createModelResp.Body.Bytes())
	if model.Model != "gemini-2.0-flash" {
		t.Fatalf("expected canonical model on create, got %#v", model)
	}

	patchModelResp := doJSON(env.handler, nethttp.MethodPatch, "/v1/llm-providers/"+provider.ID+"/models/"+model.ID, map[string]any{
		"model": "models/gemini-2.5-pro",
	}, authHeader(env.adminToken))
	if patchModelResp.Code != nethttp.StatusOK {
		t.Fatalf("patch gemini model: %d %s", patchModelResp.Code, patchModelResp.Body.String())
	}
	model = decodeJSONBody[llmProviderModelResponse](t, patchModelResp.Body.Bytes())
	if model.Model != "gemini-2.5-pro" {
		t.Fatalf("expected canonical model on patch, got %#v", model)
	}

	listResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers", nil, authHeader(env.adminToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list providers: %d %s", listResp.Code, listResp.Body.String())
	}
	providers := decodeJSONBody[[]llmProviderResponse](t, listResp.Body.Bytes())
	if len(providers) != 1 || len(providers[0].Models) != 1 {
		t.Fatalf("unexpected provider list: %#v", providers)
	}
	if providers[0].Models[0].Model != "gemini-2.5-pro" {
		t.Fatalf("expected canonical model in list response, got %#v", providers[0].Models[0])
	}

	availableResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers/"+provider.ID+"/available-models", nil, authHeader(env.adminToken))
	if availableResp.Code != nethttp.StatusOK {
		t.Fatalf("available models: %d %s", availableResp.Code, availableResp.Body.String())
	}
	payload := decodeJSONBody[llmProviderAvailableModelsResponse](t, availableResp.Body.Bytes())
	if len(payload.Models) != 2 {
		t.Fatalf("unexpected available models payload: %#v", payload)
	}

	configured := map[string]bool{}
	for _, item := range payload.Models {
		configured[item.ID] = item.Configured
	}
	if configured["gemini-2.0-flash"] {
		t.Fatalf("expected old model to be unconfigured after patch: %#v", configured)
	}
	if !configured["gemini-2.5-pro"] {
		t.Fatalf("expected canonical configured model flag: %#v", configured)
	}
}
