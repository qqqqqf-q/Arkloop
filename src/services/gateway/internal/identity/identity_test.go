package identity

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("test-secret-key-for-unit-tests")

func signJWT(t *testing.T, claims jwt.MapClaims, secret []byte) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func TestExtractAccountID(t *testing.T) {
	validJWT := signJWT(t, jwt.MapClaims{
		"org": "org-123",
		"sub": "user-456",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, testSecret)

	forgedJWT := signJWT(t, jwt.MapClaims{
		"org": "forged-org",
		"sub": "attacker",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, []byte("wrong-secret"))

	expiredJWT := signJWT(t, jwt.MapClaims{
		"org": "org-123",
		"sub": "user-456",
		"exp": time.Now().Add(-time.Hour).Unix(),
	}, testSecret)

	accountJWT := signJWT(t, jwt.MapClaims{
		"account": "account-789",
		"sub":     "user-456",
		"exp":     time.Now().Add(time.Hour).Unix(),
	}, testSecret)

	tests := []struct {
		name       string
		authHeader string
		secret     []byte
		want       string
	}{
		{
			name:       "valid jwt with org claim (fallback)",
			authHeader: "Bearer " + validJWT,
			secret:     testSecret,
			want:       "org-123",
		},
		{
			name:       "valid jwt with account claim",
			authHeader: "Bearer " + accountJWT,
			secret:     testSecret,
			want:       "account-789",
		},
		{
			name:       "forged jwt rejected",
			authHeader: "Bearer " + forgedJWT,
			secret:     testSecret,
			want:       "",
		},
		{
			name:       "expired jwt rejected",
			authHeader: "Bearer " + expiredJWT,
			secret:     testSecret,
			want:       "",
		},
		{
			name:       "valid jwt without secret stays anonymous",
			authHeader: "Bearer " + validJWT,
			secret:     nil,
			want:       "",
		},
		{
			name:       "forged jwt without secret stays anonymous",
			authHeader: "Bearer " + forgedJWT,
			secret:     nil,
			want:       "",
		},
		{
			name:       "empty auth header",
			authHeader: "",
			secret:     testSecret,
			want:       "",
		},
		{
			name:       "no bearer prefix",
			authHeader: "Basic abc123",
			secret:     testSecret,
			want:       "",
		},
		{
			name:       "malformed token",
			authHeader: "Bearer not-a-jwt",
			secret:     testSecret,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAccountID(context.Background(), tt.authHeader, nil, tt.secret)
			if got != tt.want {
				t.Errorf("ExtractAccountID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractInfo(t *testing.T) {
	validJWT := signJWT(t, jwt.MapClaims{
		"org": "org-abc",
		"sub": "user-xyz",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, testSecret)

	forgedJWT := signJWT(t, jwt.MapClaims{
		"org": "forged-org",
		"sub": "attacker",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, []byte("wrong-secret"))

	accountJWT := signJWT(t, jwt.MapClaims{
		"account": "acct-abc",
		"sub":     "user-xyz",
		"exp":     time.Now().Add(time.Hour).Unix(),
	}, testSecret)

	tests := []struct {
		name          string
		authHeader    string
		secret        []byte
		wantType      IdentityType
		wantAccountID string
		wantUserID    string
	}{
		{
			name:          "valid jwt with org claim (fallback)",
			authHeader:    "Bearer " + validJWT,
			secret:        testSecret,
			wantType:      IdentityJWT,
			wantAccountID: "org-abc",
			wantUserID:    "user-xyz",
		},
		{
			name:          "valid jwt with account claim",
			authHeader:    "Bearer " + accountJWT,
			secret:        testSecret,
			wantType:      IdentityJWT,
			wantAccountID: "acct-abc",
			wantUserID:    "user-xyz",
		},
		{
			name:          "forged jwt rejected to anonymous",
			authHeader:    "Bearer " + forgedJWT,
			secret:        testSecret,
			wantType:      IdentityAnonymous,
			wantAccountID: "",
			wantUserID:    "",
		},
		{
			name:          "no secret stays anonymous",
			authHeader:    "Bearer " + validJWT,
			secret:        nil,
			wantType:      IdentityAnonymous,
			wantAccountID: "",
			wantUserID:    "",
		},
		{
			name:          "empty header is anonymous",
			authHeader:    "",
			secret:        testSecret,
			wantType:      IdentityAnonymous,
			wantAccountID: "",
			wantUserID:    "",
		},
		{
			name:          "malformed token is anonymous",
			authHeader:    "Bearer garbage",
			secret:        testSecret,
			wantType:      IdentityAnonymous,
			wantAccountID: "",
			wantUserID:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ExtractInfo(context.Background(), tt.authHeader, nil, tt.secret)
			if info.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", info.Type, tt.wantType)
			}
			if info.AccountID != tt.wantAccountID {
				t.Errorf("AccountID = %q, want %q", info.AccountID, tt.wantAccountID)
			}
			if info.UserID != tt.wantUserID {
				t.Errorf("UserID = %q, want %q", info.UserID, tt.wantUserID)
			}
		})
	}
}
