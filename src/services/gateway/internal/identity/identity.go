package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// jwtParser 复用 parser 实例，限定 HS256 算法防止 alg 切换攻击。
var jwtParser = jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))

const apiKeyCacheKeyPrefix = "arkloop:api_keys:"

type apiKeyCacheEntry struct {
	OrgID   string `json:"org_id"`
	Revoked bool   `json:"revoked"`
}

// ExtractOrgID 从 Authorization header 提取 org_id。
// jwtSecret 非空时验证 JWT 签名；为空时降级到无签名解码（兼容未配置场景）。
func ExtractOrgID(ctx context.Context, authHeader string, rdb *redis.Client, jwtSecret []byte) string {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return ""
	}

	if strings.HasPrefix(token, "ak-") {
		return extractOrgIDFromAPIKey(ctx, token, rdb)
	}

	if len(jwtSecret) > 0 {
		return extractOrgIDFromJWTVerified(token, jwtSecret)
	}
	return extractOrgIDFromJWTPayload(token)
}

func extractOrgIDFromAPIKey(ctx context.Context, rawKey string, rdb *redis.Client) string {
	if rdb == nil {
		return ""
	}

	digest := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(digest[:])
	redisKey := fmt.Sprintf("%s%s", apiKeyCacheKeyPrefix, keyHash)

	raw, err := rdb.Get(ctx, redisKey).Bytes()
	if err != nil {
		return ""
	}

	var entry apiKeyCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return ""
	}
	if entry.Revoked {
		return ""
	}

	return strings.TrimSpace(entry.OrgID)
}

// extractOrgIDFromJWTPayload 不验证签名，仅 base64 解码 JWT payload 取 org claim。
func extractOrgIDFromJWTPayload(token string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return ""
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var claims struct {
		Org string `json:"org"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}

	return strings.TrimSpace(claims.Org)
}

// IdentityType 标识身份来源。
type IdentityType string

const (
	IdentityJWT       IdentityType = "jwt"
	IdentityAPIKey    IdentityType = "api_key"
	IdentityAnonymous IdentityType = "anonymous"
)

// Info 包含从请求中提取的身份信息。
type Info struct {
	Type   IdentityType
	OrgID  string
	UserID string
}

// ExtractInfo 从 Authorization header 提取完整身份信息。
// jwtSecret 非空时验证 JWT 签名；为空时降级到无签名解码。
func ExtractInfo(ctx context.Context, authHeader string, rdb *redis.Client, jwtSecret []byte) Info {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return Info{Type: IdentityAnonymous}
	}

	if strings.HasPrefix(token, "ak-") {
		orgID := extractOrgIDFromAPIKey(ctx, token, rdb)
		if orgID == "" {
			return Info{Type: IdentityAnonymous}
		}
		return Info{Type: IdentityAPIKey, OrgID: orgID}
	}

	if len(jwtSecret) > 0 {
		return extractInfoFromJWTVerified(token, jwtSecret)
	}
	return extractInfoFromJWTPayload(token)
}

func extractInfoFromJWTPayload(token string) Info {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return Info{Type: IdentityAnonymous}
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Info{Type: IdentityAnonymous}
	}

	var claims struct {
		Sub string `json:"sub"`
		Org string `json:"org"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Info{Type: IdentityAnonymous}
	}

	return Info{
		Type:   IdentityJWT,
		OrgID:  strings.TrimSpace(claims.Org),
		UserID: strings.TrimSpace(claims.Sub),
	}
}

func extractOrgIDFromJWTVerified(token string, secret []byte) string {
	parsed, err := jwtParser.Parse(token, func(t *jwt.Token) (any, error) {
		return secret, nil
	})
	if err != nil || !parsed.Valid {
		return ""
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	orgRaw, exists := claims["org"]
	if !exists {
		return ""
	}
	orgStr, ok := orgRaw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(orgStr)
}

func extractInfoFromJWTVerified(token string, secret []byte) Info {
	parsed, err := jwtParser.Parse(token, func(t *jwt.Token) (any, error) {
		return secret, nil
	})
	if err != nil || !parsed.Valid {
		return Info{Type: IdentityAnonymous}
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Info{Type: IdentityAnonymous}
	}

	orgRaw, _ := claims["org"].(string)
	subRaw, _ := claims["sub"].(string)

	return Info{
		Type:   IdentityJWT,
		OrgID:  strings.TrimSpace(orgRaw),
		UserID: strings.TrimSpace(subRaw),
	}
}
