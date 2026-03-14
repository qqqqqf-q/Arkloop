// Package acptoken provides JWT-based temporary session tokens for ACP agents
// running in sandboxes. These tokens allow sandbox agents to call the LLM proxy
// endpoint without exposing real API keys.
package acptoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	tokenPrefix = "acpt_"
	tokenType   = "acp_session"
	algHS256    = "HS256"
)

var (
	ErrInvalidToken     = errors.New("acptoken: invalid token")
	ErrExpiredToken     = errors.New("acptoken: token has expired")
	ErrWrongTokenType   = errors.New("acptoken: wrong token type")
	ErrInvalidSignature = errors.New("acptoken: invalid signature")
	ErrEmptySecret      = errors.New("acptoken: secret must not be empty")
)

// jwtHeader is the fixed JWT header for all ACP session tokens.
type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// Claims represents the JWT claims for an ACP session token.
type Claims struct {
	Typ     string   `json:"typ"`
	Sub     string   `json:"sub"`
	Account string   `json:"account"`
	Models  []string `json:"models,omitempty"`
	Budget  int64    `json:"budget,omitempty"`
	Iat     int64    `json:"iat"`
	Exp     int64    `json:"exp"`
}

// IssueParams contains the parameters for issuing a new ACP session token.
type IssueParams struct {
	RunID     string        // run UUID
	AccountID string        // account UUID
	Models    []string      // allowed model names
	Budget    int64         // max total tokens (0 = unlimited)
	TTL       time.Duration // override default TTL (0 = use default)
}

// Issuer creates ACP session tokens.
type Issuer struct {
	secret     []byte
	defaultTTL time.Duration
}

// NewIssuer creates a new token issuer with the given HMAC secret and default TTL.
func NewIssuer(secret string, defaultTTL time.Duration) (*Issuer, error) {
	if secret == "" {
		return nil, ErrEmptySecret
	}
	return &Issuer{
		secret:     []byte(secret),
		defaultTTL: defaultTTL,
	}, nil
}

// Issue creates a signed ACP session token with the given parameters.
func (iss *Issuer) Issue(params IssueParams) (string, error) {
	ttl := iss.defaultTTL
	if params.TTL > 0 {
		ttl = params.TTL
	}

	now := time.Now()
	claims := Claims{
		Typ:     tokenType,
		Sub:     params.RunID,
		Account: params.AccountID,
		Models:  params.Models,
		Budget:  params.Budget,
		Iat:     now.Unix(),
		Exp:     now.Add(ttl).Unix(),
	}

	token, err := signJWT(iss.secret, &claims)
	if err != nil {
		return "", fmt.Errorf("acptoken: failed to sign token: %w", err)
	}

	return tokenPrefix + token, nil
}

// Validator verifies ACP session tokens.
type Validator struct {
	secret []byte
}

// NewValidator creates a new token validator with the given HMAC secret.
func NewValidator(secret string) (*Validator, error) {
	if secret == "" {
		return nil, ErrEmptySecret
	}
	return &Validator{secret: []byte(secret)}, nil
}

// ValidatedToken contains the verified claims from an ACP session token.
type ValidatedToken struct {
	RunID     string
	AccountID string
	Models    []string
	Budget    int64
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// AllowsModel returns true if the token permits the given model.
// An empty Models list allows all models (backward compatibility).
func (t *ValidatedToken) AllowsModel(model string) bool {
	if len(t.Models) == 0 {
		return true
	}
	for _, m := range t.Models {
		if m == model {
			return true
		}
	}
	return false
}

// Validate parses and verifies an ACP session token string.
func (v *Validator) Validate(tokenStr string) (*ValidatedToken, error) {
	raw, ok := strings.CutPrefix(tokenStr, tokenPrefix)
	if !ok {
		return nil, ErrInvalidToken
	}

	claims, err := verifyJWT(v.secret, raw)
	if err != nil {
		return nil, err
	}

	if claims.Typ != tokenType {
		return nil, ErrWrongTokenType
	}

	if time.Now().Unix() >= claims.Exp {
		return nil, ErrExpiredToken
	}

	return &ValidatedToken{
		RunID:     claims.Sub,
		AccountID: claims.Account,
		Models:    claims.Models,
		Budget:    claims.Budget,
		IssuedAt:  time.Unix(claims.Iat, 0),
		ExpiresAt: time.Unix(claims.Exp, 0),
	}, nil
}

// --- Manual JWT implementation using HMAC-SHA256 ---

func signJWT(secret []byte, claims *Claims) (string, error) {
	header := jwtHeader{Alg: algHS256, Typ: "JWT"}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	sig := hmacSHA256(secret, []byte(signingInput))
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return signingInput + "." + sigB64, nil
}

func verifyJWT(secret []byte, token string) (*Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSig := hmacSHA256(secret, []byte(signingInput))

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidSignature
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, ErrInvalidSignature
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}

	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrInvalidToken
	}

	return &claims, nil
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
