package platform

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTokenProvider_GeneratesValidJWT(t *testing.T) {
	tp := NewTokenProvider([]byte("test-secret-at-least-32-bytes-long!!"), "user-1", "account-1", "platform_admin", 15*time.Minute)
	token := tp.Token()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}

	header := decodeJWTPart(t, parts[0])
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		t.Fatalf("unexpected header: %v", header)
	}

	claims := decodeJWTPart(t, parts[1])
	if claims["sub"] != "user-1" {
		t.Fatalf("expected sub=user-1, got %v", claims["sub"])
	}
	if claims["account"] != "account-1" {
		t.Fatalf("expected account=account-1, got %v", claims["account"])
	}
	if claims["role"] != "platform_admin" {
		t.Fatalf("expected role=platform_admin, got %v", claims["role"])
	}
	if claims["typ"] != "access" {
		t.Fatalf("expected typ=access, got %v", claims["typ"])
	}
	exp, ok := claims["exp"].(float64)
	if !ok || exp == 0 {
		t.Fatalf("missing or invalid exp claim")
	}
}

func TestTokenProvider_CachesWithinTTL(t *testing.T) {
	tp := NewTokenProvider([]byte("test-secret-at-least-32-bytes-long!!"), "u", "a", "r", 15*time.Minute)
	t1 := tp.Token()
	t2 := tp.Token()
	if t1 != t2 {
		t.Fatal("expected same cached token within TTL")
	}
}

func TestTokenProvider_RefreshesAfterExpiry(t *testing.T) {
	tp := NewTokenProvider([]byte("test-secret-at-least-32-bytes-long!!"), "u", "a", "r", 1*time.Second)
	tp.Token()

	oldExpiry := tp.expiresAt

	// 强制过期
	tp.mu.Lock()
	tp.expiresAt = time.Now().Add(-1 * time.Minute)
	tp.cached = ""
	tp.mu.Unlock()

	tp.Token()

	// 验证 expiresAt 被更新
	tp.mu.Lock()
	newExpiry := tp.expiresAt
	tp.mu.Unlock()

	if !newExpiry.After(oldExpiry) {
		t.Fatal("expected expiresAt to be refreshed after expiry")
	}
}

func TestTokenProvider_ConcurrentAccess(t *testing.T) {
	tp := NewTokenProvider([]byte("test-secret-at-least-32-bytes-long!!"), "u", "a", "r", 15*time.Minute)
	var wg sync.WaitGroup
	tokens := make([]string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tokens[idx] = tp.Token()
		}(i)
	}
	wg.Wait()

	// 所有 token 应该相同（缓存命中）
	for i := 1; i < len(tokens); i++ {
		if tokens[i] != tokens[0] {
			t.Fatalf("token mismatch at index %d", i)
		}
	}
}

func decodeJWTPart(t *testing.T, part string) map[string]any {
	t.Helper()
	// 补齐 base64 padding
	switch len(part) % 4 {
	case 2:
		part += "=="
	case 3:
		part += "="
	}
	data, err := base64.URLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("decode jwt part: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal jwt part: %v", err)
	}
	return m
}
