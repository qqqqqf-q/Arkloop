package entitlement

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

const EntitlementCacheSignatureSuffix = ":sig"

const (
	entitlementCacheSigningSecretEnv            = "ARKLOOP_AUTH_JWT_SECRET"
	minEntitlementCacheSigningSecretLengthBytes = 32
)

func EntitlementCacheSigningEnabled() bool {
	secret := strings.TrimSpace(os.Getenv(entitlementCacheSigningSecretEnv))
	return len(secret) >= minEntitlementCacheSigningSecretLengthBytes
}

// ComputeEntitlementCacheSignature 计算权益缓存的签名。
// 签名绑定 cacheKey，避免跨 account/key 的复制重放。
func ComputeEntitlementCacheSignature(cacheKey, rawValue string) (sig string, ok bool) {
	secret := strings.TrimSpace(os.Getenv(entitlementCacheSigningSecretEnv))
	if len(secret) < minEntitlementCacheSigningSecretLengthBytes {
		return "", false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(cacheKey))
	mac.Write([]byte("\n"))
	mac.Write([]byte(rawValue))
	return hex.EncodeToString(mac.Sum(nil)), true
}
