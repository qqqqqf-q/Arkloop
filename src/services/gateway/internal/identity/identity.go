package identity

import (
	"context"
	"crypto/sha256"
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
// JWT 仅在 jwtSecret 已配置时参与身份提取；未配置时返回空字符串。
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
	return ""
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
// JWT 仅在 jwtSecret 已配置时参与身份提取；未配置时视为匿名。
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
	return Info{Type: IdentityAnonymous}
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
