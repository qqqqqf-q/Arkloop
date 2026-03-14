package acptoken

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-key-for-unit-tests"

func TestIssueAndValidate(t *testing.T) {
	issuer, err := NewIssuer(testSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	validator, err := NewValidator(testSecret)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	token, err := issuer.Issue(IssueParams{
		RunID:     "run-1234",
		AccountID: "acct-5678",
		Models:    []string{"claude-sonnet-4-5", "gpt-4o"},
		Budget:    100000,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if !strings.HasPrefix(token, tokenPrefix) {
		t.Fatalf("token should have prefix %q, got %q", tokenPrefix, token[:10])
	}

	vt, err := validator.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if vt.RunID != "run-1234" {
		t.Errorf("RunID = %q, want %q", vt.RunID, "run-1234")
	}
	if vt.AccountID != "acct-5678" {
		t.Errorf("AccountID = %q, want %q", vt.AccountID, "acct-5678")
	}
	if vt.Budget != 100000 {
		t.Errorf("Budget = %d, want %d", vt.Budget, 100000)
	}
	if len(vt.Models) != 2 || vt.Models[0] != "claude-sonnet-4-5" || vt.Models[1] != "gpt-4o" {
		t.Errorf("Models = %v, want [claude-sonnet-4-5 gpt-4o]", vt.Models)
	}
	if vt.IssuedAt.IsZero() {
		t.Error("IssuedAt should not be zero")
	}
	if vt.ExpiresAt.Before(vt.IssuedAt) {
		t.Error("ExpiresAt should be after IssuedAt")
	}
}

func TestExpiredToken(t *testing.T) {
	issuer, err := NewIssuer(testSecret, time.Second)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	validator, err := NewValidator(testSecret)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	token, err := issuer.Issue(IssueParams{
		RunID:     "run-expired",
		AccountID: "acct-5678",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)

	_, err = validator.Validate(token)
	if err != ErrExpiredToken {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}
}

func TestInvalidSignature(t *testing.T) {
	issuer, err := NewIssuer(testSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	validator, err := NewValidator(testSecret)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	token, err := issuer.Issue(IssueParams{
		RunID:     "run-tamper",
		AccountID: "acct-5678",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Tamper with the payload by replacing the run ID in the base64 payload.
	raw := strings.TrimPrefix(token, tokenPrefix)
	parts := strings.SplitN(raw, ".", 3)

	payloadJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
	tampered := strings.Replace(string(payloadJSON), "run-tamper", "run-HACKED", 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(tampered))
	tamperedToken := tokenPrefix + strings.Join(parts, ".")

	_, err = validator.Validate(tamperedToken)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestWrongTokenType(t *testing.T) {
	issuer, err := NewIssuer(testSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	validator, err := NewValidator(testSecret)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	// Issue a valid token, then re-sign with wrong typ.
	claims := Claims{
		Typ:     "access",
		Sub:     "run-wrong-type",
		Account: "acct-5678",
		Iat:     time.Now().Unix(),
		Exp:     time.Now().Add(10 * time.Minute).Unix(),
	}
	jwt, err := signJWT(issuer.secret, &claims)
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}
	token := tokenPrefix + jwt

	_, err = validator.Validate(token)
	if err != ErrWrongTokenType {
		t.Errorf("expected ErrWrongTokenType, got %v", err)
	}
}

func TestAllowsModel(t *testing.T) {
	vt := &ValidatedToken{
		Models: []string{"claude-sonnet-4-5", "gpt-4o"},
	}

	if !vt.AllowsModel("claude-sonnet-4-5") {
		t.Error("should allow claude-sonnet-4-5")
	}
	if !vt.AllowsModel("gpt-4o") {
		t.Error("should allow gpt-4o")
	}
	if vt.AllowsModel("gpt-3.5-turbo") {
		t.Error("should not allow gpt-3.5-turbo")
	}
}

func TestEmptyModelsAllowsAll(t *testing.T) {
	vt := &ValidatedToken{
		Models: nil,
	}

	if !vt.AllowsModel("anything") {
		t.Error("empty models should allow any model")
	}
	if !vt.AllowsModel("claude-sonnet-4-5") {
		t.Error("empty models should allow any model")
	}

	vt2 := &ValidatedToken{
		Models: []string{},
	}

	if !vt2.AllowsModel("anything") {
		t.Error("empty slice should allow any model")
	}
}

func TestCustomTTL(t *testing.T) {
	issuer, err := NewIssuer(testSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	validator, err := NewValidator(testSecret)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	token, err := issuer.Issue(IssueParams{
		RunID:     "run-custom-ttl",
		AccountID: "acct-5678",
		TTL:       30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	vt, err := validator.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	duration := vt.ExpiresAt.Sub(vt.IssuedAt)
	if duration < 29*time.Minute || duration > 31*time.Minute {
		t.Errorf("expected ~30min TTL, got %v", duration)
	}
}

func TestMissingPrefix(t *testing.T) {
	validator, err := NewValidator(testSecret)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	_, err = validator.Validate("not-a-valid-token")
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestWrongSecret(t *testing.T) {
	issuer, err := NewIssuer(testSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	validator, err := NewValidator("wrong-secret")
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	token, err := issuer.Issue(IssueParams{
		RunID:     "run-wrong-secret",
		AccountID: "acct-5678",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = validator.Validate(token)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestEmptySecret(t *testing.T) {
	_, err := NewIssuer("", 10*time.Minute)
	if err != ErrEmptySecret {
		t.Errorf("NewIssuer: expected ErrEmptySecret, got %v", err)
	}
	_, err = NewValidator("")
	if err != ErrEmptySecret {
		t.Errorf("NewValidator: expected ErrEmptySecret, got %v", err)
	}
}

func TestTokenIsValidJWT(t *testing.T) {
	issuer, err := NewIssuer(testSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	token, err := issuer.Issue(IssueParams{
		RunID:     "run-jwt",
		AccountID: "acct-5678",
		Models:    []string{"claude-sonnet-4-5"},
		Budget:    50000,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	raw := strings.TrimPrefix(token, tokenPrefix)
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) != 3 {
		t.Fatal("JWT should have 3 parts")
	}

	// Verify header
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header["alg"] != "HS256" {
		t.Errorf("header alg = %q, want HS256", header["alg"])
	}

	// Verify payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if claims.Typ != tokenType {
		t.Errorf("claims typ = %q, want %q", claims.Typ, tokenType)
	}
	if claims.Sub != "run-jwt" {
		t.Errorf("claims sub = %q, want run-jwt", claims.Sub)
	}
}
