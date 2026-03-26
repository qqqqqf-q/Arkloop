package understandimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/tools"
)

type fakeProvider struct {
	resp DescribeImageResponse
	err  error
	req  DescribeImageRequest
}

func (p *fakeProvider) DescribeImage(_ context.Context, req DescribeImageRequest) (DescribeImageResponse, error) {
	p.req = req
	if p.err != nil {
		return DescribeImageResponse{}, p.err
	}
	return p.resp, nil
}

func TestToolExecutorSuccess(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	provider := &fakeProvider{
		resp: DescribeImageResponse{
			Text:     "一张测试图片",
			Provider: "minimax",
			Model:    DefaultMiniMaxModel,
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNGBytes(t))
	}))
	defer server.Close()

	executor := NewToolExecutorWithProvider(provider)
	result := executor.Execute(context.Background(), "understand_image", map[string]any{
		"url":    server.URL + "/image.png",
		"prompt": "描述这张图",
	}, tools.ExecutionContext{}, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := provider.req.MimeType; got != "image/png" {
		t.Fatalf("unexpected mime type: %q", got)
	}
	if len(provider.req.Bytes) == 0 {
		t.Fatal("expected provider to receive image bytes")
	}
	if got := result.ResultJSON["text"]; got != "一张测试图片" {
		t.Fatalf("unexpected text: %#v", got)
	}
}

func TestToolExecutorRejectsNonImage(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	executor := NewToolExecutorWithProvider(&fakeProvider{})
	result := executor.Execute(context.Background(), "understand_image", map[string]any{
		"url":    server.URL,
		"prompt": "描述这张图",
	}, tools.ExecutionContext{}, "")

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.ErrorClass != errorUnsupportedMedia {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func TestToolExecutorRejectsOversizedImage(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "2048")
		_, _ = w.Write([]byte(strings.Repeat("a", 2048)))
	}))
	defer server.Close()

	executor := NewToolExecutorWithProvider(&fakeProvider{})
	result := executor.Execute(context.Background(), "understand_image", map[string]any{
		"url":       server.URL,
		"prompt":    "描述这张图",
		"max_bytes": 1024,
	}, tools.ExecutionContext{}, "")

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.ErrorClass != errorTooLarge {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func TestToolExecutorMapsProviderError(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNGBytes(t))
	}))
	defer server.Close()

	executor := NewToolExecutorWithProvider(&fakeProvider{
		err: ProviderError{Message: "provider failed", StatusCode: 502, TraceID: "trace-test"},
	})
	result := executor.Execute(context.Background(), "understand_image", map[string]any{
		"url":    server.URL,
		"prompt": "描述这张图",
	}, tools.ExecutionContext{}, "")

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.ErrorClass != errorProviderFailed {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
	if got := result.Error.Details["trace_id"]; got != "trace-test" {
		t.Fatalf("unexpected trace id: %#v", got)
	}
}

func TestMiniMaxProviderDescribeImage(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/coding_plan/vlm" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp": map[string]any{
				"status_code": 0,
				"status_msg":  "ok",
			},
			"content": "图片里是一只小鸟",
		})
	}))
	defer server.Close()

	provider, err := NewMiniMaxProvider("test-key", server.URL, "")
	if err != nil {
		t.Fatalf("NewMiniMaxProvider: %v", err)
	}
	resp, err := provider.DescribeImage(context.Background(), DescribeImageRequest{
		Prompt:   "识别这张图",
		MimeType: "image/png",
		Bytes:    testPNGBytes(t),
	})
	if err != nil {
		t.Fatalf("DescribeImage: %v", err)
	}
	if resp.Text != "图片里是一只小鸟" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if resp.Model != DefaultMiniMaxModel {
		t.Fatalf("unexpected model: %q", resp.Model)
	}
	rawURL, ok := captured["image_url"].(string)
	if !ok || !strings.HasPrefix(rawURL, "data:image/png;base64,") {
		t.Fatalf("unexpected image_url payload: %#v", captured["image_url"])
	}
}

func TestMiniMaxProviderHTTPError(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trace-Id", "trace-123")
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	provider, err := NewMiniMaxProvider("test-key", server.URL, "")
	if err != nil {
		t.Fatalf("NewMiniMaxProvider: %v", err)
	}
	_, err = provider.DescribeImage(context.Background(), DescribeImageRequest{
		Prompt:   "识别这张图",
		MimeType: "image/png",
		Bytes:    testPNGBytes(t),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var providerErr ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected status code: %d", providerErr.StatusCode)
	}
	if providerErr.TraceID != "trace-123" {
		t.Fatalf("unexpected trace id: %q", providerErr.TraceID)
	}
}

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	raw := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF9sAAAAASUVORK5CYII="
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return decoded
}
