package auth

import (
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
		return "token 已过期"
	}
	return e.message
}

type TokenInvalidError struct {
	message string
}

func (e TokenInvalidError) Error() string {
	if e.message == "" {
		return "token 无效"
	}
	return e.message
}

type VerifiedAccessToken struct {
	UserID   uuid.UUID
	IssuedAt time.Time
}

type JwtAccessTokenService struct {
	secret     []byte
	ttlSeconds int
}

func NewJwtAccessTokenService(secret string, ttlSeconds int) (*JwtAccessTokenService, error) {
	if secret == "" {
		return nil, errors.New("secret 不能为空")
	}
	if ttlSeconds <= 0 {
		return nil, errors.New("ttlSeconds 必须为正数")
	}
	return &JwtAccessTokenService{
		secret:     []byte(secret),
		ttlSeconds: ttlSeconds,
	}, nil
}

func (s *JwtAccessTokenService) Issue(userID uuid.UUID, now time.Time) (string, error) {
	if userID == uuid.Nil {
		return "", errors.New("user_id 不能为空")
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
			return VerifiedAccessToken{}, TokenExpiredError{message: "token 已过期"}
		}
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}
	if parsed == nil || !parsed.Valid {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}

	if _, ok := claims["exp"]; !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}

	typ, ok := claims["typ"]
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}
	if typStr, ok := typ.(string); !ok || typStr != accessTokenType {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 类型不正确"}
	}

	sub, ok := claims["sub"]
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}
	subStr, ok := sub.(string)
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token subject 无效"}
	}
	userID, err := uuid.Parse(subStr)
	if err != nil {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token subject 无效"}
	}

	iat, ok := claims["iat"]
	if !ok {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token 无效"}
	}
	issuedAt, err := parseIAT(iat)
	if err != nil {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token iat 无效"}
	}

	return VerifiedAccessToken{
		UserID:   userID,
		IssuedAt: issuedAt,
	}, nil
}

func timestampFloatSeconds(value time.Time) float64 {
	return float64(value.UnixNano()) / 1e9
}

func parseIAT(value any) (time.Time, error) {
	iat, ok := numericToFloat64(value)
	if !ok {
		return time.Time{}, errors.New("iat 不是数字")
	}
	if math.IsNaN(iat) || math.IsInf(iat, 0) {
		return time.Time{}, errors.New("iat 无效")
	}
	sec, frac := math.Modf(iat)
	if sec < 0 {
		return time.Time{}, errors.New("iat 无效")
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
