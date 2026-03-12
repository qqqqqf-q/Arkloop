package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	jwtAlgorithmHS256 = "HS256"
	accessTokenType   = "access"
)

type TokenExpiredError struct {
	message string
}

func (e TokenExpiredError) Error() string {
	if e.message == "" {
		return "token expired"
	}
	return e.message
}

type TokenInvalidError struct {
	message string
}

func (e TokenInvalidError) Error() string {
	if e.message == "" {
		return "token invalid"
	}
	return e.message
}

type VerifiedAccessToken struct {
	UserID      uuid.UUID
	AccountID   uuid.UUID // uuid.Nil 表示旧 token 无 account claim
	AccountRole string    // 账户角色（如 owner/member/platform_admin）；旧 token 为空
	IssuedAt    time.Time
}

type JwtAccessTokenService struct {
	secret                 []byte
	ttlSeconds             int
	refreshTokenTTLSeconds int
}

func NewJwtAccessTokenService(secret string, ttlSeconds int, refreshTokenTTLSeconds int) (*JwtAccessTokenService, error) {
	if secret == "" {
		return nil, errors.New("secret must not be empty")
	}
	if ttlSeconds <= 0 {
		return nil, errors.New("ttlSeconds must be positive")
	}
	if refreshTokenTTLSeconds <= 0 {
		return nil, errors.New("refreshTokenTTLSeconds must be positive")
	}
	return &JwtAccessTokenService{
		secret:                 []byte(secret),
		ttlSeconds:             ttlSeconds,
		refreshTokenTTLSeconds: refreshTokenTTLSeconds,
	}, nil
}

func (s *JwtAccessTokenService) RefreshTokenTTLSeconds() int {
	if s == nil {
		return 0
	}
	return s.refreshTokenTTLSeconds
}

func (s *JwtAccessTokenService) Issue(userID uuid.UUID, accountID uuid.UUID, accountRole string, now time.Time) (string, error) {
	if userID == uuid.Nil {
		return "", errors.New("user_id must not be nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	issuedAt := now.UTC()
	expiresAt := issuedAt.Add(time.Duration(s.ttlSeconds) * time.Second)
	claims := jwt.MapClaims{
		"sub": userID.String(),
		"typ": accessTokenType,
		"iat": timestampFloatSeconds(issuedAt),
		"exp": expiresAt.Unix(),
	}
	if accountID != uuid.Nil {
		claims["account"] = accountID.String()
	}
	if accountRole != "" {
		claims["role"] = accountRole
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", err
	}
	return signed, nil
}

func (s *JwtAccessTokenService) Verify(token string) (VerifiedAccessToken, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwtAlgorithmHS256}))
	parsed, err := parser.Parse(token, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return VerifiedAccessToken{}, TokenExpiredError{message: "token expired"}
		}
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}
	if parsed == nil || !parsed.Valid {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}

	if _, ok := claims["exp"]; !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}

	typ, ok := claims["typ"]
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}
	if typStr, ok := typ.(string); !ok || typStr != accessTokenType {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token type incorrect"}
	}

	sub, ok := claims["sub"]
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}
	subStr, ok := sub.(string)
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token subject invalid"}
	}
	userID, err := uuid.Parse(subStr)
	if err != nil {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token subject invalid"}
	}

	iat, ok := claims["iat"]
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token invalid"}
	}
	issuedAt, err := parseIAT(iat)
	if err != nil {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token iat invalid"}
	}

	var accountID uuid.UUID
	if raw, exists := claims["account"]; exists {
		if str, ok := raw.(string); ok {
			if parsed, err := uuid.Parse(str); err == nil {
				accountID = parsed
			}
		}
	}
	// 向后兼容：旧 token 仍使用 "org" claim
	if accountID == uuid.Nil {
		if raw, exists := claims["org"]; exists {
			if str, ok := raw.(string); ok {
				if parsed, err := uuid.Parse(str); err == nil {
					accountID = parsed
				}
			}
		}
	}

	accountRole := ""
	if roleRaw, exists := claims["role"]; exists {
		if roleStr, ok := roleRaw.(string); ok {
			accountRole = roleStr
		}
	}

	return VerifiedAccessToken{
		UserID:      userID,
		AccountID:   accountID,
		AccountRole: accountRole,
		IssuedAt:    issuedAt,
	}, nil
}

func timestampFloatSeconds(value time.Time) float64 {
	return float64(value.UnixNano()) / 1e9
}

func parseIAT(value any) (time.Time, error) {
	iat, ok := numericToFloat64(value)
	if !ok {
		return time.Time{}, errors.New("iat is not a number")
	}
	if math.IsNaN(iat) || math.IsInf(iat, 0) {
		return time.Time{}, errors.New("iat invalid")
	}
	sec, frac := math.Modf(iat)
	if sec < 0 {
		return time.Time{}, errors.New("iat invalid")
	}

	nsec := int64(frac * 1e9)
	if nsec < 0 {
		nsec = -nsec
	}
	return time.Unix(int64(sec), nsec).UTC(), nil
}

func numericToFloat64(value any) (float64, bool) {
	switch casted := value.(type) {
	case float64:
		return casted, true
	case float32:
		return float64(casted), true
	case int:
		return float64(casted), true
	case int64:
		return float64(casted), true
	case int32:
		return float64(casted), true
	case json.Number:
		parsed, err := casted.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

// IssueRefreshToken 生成随机 Refresh Token，返回 plaintext（存客户端）、hash（存 DB）和过期时间。
func (s *JwtAccessTokenService) IssueRefreshToken(now time.Time) (plaintext string, hash string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", time.Time{}, errors.New("generate refresh token: " + err.Error())
	}
	plaintext = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(sum[:])
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt = now.Add(time.Duration(s.refreshTokenTTLSeconds) * time.Second)
	return plaintext, hash, expiresAt, nil
}
