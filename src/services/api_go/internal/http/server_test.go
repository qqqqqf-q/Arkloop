package http

import (
	"encoding/json"
	"io"
	"regexp"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api_go/internal/observability"
)

// flusherRecorder 是一个同时实现 http.Flusher 的测试 recorder，
// 用于验证中间件包装后 http.Flusher 断言不会丢失。
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() {
	f.flushed = true
	f.ResponseRecorder.Flush()
}

func TestHealthz(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestNotFoundReturnsEnvelope(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	req := httptest.NewRequest(nethttp.MethodGet, "/nope", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusNotFound {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(traceID) {
		t.Fatalf("invalid trace id: %q", traceID)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}
	if payload.Code != "http_error" {
		t.Fatalf("unexpected code: %q", payload.Code)
	}
	if payload.Message == "" {
		t.Fatalf("missing message")
	}
}

func TestReadyzRequiresDatabase(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != "not_ready" {
		t.Fatalf("unexpected code: %q", payload.Code)
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}
}

// TestTraceMiddlewarePreservesHttpFlusher 验证经过 TraceMiddleware 包装后底层 http.Flusher 能力不丢失。
// SSE/流式输出依赖此能力，一旦断言失败会导致连接建立但数据被缓冲不发送。
func TestTraceMiddlewarePreservesHttpFlusher(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)

	var capturedWriter nethttp.ResponseWriter
	inner := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		capturedWriter = w
		w.WriteHeader(nethttp.StatusOK)
	})

	handler := TraceMiddleware(inner, logger, false)

	underlying := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	handler.ServeHTTP(underlying, req)

	if capturedWriter == nil {
		t.Fatal("capturedWriter is nil")
	}
	if _, ok := capturedWriter.(nethttp.Flusher); !ok {
		t.Fatal("TraceMiddleware 包装后 ResponseWriter 丢失了 http.Flusher 接口")
	}
}

// TestThreadSubResourceRouting 验证 /v1/threads/{uuid}/messages 等 sub-resource 路径返回 404，
// 而不是 422（uuid parse 错误），证明路由拆分逻辑正确识别 segment。
func TestThreadSubResourceRouting(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	cases := []struct {
		path string
	}{
		{"/v1/threads/00000000-0000-0000-0000-000000000001/messages"},
		{"/v1/threads/00000000-0000-0000-0000-000000000001/runs"},
		{"/v1/threads/00000000-0000-0000-0000-000000000001/unknown"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(nethttp.MethodGet, tc.path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != nethttp.StatusNotFound {
			t.Fatalf("path=%s: expected 404, got %d body=%s", tc.path, recorder.Code, recorder.Body.String())
		}
	}
}
