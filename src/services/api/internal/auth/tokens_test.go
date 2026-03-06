package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestNewJwtAccessTokenService(t *testing.T) {
	cases := []struct {
		name       string
		secret     string
		ttl        int
		refreshTTL int
		wantErr    bool
	}{
		{"valid", "secret-key", 3600, 86400, false},
		{"empty_secret", "", 3600, 86400, true},
		{"zero_ttl", "s", 0, 86400, true},
		{"negative_ttl", "s", -1, 86400, true},
		{"zero_refresh_ttl", "s", 3600, 0, true},
		{"negative_refresh_ttl", "s", 3600, -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := NewJwtAccessTokenService(tc.secret, tc.ttl, tc.refreshTTL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if svc == nil {
				t.Fatalf("service must not be nil")
			}
		})
	}
}

func TestIssueRejectsNilUserID(t *testing.T) {
	svc := mustTokenService(t)
	_, err := svc.Issue(uuid.Nil, uuid.New(), "owner", time.Now())
	if err == nil {
		t.Fatalf("expected error for nil userID")
	}
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	svc := mustTokenService(t)
	userID := uuid.New()
	orgID := uuid.New()
	orgRole := "owner"
	now := time.Now().UTC().Truncate(time.Second)

	token, err := svc.Issue(userID, orgID, orgRole, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatalf("token must not be empty")
	}

	verified, err := svc.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.UserID != userID {
		t.Errorf("userID: got %s, want %s", verified.UserID, userID)
	}
	if verified.OrgID != orgID {
		t.Errorf("orgID: got %s, want %s", verified.OrgID, orgID)
	}
	if verified.OrgRole != orgRole {
		t.Errorf("orgRole: got %q, want %q", verified.OrgRole, orgRole)
	}
	if verified.IssuedAt.Unix() != now.Unix() {
		t.Errorf("issuedAt: got %v, want %v", verified.IssuedAt.Unix(), now.Unix())
	}
}

