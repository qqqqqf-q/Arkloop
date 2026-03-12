package ratelimit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testJWTSecret = "this-is-a-test-secret-that-is-at-least-32-chars"

// mockLimiter 可控的限流器 mock。
type mockLimiter struct {
	result  ConsumeResult
	err     error
	lastKey string
}

func (m *mockLimiter) Consume(_ context.Context, key string) (ConsumeResult, error) {
	m.lastKey = key
	return m.result, m.err
}

type blockingLimiter struct{}

func (b *blockingLimiter) Consume(ctx context.Context, _ string) (ConsumeResult, error) {
	<-ctx.Done()
	return ConsumeResult{}, ctx.Err()
}

func makeJWT(t *testing.T, accountID uuid.UUID) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub": uuid.New().String(),
		"typ": "access",
		"iat": float64(time.Now().Unix()),
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}
	if accountID != uuid.Nil {
		claims["account"] = accountID.String()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signed
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddleware_AllowedRequest(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: true, Remaining: 59}}
	accountID := uuid.New()
	token := makeJWT(t, accountID)

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ml.lastKey != rateLimitAccountKeyPrefix+accountID.String() {
		t.Fatalf("unexpected rate limit key: %s", ml.lastKey)
	}
}

func TestMiddleware_BlockedRequest(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: false, RetryAfterSecs: 5}}
	accountID := uuid.New()
	token := makeJWT(t, accountID)

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "5" {
		t.Fatalf("expected Retry-After: 5, got %q", rec.Header().Get("Retry-After"))
	}
}

func TestMiddleware_NoJWT_FallsBackToIP(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: true}}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.RemoteAddr = "203.0.113.7:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ml.lastKey != rateLimitIPKeyPrefix+"203.0.113.7" {
		t.Fatalf("unexpected rate limit key: %s", ml.lastKey)
	}
}

func TestMiddleware_SSEByAcceptHeader_Skipped(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: false}} // 即使超限也应放行

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/abc/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SSE request should bypass ratelimit, got %d", rec.Code)
	}
	if ml.lastKey != "" {
		t.Fatal("Consume should not be called for SSE requests")
	}
}

func TestMiddleware_SSEPathOnly_NotSkipped(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: false, RetryAfterSecs: 5}}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	// 只有路径匹配但缺少 Accept header 时不应跳过限流
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/abc/events", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("SSE path without Accept header should NOT bypass ratelimit, got %d", rec.Code)
	}
}

func TestMiddleware_SSEAcceptOnly_NotSkipped(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: false, RetryAfterSecs: 5}}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	// 只有 Accept header 但路径不匹配时不应跳过限流
	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("non-SSE path with Accept header should NOT bypass ratelimit, got %d", rec.Code)
	}
}

func TestMiddleware_SSEThreadsPath_Skipped(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: false}}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	// /v1/threads/{id}/runs/{id}/events 也是合法 SSE 端点
	req := httptest.NewRequest(http.MethodGet, "/v1/threads/t1/runs/r1/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SSE threads path should bypass ratelimit, got %d", rec.Code)
	}
}

func TestMiddleware_LimiterError_FailOpen(t *testing.T) {
	ml := &mockLimiter{err: io.ErrUnexpectedEOF}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Redis 不可用时应放行（fail-open）
	if rec.Code != http.StatusOK {
		t.Fatalf("limiter error should fail open, got %d", rec.Code)
	}
}

func TestMiddleware_ContextTimeout_FailOpen(t *testing.T) {
	bl := &blockingLimiter{}

	h := NewRateLimitMiddleware(okHandler(), bl, testJWTSecret, 10*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	start := time.Now()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeout should fail open, got %d", rec.Code)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("request should not hang, took %s", time.Since(start))
	}
}

func TestMiddleware_InvalidJWT_FallsBackToIP(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: true}}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	req.RemoteAddr = "10.0.0.2:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ml.lastKey != rateLimitIPKeyPrefix+"10.0.0.2" {
		t.Fatalf("unexpected key: %s", ml.lastKey)
	}
}

func TestMiddleware_MissingJWTSecretFallsBackToIP(t *testing.T) {
	ml := &mockLimiter{result: ConsumeResult{Allowed: true}}
	accountID := uuid.New()
	token := makeJWT(t, accountID)

	h := NewRateLimitMiddleware(okHandler(), ml, "", 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "10.0.0.3:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ml.lastKey != rateLimitIPKeyPrefix+"10.0.0.3" {
		t.Fatalf("unexpected key: %s", ml.lastKey)
	}
}

func TestMiddleware_LimiterError_AuthenticatedAPIKey_FailClosed(t *testing.T) {
	ml := &mockLimiter{err: io.ErrUnexpectedEOF}

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Bearer ak-test-key-12345")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("authenticated request with limiter error should fail closed (503), got %d", rec.Code)
	}
}

func TestMiddleware_LimiterError_AuthenticatedJWT_FailClosed(t *testing.T) {
	ml := &mockLimiter{err: io.ErrUnexpectedEOF}
	accountID := uuid.New()
	token := makeJWT(t, accountID)

	h := NewRateLimitMiddleware(okHandler(), ml, testJWTSecret, 0)

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("authenticated JWT request with limiter error should fail closed (503), got %d", rec.Code)
	}
}
