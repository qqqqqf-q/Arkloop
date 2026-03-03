package ratelimit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"arkloop/services/gateway/internal/clientip"
	"arkloop/services/gateway/internal/identity"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	rateLimitOrgKeyPrefix = "arkloop:ratelimit:org:"
	rateLimitIPKeyPrefix  = "arkloop:ratelimit:ip:"
)

// NewRateLimitMiddleware 返回限流中间件。
// SSE 请求（Accept: text/event-stream 且路径匹配已知 SSE 端点）跳过限流。
// 有效 JWT 或 API Key 按 org_id 限流；否则按客户端 IP 限流。
// Redis 不可用时 fail-open：放行请求，不阻断流量。
func NewRateLimitMiddleware(next http.Handler, limiter Limiter, jwtSecret string, redisTimeout time.Duration, rdb ...*redis.Client) http.Handler {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))
	secretBytes := []byte(jwtSecret)

	var redisClient *redis.Client
	if len(rdb) > 0 {
		redisClient = rdb[0]
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSSE(r) {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		cancel := func() {}
		if redisTimeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, redisTimeout)
		}

		key := rateLimitKeyFromRequest(ctx, r, parser, secretBytes, redisClient)

		result, err := limiter.Consume(ctx, key)
		cancel()
		if err != nil {
			// Redis 不可用时放行，避免限流器故障阻断所有流量
			next.ServeHTTP(w, r)
			return
		}

		if !result.Allowed {
			writeRateLimitExceeded(w, result)
			return
		}

		writeRateLimitHeaders(w, result)
		next.ServeHTTP(w, r)
	})
}

// rateLimitKeyFromRequest 提取限流 key。
// 优先从 JWT 或 API Key（Redis 缓存）取 org_id；失败时降级到客户端 IP。
func rateLimitKeyFromRequest(ctx context.Context, r *http.Request, parser *jwt.Parser, secret []byte, rdb *redis.Client) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	bearer, ok := strings.CutPrefix(auth, "Bearer ")

	// API Key 路径：通过 identity 包查缓存
	if ok && strings.HasPrefix(bearer, "ak-") {
		if orgStr := identity.ExtractOrgID(ctx, auth, rdb); orgStr != "" {
			return rateLimitOrgKeyPrefix + orgStr
		}
		return rateLimitIPKeyPrefix + clientIP(r)
	}

	// JWT 路径：带签名验证
	if orgID := extractOrgIDFromBearer(r, parser, secret); orgID != uuid.Nil {
		return rateLimitOrgKeyPrefix + orgID.String()
	}
	return rateLimitIPKeyPrefix + clientIP(r)
}

// extractOrgIDFromBearer 验证 Bearer JWT 并提取 org claim。
// 验证失败（无 token、签名错误、已过期）均返回 uuid.Nil。
func extractOrgIDFromBearer(r *http.Request, parser *jwt.Parser, secret []byte) uuid.UUID {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return uuid.Nil
	}
	raw := strings.TrimPrefix(auth, "Bearer ")

	// 仅解码 payload 以提取 org claim；签名验证由 API 负责。
	// Gateway 这里验证签名是为了防止客户端伪造 org_id 逃脱限流。
	if len(secret) == 0 {
		return extractOrgClaimUnsafe(raw)
	}

	token, err := parser.Parse(raw, func(t *jwt.Token) (any, error) {
		return secret, nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return uuid.Nil
	}
	orgRaw, exists := claims["org"]
	if !exists {
		return uuid.Nil
	}
	orgStr, ok := orgRaw.(string)
	if !ok {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(orgStr)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

// extractOrgClaimUnsafe 不验证签名，仅 base64 解码 payload 取 org claim。
// 仅在 jwtSecret 未配置时使用（降级模式）。
func extractOrgClaimUnsafe(raw string) uuid.UUID {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return uuid.Nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return uuid.Nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return uuid.Nil
	}
	orgRaw, ok := claims["org"]
	if !ok {
		return uuid.Nil
	}
	orgStr, ok := orgRaw.(string)
	if !ok {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(orgStr)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

// ssePathPattern 匹配 /v1/threads/{id}/runs/{id}/events 和 /v1/runs/{id}/events。
var ssePathPattern = regexp.MustCompile(`^/v1/(threads/[^/]+/)?runs/[^/]+/events$`)

// isSSE 判断请求是否为 SSE 长连接。
// 同时要求 Accept header 和路径匹配，防止任一条件被单独伪造绕过限流。
func isSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream") &&
		ssePathPattern.MatchString(r.URL.Path)
}

// clientIP 优先从 context 读 clientip 中间件解析的真实 IP，降级到 RemoteAddr。
// 与 ipfilter 的策略一致。
func clientIP(r *http.Request) string {
	if ip := clientip.FromContext(r.Context()); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeRateLimitHeaders(w http.ResponseWriter, result ConsumeResult) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetSecs))
}

func writeRateLimitExceeded(w http.ResponseWriter, result ConsumeResult) {
	writeRateLimitHeaders(w, result)
	if result.RetryAfterSecs > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", result.RetryAfterSecs))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"code":"ratelimit.exceeded","message":"rate limit exceeded"}`))
}
