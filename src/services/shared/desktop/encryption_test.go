//go:build desktop

package desktop

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	sharedencryption "arkloop/services/shared/encryption"
)

func TestLoadEncryptionKeyRingPrefersEnv(t *testing.T) {
	envKey := make([]byte, 32)
	for i := range envKey {
		envKey[i] = byte(i + 1)
	}
	fileKey := make([]byte, 32)
	for i := range fileKey {
		fileKey[i] = byte(i + 33)
	}

	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)
	t.Setenv(sharedencryption.EncryptionKeyEnv, hex.EncodeToString(envKey))
	if err := os.WriteFile(filepath.Join(dataDir, "encryption.key"), []byte(hex.EncodeToString(fileKey)), 0o600); err != nil {
		t.Fatalf("write encryption key: %v", err)
	}

	envRing, err := sharedencryption.NewKeyRing(map[int][]byte{1: envKey})
	if err != nil {
		t.Fatalf("env ring: %v", err)
	}
	encoded, ver, err := envRing.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	loaded, err := LoadEncryptionKeyRing(KeyRingOptions{})
	if err != nil {
		t.Fatalf("load key ring: %v", err)
	}
	plain, err := loaded.Decrypt(encoded, ver)
	if err != nil {
		t.Fatalf("decrypt with loaded ring: %v", err)
	}
	if string(plain) != "hello" {
		t.Fatalf("unexpected plaintext: %q", string(plain))
	}
}

func TestLoadEncryptionKeyRingReadsFileWhenEnvMissing(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 9)
	}
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, "encryption.key"), []byte(hex.EncodeToString(key)), 0o600); err != nil {
		t.Fatalf("write encryption key: %v", err)
	}

	fileRing, err := sharedencryption.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("file ring: %v", err)
	}
	encoded, ver, err := fileRing.Encrypt([]byte("from-file"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	loaded, err := LoadEncryptionKeyRing(KeyRingOptions{})
	if err != nil {
		t.Fatalf("load key ring: %v", err)
	}
	plain, err := loaded.Decrypt(encoded, ver)
	if err != nil {
		t.Fatalf("decrypt with loaded ring: %v", err)
	}
	if string(plain) != "from-file" {
		t.Fatalf("unexpected plaintext: %q", string(plain))
	}
}

func TestLoadEncryptionKeyRingGeneratesFileWhenMissing(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	loaded, err := LoadEncryptionKeyRing(KeyRingOptions{GenerateIfMissing: true})
	if err != nil {
		t.Fatalf("load key ring: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "encryption.key")); err != nil {
		t.Fatalf("expected encryption.key to exist: %v", err)
	}

	encoded, ver, err := loaded.Encrypt([]byte("generated"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	reloaded, err := LoadEncryptionKeyRing(KeyRingOptions{})
	if err != nil {
		t.Fatalf("reload key ring: %v", err)
	}
	plain, err := reloaded.Decrypt(encoded, ver)
	if err != nil {
		t.Fatalf("decrypt with reloaded ring: %v", err)
	}
	if string(plain) != "generated" {
		t.Fatalf("unexpected plaintext: %q", string(plain))
	}
}

func TestLoadEncryptionKeyRingRejectsInvalidFile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, "encryption.key"), []byte("invalid"), 0o600); err != nil {
		t.Fatalf("write encryption key: %v", err)
	}

	if _, err := LoadEncryptionKeyRing(KeyRingOptions{GenerateIfMissing: true}); err == nil {
		t.Fatal("expected invalid encryption.key to fail")
	}
}

func TestLoadEncryptionKeyRingRejectsInvalidEnvWithoutFileFallback(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)
	t.Setenv(sharedencryption.EncryptionKeyEnv, "invalid")
	validFileKey := make([]byte, 32)
	for i := range validFileKey {
		validFileKey[i] = byte(i + 17)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "encryption.key"), []byte(hex.EncodeToString(validFileKey)), 0o600); err != nil {
		t.Fatalf("write encryption key: %v", err)
	}

	if _, err := LoadEncryptionKeyRing(KeyRingOptions{}); err == nil {
		t.Fatal("expected invalid env key to fail")
	}
}
