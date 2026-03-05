package auth

import (
	"strings"
	"testing"
)

func TestNewBcryptPasswordHasher(t *testing.T) {
	cases := []struct {
		name    string
		rounds  int
		wantErr bool
	}{
		{"zero_defaults_to_12", 0, false},
		{"min_valid", 4, false},
		{"mid_valid", 10, false},
		{"max_valid", 31, false},
		{"below_min", 3, true},
		{"above_max", 32, true},
		{"negative", -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewBcryptPasswordHasher(tc.rounds)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h == nil {
				t.Fatalf("hasher must not be nil")
			}
		})
	}
}

func TestNewBcryptPasswordHasherDefaultRounds(t *testing.T) {
	h, err := NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.rounds != defaultBcryptRounds {
		t.Errorf("rounds: got %d, want %d", h.rounds, defaultBcryptRounds)
	}
}

func TestHashPasswordAndVerifyRoundTrip(t *testing.T) {
	h := mustHasher(t)
	password := "correct-horse-battery-staple"

	hashed, err := h.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hashed == "" {
		t.Fatalf("hash must not be empty")
	}
	if hashed == password {
		t.Fatalf("hash must differ from plaintext")
	}

	if !h.VerifyPassword(password, hashed) {
		t.Errorf("VerifyPassword should return true for correct password")
	}
	if h.VerifyPassword("wrong-password", hashed) {
		t.Errorf("VerifyPassword should return false for wrong password")
	}
}

func TestHashPasswordSaltRandomness(t *testing.T) {
	h := mustHasher(t)
	password := "same-password"

	h1, err := h.HashPassword(password)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	h2, err := h.HashPassword(password)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if h1 == h2 {
		t.Fatalf("two hashes of same password must differ (random salt), both got %q", h1)
	}
	if !h.VerifyPassword(password, h1) {
		t.Errorf("first hash should verify")
	}
	if !h.VerifyPassword(password, h2) {
		t.Errorf("second hash should verify")
	}
}

func TestVerifyPasswordEmptyHash(t *testing.T) {
	h := mustHasher(t)

	if h.VerifyPassword("anything", "") {
		t.Errorf("empty hash must return false")
	}
	if h.VerifyPassword("anything", "   ") {
		t.Errorf("whitespace-only hash must return false")
	}
	if h.VerifyPassword("anything", "\t\n") {
		t.Errorf("tab/newline hash must return false")
	}
}

func TestVerifyPasswordInvalidHash(t *testing.T) {
	h := mustHasher(t)

	if h.VerifyPassword("password", "not-a-bcrypt-hash") {
		t.Errorf("garbage hash must return false")
	}
	if h.VerifyPassword("password", "$2a$10$invalid") {
		t.Errorf("truncated bcrypt hash must return false")
	}
}

func TestHashAndVerifyEmptyPassword(t *testing.T) {
	h := mustHasher(t)

	hashed, err := h.HashPassword("")
	if err != nil {
		t.Fatalf("hash empty password: %v", err)
	}
	if !h.VerifyPassword("", hashed) {
		t.Errorf("empty password should verify against its own hash")
	}
	if h.VerifyPassword("non-empty", hashed) {
		t.Errorf("non-empty password should not verify against empty password hash")
	}
}

func TestHashPasswordBcrypt72ByteLimit(t *testing.T) {
	h := mustHasher(t)

	// golang.org/x/crypto/bcrypt rejects passwords > 72 bytes
	long := strings.Repeat("a", 73)
	_, err := h.HashPassword(long)
	if err == nil {
		t.Fatalf("expected error for >72 byte password")
	}

	exact := strings.Repeat("a", 72)
	hashed, err := h.HashPassword(exact)
	if err != nil {
		t.Fatalf("hash 72-byte password: %v", err)
	}
	if !h.VerifyPassword(exact, hashed) {
		t.Errorf("72-byte password should verify")
	}
}

func TestBcryptRoundsInHashOutput(t *testing.T) {
	h4, err := NewBcryptPasswordHasher(4)
	if err != nil {
		t.Fatalf("new hasher rounds=4: %v", err)
	}
	h5, err := NewBcryptPasswordHasher(5)
	if err != nil {
		t.Fatalf("new hasher rounds=5: %v", err)
	}

	password := "test-password"
	hash4, err := h4.HashPassword(password)
	if err != nil {
		t.Fatalf("hash rounds=4: %v", err)
	}
	hash5, err := h5.HashPassword(password)
	if err != nil {
		t.Fatalf("hash rounds=5: %v", err)
	}

	if !strings.Contains(hash4, "$04$") {
		t.Errorf("rounds=4 hash should contain $04$, got %q", hash4)
	}
	if !strings.Contains(hash5, "$05$") {
		t.Errorf("rounds=5 hash should contain $05$, got %q", hash5)
	}

	if !h4.VerifyPassword(password, hash5) {
		t.Errorf("rounds=4 hasher should verify rounds=5 hash")
	}
	if !h5.VerifyPassword(password, hash4) {
		t.Errorf("rounds=5 hasher should verify rounds=4 hash")
	}
}

func mustHasher(t *testing.T) *BcryptPasswordHasher {
	t.Helper()
	h, err := NewBcryptPasswordHasher(4)
	if err != nil {
		t.Fatalf("create hasher: %v", err)
	}
	return h
}
