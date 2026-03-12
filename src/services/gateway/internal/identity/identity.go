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
	AccountID string `json:"account_id"`
	Revoked   bool   `json:"revoked"`
}

// ExtractAccountID 从 Authorization header 提取 account_id。
// JWT 仅在 jwtSecret 已配置时参与身份提取；未配置时返回空字符串。
func ExtractAccountID(ctx context.Context, authHeader string, rdb *redis.Client, jwtSecret []byte) string {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return ""
	}

	if strings.HasPrefix(token, "ak-") {
		return extractAccountIDFromAPIKey(ctx, token, rdb)
	}

	if len(jwtSecret) > 0 {
		return extractAccountIDFromJWT(token, jwtSecret)
	}
	return ""
}

func extractAccountIDFromAPIKey(ctx context.Context, rawKey string, rdb *redis.Client) string {
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

	return strings.TrimSpace(entry.AccountID)
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
	Type      IdentityType
	AccountID string
	UserID    string
}

// ExtractInfo 从 Authorization header 提取完整身份信息。
// JWT 仅在 jwtSecret 已配置时参与身份提取；未配置时视为匿名。
func ExtractInfo(ctx context.Context, authHeader string, rdb *redis.Client, jwtSecret []byte) Info {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return Info{Type: IdentityAnonymous}
	}

	if strings.HasPrefix(token, "ak-") {
		accountID := extractAccountIDFromAPIKey(ctx, token, rdb)
		if accountID == "" {
			return Info{Type: IdentityAnonymous}
		}
		return Info{Type: IdentityAPIKey, AccountID: accountID}
	}

	if len(jwtSecret) > 0 {
		return extractInfoFromJWTVerified(token, jwtSecret)
	}
	return Info{Type: IdentityAnonymous}
}

func extractAccountIDFromJWT(token string, secret []byte) string {
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
	if accountRaw, exists := claims["account"]; exists {
		if accountStr, ok := accountRaw.(string); ok {
			return strings.TrimSpace(accountStr)
		}
	}
	if orgRaw, exists := claims["org"]; exists {
		if orgStr, ok := orgRaw.(string); ok {
			return strings.TrimSpace(orgStr)
		}
	}
	return ""
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

	var accountID string
	if accountRaw, _ := claims["account"].(string); accountRaw != "" {
		accountID = accountRaw
	} else if orgRaw, _ := claims["org"].(string); orgRaw != "" {
		accountID = orgRaw
	}
	subRaw, _ := claims["sub"].(string)

	return Info{
		Type:      IdentityJWT,
		AccountID: strings.TrimSpace(accountID),
		UserID:    strings.TrimSpace(subRaw),
	}
}
