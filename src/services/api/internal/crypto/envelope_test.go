package crypto

import (
	"errors"
	"strings"
	"testing"
)

func testKeyRing(t *testing.T) *KeyRing {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	ring, err := NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return ring
}

func TestRoundTrip(t *testing.T) {
	ring := testKeyRing(t)

	plaintext := []byte("hello, secrets")
	encoded, ver, err := ring.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ver != 1 {
		t.Fatalf("expected key version 1, got %d", ver)
	}
	if encoded == "" {
		t.Fatal("encoded must not be empty")
	}

	got, err := ring.Decrypt(encoded, ver)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	ring := testKeyRing(t)
	plaintext := []byte("same input")

	enc1, _, err := ring.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}
	enc2, _, err := ring.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}
	// 每次 nonce 不同，密文必须不同
	if enc1 == enc2 {
		t.Fatal("two encryptions of the same plaintext must not produce identical ciphertext")
	}
}

func TestWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i + 1)
		key2[i] = byte(i + 100)
	}

	ring1, _ := NewKeyRing(map[int][]byte{1: key1})
	ring2, _ := NewKeyRing(map[int][]byte{1: key2})

	encoded, ver, err := ring1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = ring2.Decrypt(encoded, ver)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
}

func TestUnknownKeyVersion(t *testing.T) {
	ring := testKeyRing(t)

	encoded, _, err := ring.Encrypt([]byte("data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = ring.Decrypt(encoded, 99)
	if err == nil {
		t.Fatal("expected error for unknown key version")
	}
	if !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("expected ErrUnknownKeyVersion, got: %v", err)
	}
}

func TestMultiVersion(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i + 1)
		key2[i] = byte(i + 50)
	}

	// 初始只有 v1
	ring, err := NewKeyRing(map[int][]byte{1: key1})
	if err != nil {
		t.Fatalf("NewKeyRing v1: %v", err)
	}

	encoded, ver, err := ring.Encrypt([]byte("old secret"))
	if err != nil {
		t.Fatalf("Encrypt v1: %v", err)
	}
	if ver != 1 {
		t.Fatalf("expected ver=1, got %d", ver)
	}

	// 轮换：加入 v2，重建 ring（v2 是 currentVer）
	ring2, err := NewKeyRing(map[int][]byte{1: key1, 2: key2})
	if err != nil {
		t.Fatalf("NewKeyRing v2: %v", err)
	}

	// v1 时期加密的数据，仍可用 v1 解密
	got, err := ring2.Decrypt(encoded, ver)
	if err != nil {
		t.Fatalf("Decrypt v1 after rotation: %v", err)
	}
	if string(got) != "old secret" {
		t.Fatalf("unexpected plaintext: %q", got)
	}

	// 新加密使用 v2
	enc2, ver2, err := ring2.Encrypt([]byte("new secret"))
	if err != nil {
		t.Fatalf("Encrypt v2: %v", err)
	}
	if ver2 != 2 {
		t.Fatalf("expected ver=2, got %d", ver2)
	}
	got2, err := ring2.Decrypt(enc2, ver2)
	if err != nil {
		t.Fatalf("Decrypt v2: %v", err)
	}
	if string(got2) != "new secret" {
		t.Fatalf("unexpected plaintext: %q", got2)
	}
}

func TestEmptyKeyMap(t *testing.T) {
	_, err := NewKeyRing(map[int][]byte{})
	if err == nil {
		t.Fatal("expected error for empty key map")
	}
}

func TestInvalidKeyLength(t *testing.T) {
	_, err := NewKeyRing(map[int][]byte{1: []byte("tooshort")})
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestNewKeyRingFromEnvMissing(t *testing.T) {
	t.Setenv(encryptionKeyEnv, "")
	_, err := NewKeyRingFromEnv()
	if err == nil {
		t.Fatal("expected error when env var is missing")
	}
}

func TestNewKeyRingFromEnvInvalidHex(t *testing.T) {
	t.Setenv(encryptionKeyEnv, "not-hex-string")
	_, err := NewKeyRingFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestNewKeyRingFromEnvWrongLength(t *testing.T) {
	// 只有 16 字节（32 位 hex），不是 32 字节
	t.Setenv(encryptionKeyEnv, strings.Repeat("ab", 16))
	_, err := NewKeyRingFromEnv()
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestNewKeyRingFromEnvValid(t *testing.T) {
	// 32 字节 = 64 位 hex
	t.Setenv(encryptionKeyEnv, strings.Repeat("ab", 32))
	ring, err := NewKeyRingFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ring == nil {
		t.Fatal("ring must not be nil")
	}
}
