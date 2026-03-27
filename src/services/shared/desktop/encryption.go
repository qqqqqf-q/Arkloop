//go:build desktop

package desktop

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharedencryption "arkloop/services/shared/encryption"
)

type KeyRingOptions struct {
	DataDir           string
	GenerateIfMissing bool
}

func LoadEncryptionKeyRing(opts KeyRingOptions) (*sharedencryption.KeyRing, error) {
	if raw, ok := os.LookupEnv(sharedencryption.EncryptionKeyEnv); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("crypto: %s must not be empty", sharedencryption.EncryptionKeyEnv)
		}
		return sharedencryption.NewKeyRingFromEnv()
	}

	dataDir, err := ResolveDataDir(opts.DataDir)
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dataDir, "encryption.key")
	raw, err := os.ReadFile(keyPath)
	if err == nil {
		decoded, decErr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if decErr != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("invalid encryption.key (expected 64 hex chars)")
		}
		return sharedencryption.NewKeyRing(map[int][]byte{1: decoded})
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read encryption.key: %w", err)
	}
	if !opts.GenerateIfMissing {
		return nil, fmt.Errorf("read encryption.key: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	key := make([]byte, 32)
	if _, err := cryptorand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("write encryption.key: %w", err)
	}
	return sharedencryption.NewKeyRing(map[int][]byte{1: key})
}
