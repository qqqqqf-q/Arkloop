package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	authFlowTokenType = "auth_flow"
	authFlowTokenTTL  = 10 * time.Minute
)

type VerifiedAuthFlowToken struct {
	UserID   uuid.UUID
	IssuedAt time.Time
}

func (s *JwtAccessTokenService) IssueAuthFlowToken(userID uuid.UUID, now time.Time) (string, error) {
	if userID == uuid.Nil {
		return "", errors.New("user_id must not be nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	issuedAt := now.UTC()
	expiresAt := issuedAt.Add(authFlowTokenTTL)
	claims := jwt.MapClaims{
		"sub": userID.String(),
		"typ": authFlowTokenType,
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

func (s *JwtAccessTokenService) VerifyAuthFlowToken(token string) (VerifiedAuthFlowToken, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwtAlgorithmHS256}))
	parsed, err := parser.Parse(token, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return VerifiedAuthFlowToken{}, TokenExpiredError{message: "token expired"}
		}
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}
	if parsed == nil || !parsed.Valid {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}
	if _, ok := claims["exp"]; !ok {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}

	typ, ok := claims["typ"]
	if !ok {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}
	if typStr, ok := typ.(string); !ok || typStr != authFlowTokenType {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token type incorrect"}
	}

	sub, ok := claims["sub"]
	if !ok {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}
	subStr, ok := sub.(string)
	if !ok {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token subject invalid"}
	}
	userID, err := uuid.Parse(subStr)
	if err != nil {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token subject invalid"}
	}

	iat, ok := claims["iat"]
	if !ok {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token invalid"}
	}
	issuedAt, err := parseIAT(iat)
	if err != nil {
		return VerifiedAuthFlowToken{}, TokenInvalidError{message: "token iat invalid"}
	}

	return VerifiedAuthFlowToken{UserID: userID, IssuedAt: issuedAt}, nil
}