func TestIssueWithoutOrgID(t *testing.T) {
	svc := mustTokenService(t)
	userID := uuid.New()
	now := time.Now().UTC()

	token, err := svc.Issue(userID, uuid.Nil, "", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	verified, err := svc.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.OrgID != uuid.Nil {
		t.Errorf("orgID should be Nil when not set, got %s", verified.OrgID)
	}
	if verified.OrgRole != "" {
		t.Errorf("orgRole should be empty when not set, got %q", verified.OrgRole)
	}
	if verified.UserID != userID {
		t.Errorf("userID: got %s, want %s", verified.UserID, userID)
	}
}

func TestIssueZeroNowDoesNotError(t *testing.T) {
	svc := mustTokenService(t)
	token, err := svc.Issue(uuid.New(), uuid.New(), "owner", time.Time{})
	if err != nil {
		t.Fatalf("issue with zero time: %v", err)
	}
	if token == "" {
		t.Fatalf("token must not be empty")
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	svc, err := NewJwtAccessTokenService("test-secret", 1, 86400)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	past := time.Now().UTC().Add(-10 * time.Second)
	token, err := svc.Issue(uuid.New(), uuid.New(), "owner", past)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = svc.Verify(token)
	if err == nil {
		t.Fatalf("expected error for expired token")
	}
	var expiredErr TokenExpiredError
	if !errors.As(err, &expiredErr) {
		t.Fatalf("expected TokenExpiredError, got %T: %v", err, err)
	}
}

func TestVerifyInvalidTokens(t *testing.T) {
	svc := mustTokenService(t)

	cases := []struct {
		name  string
		token string
	}{
		{"garbage", "not-a-jwt-token"},
		{"empty", ""},
		{"partial", "eyJhbGciOiJIUzI1NiJ9."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Verify(tc.token)
			if err == nil {
				t.Fatalf("expected error for invalid token %q", tc.token)
			}
			var invalidErr TokenInvalidError
			if !errors.As(err, &invalidErr) {
				t.Fatalf("expected TokenInvalidError, got %T: %v", err, err)
			}
		})
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	svc1, _ := NewJwtAccessTokenService("secret-1", 3600, 86400)
	svc2, _ := NewJwtAccessTokenService("secret-2", 3600, 86400)

	token, err := svc1.Issue(uuid.New(), uuid.New(), "owner", time.Now())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = svc2.Verify(token)
	if err == nil {
		t.Fatalf("expected error for wrong secret")
	}
	var invalidErr TokenInvalidError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected TokenInvalidError, got %T: %v", err, err)
	}
}

func TestVerifyMissingTypClaim(t *testing.T) {
	secret := "test-secret-for-manual"
	claims := jwt.MapClaims{
		"sub": uuid.New().String(),
		"iat": float64(time.Now().Unix()),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	svc, _ := NewJwtAccessTokenService(secret, 3600, 86400)
	_, err = svc.Verify(signed)
	if err == nil {
		t.Fatalf("expected error for missing typ claim")
	}
	var invalidErr TokenInvalidError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected TokenInvalidError, got %T: %v", err, err)
	}
}

func TestVerifyWrongTypClaim(t *testing.T) {
	secret := "test-secret-for-typ"
	claims := jwt.MapClaims{
		"sub": uuid.New().String(),
		"typ": "refresh",
		"iat": float64(time.Now().Unix()),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	svc, _ := NewJwtAccessTokenService(secret, 3600, 86400)
	_, err = svc.Verify(signed)
	if err == nil {
		t.Fatalf("expected error for wrong typ claim")
	}
	var invalidErr TokenInvalidError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected TokenInvalidError, got %T: %v", err, err)
	}
}

func TestIssueRefreshToken(t *testing.T) {
	svc := mustTokenService(t)
	now := time.Now().UTC()

	plaintext, hash, expiresAt, err := svc.IssueRefreshToken(now)
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}
	if plaintext == "" {
		t.Fatalf("plaintext must not be empty")
	}
	if hash == "" {
		t.Fatalf("hash must not be empty")
	}

	sum := sha256.Sum256([]byte(plaintext))
	expectedHash := hex.EncodeToString(sum[:])
	if hash != expectedHash {
		t.Errorf("hash mismatch: got %s, want sha256(%s) = %s", hash, plaintext, expectedHash)
	}

	expectedExpiry := now.Add(86400 * time.Second)
	if expiresAt.Unix() != expectedExpiry.Unix() {
		t.Errorf("expiresAt: got %v, want %v", expiresAt.Unix(), expectedExpiry.Unix())
	}
}

func TestIssueRefreshTokenRandomness(t *testing.T) {
	svc := mustTokenService(t)
	now := time.Now().UTC()

	p1, _, _, err := svc.IssueRefreshToken(now)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	p2, _, _, err := svc.IssueRefreshToken(now)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("two refresh tokens must differ, both got %q", p1)
	}
}

func TestIssueRefreshTokenZeroNow(t *testing.T) {
	svc := mustTokenService(t)
	_, _, _, err := svc.IssueRefreshToken(time.Time{})
	if err != nil {
		t.Fatalf("issue with zero time: %v", err)
	}
}

func TestTokenExpiredErrorMessage(t *testing.T) {
	defaultErr := TokenExpiredError{}
	if defaultErr.Error() != "token expired" {
		t.Errorf("default message: got %q, want %q", defaultErr.Error(), "token expired")
	}
	customErr := TokenExpiredError{message: "custom expired"}
	if customErr.Error() != "custom expired" {
		t.Errorf("custom message: got %q, want %q", customErr.Error(), "custom expired")
	}
}

func TestTokenInvalidErrorMessage(t *testing.T) {
	defaultErr := TokenInvalidError{}
	if defaultErr.Error() != "token invalid" {
		t.Errorf("default message: got %q, want %q", defaultErr.Error(), "token invalid")
	}
	customErr := TokenInvalidError{message: "custom invalid"}
	if customErr.Error() != "custom invalid" {
		t.Errorf("custom message: got %q, want %q", customErr.Error(), "custom invalid")
	}
}

func mustTokenService(t *testing.T) *JwtAccessTokenService {
	t.Helper()
	svc, err := NewJwtAccessTokenService("test-secret-key-32bytes!!", 3600, 86400)
	if err != nil {
		t.Fatalf("create token service: %v", err)
	}
	return svc
}
