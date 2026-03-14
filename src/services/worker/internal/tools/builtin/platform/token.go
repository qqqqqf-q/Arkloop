package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// TokenProvider 生成用于 internal API 调用的短期 JWT。
// 不引入 golang-jwt 依赖，直接用 HS256 手工签名。
type TokenProvider struct {
	secret    []byte
	userID    string
	accountID string
	role      string
	ttl       time.Duration

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

func NewTokenProvider(jwtSecret []byte, systemAgentUserID, accountID, role string, ttl time.Duration) *TokenProvider {
	return &TokenProvider{
		secret:    jwtSecret,
		userID:    systemAgentUserID,
		accountID: accountID,
		role:      role,
		ttl:       ttl,
	}
}

func (p *TokenProvider) Token() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != "" && time.Now().Before(p.expiresAt.Add(-30*time.Second)) {
		return p.cached
	}
	now := time.Now()
	exp := now.Add(p.ttl)
	p.cached = p.sign(now, exp)
	p.expiresAt = exp
	return p.cached
}

func (p *TokenProvider) sign(now, exp time.Time) string {
	header := base64url(mustJSON(map[string]string{"alg": "HS256", "typ": "JWT"}))
	claims := map[string]any{
		"sub":     p.userID,
		"typ":     "access",
		"account": p.accountID,
		"role":    p.role,
		"iat":     now.Unix(),
		"exp":     exp.Unix(),
	}
	payload := base64url(mustJSON(claims))
	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(sigInput))
	sig := base64url(mac.Sum(nil))
	return sigInput + "." + sig
}

func base64url(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
