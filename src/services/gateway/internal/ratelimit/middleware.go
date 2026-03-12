package ratelimit

import (
	"context"
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
	rateLimitAccountKeyPrefix = "arkloop:ratelimit:account:"
	rateLimitIPKeyPrefix      = "arkloop:ratelimit:ip:"
)

// NewRateLimitMiddleware 返回限流中间件。
// SSE 请求（Accept: text/event-stream 且路径匹配已知 SSE 端点）跳过限流。
// 有效 JWT 或 API Key 按 account_id 限流；否则按客户端 IP 限流。
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

		rl := rateLimitKeyFromRequest(ctx, r, parser, secretBytes, redisClient)

		result, err := limiter.Consume(ctx, rl.key)
		cancel()
		if err != nil {
			if rl.authenticated {
				// 已认证请求 + Redis 故障 = fail-close，防止降级到共享 IP bucket
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"ratelimit.unavailable","message":"rate limit service temporarily unavailable"}`))
				return
			}
			// 匿名请求 fail-open，避免限流器故障阻断所有流量
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

// rateLimitResult 包含限流 key 及请求是否携带了认证凭据。
type rateLimitResult struct {
	key           string
	authenticated bool // 请求是否携带了认证凭据（JWT 或 API Key）
}

// rateLimitKeyFromRequest 提取限流 key。
// 优先从 JWT 或 API Key（Redis 缓存）取 account_id；匿名请求按客户端 IP 限流。
// 已认证但 account 提取失败时，仍标记为 authenticated 以便中间件 fail-close。
func rateLimitKeyFromRequest(ctx context.Context, r *http.Request, parser *jwt.Parser, secret []byte, rdb *redis.Client) rateLimitResult {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	bearer, ok := strings.CutPrefix(auth, "Bearer ")

	// API Key 路径：通过 identity 包查缓存
	if ok && strings.HasPrefix(bearer, "ak-") {
		if accountStr := identity.ExtractAccountID(ctx, auth, rdb, secret); accountStr != "" {
			return rateLimitResult{key: rateLimitAccountKeyPrefix + accountStr, authenticated: true}
		}
		// 有 API Key 但无法解析 account（Redis 故障等），标记为已认证
		return rateLimitResult{key: rateLimitIPKeyPrefix + clientIP(r), authenticated: true}
	}

	// JWT 路径：带签名验证
	if ok && bearer != "" {
		if accountID := extractAccountIDFromBearer(r, parser, secret); accountID != uuid.Nil {
			return rateLimitResult{key: rateLimitAccountKeyPrefix + accountID.String(), authenticated: true}
		}
		// 有 Bearer token 但签名无效或 account 缺失，视为匿名
		return rateLimitResult{key: rateLimitIPKeyPrefix + clientIP(r), authenticated: false}
	}

	return rateLimitResult{key: rateLimitIPKeyPrefix + clientIP(r), authenticated: false}
}

// extractAccountIDFromBearer 验证 Bearer JWT 并提取 account claim（兼容 org）。
// 验证失败（无 token、签名错误、已过期）均返回 uuid.Nil。
func extractAccountIDFromBearer(r *http.Request, parser *jwt.Parser, secret []byte) uuid.UUID {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return uuid.Nil
	}
	raw := strings.TrimPrefix(auth, "Bearer ")

	if len(secret) == 0 {
		return uuid.Nil
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

	var accountStr string
	if accountRaw, exists := claims["account"]; exists {
		if s, ok := accountRaw.(string); ok {
			accountStr = s
		}
	} else if orgRaw, exists := claims["org"]; exists {
		if s, ok := orgRaw.(string); ok {
			accountStr = s
		}
	}
	if accountStr == "" {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(accountStr)
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
