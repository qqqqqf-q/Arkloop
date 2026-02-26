package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const apiKeyCacheKeyPrefix = "arkloop:api_keys:"

type apiKeyCacheEntry struct {
	OrgID   string `json:"org_id"`
	Revoked bool   `json:"revoked"`
}

// ExtractOrgID 从 Authorization header 提取 org_id（不验证 JWT 签名）。
// API Key (ak- 前缀) 通过 Redis 缓存查询；JWT 通过解码 payload。
// 返回空字符串表示无法提取。
func ExtractOrgID(ctx context.Context, authHeader string, rdb *redis.Client) string {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return ""
	}

	if strings.HasPrefix(token, "ak-") {
		return extractOrgIDFromAPIKey(ctx, token, rdb)
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
func ExtractInfo(ctx context.Context, authHeader string, rdb *redis.Client) Info {
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
