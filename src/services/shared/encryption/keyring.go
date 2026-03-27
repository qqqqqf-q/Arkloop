package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	EncryptionKeyEnv = "ARKLOOP_ENCRYPTION_KEY"
	nonceSize        = 12
)

var ErrUnknownKeyVersion = errors.New("unknown key version")

type KeyRing struct {
	keys       map[int][32]byte
	currentVer int
}

func NewKeyRing(keys map[int][]byte) (*KeyRing, error) {
	if len(keys) == 0 {
		return nil, errors.New("crypto: key ring must have at least one key")
	}

	ring := &KeyRing{keys: make(map[int][32]byte, len(keys))}
	for ver, raw := range keys {
		if len(raw) != 32 {
			return nil, fmt.Errorf("crypto: key version %d must be exactly 32 bytes, got %d", ver, len(raw))
		}
		var fixed [32]byte
		copy(fixed[:], raw)
		ring.keys[ver] = fixed
		if ver > ring.currentVer {
			ring.currentVer = ver
		}
	}
	return ring, nil
}

func NewKeyRingFromEnv() (*KeyRing, error) {
	raw := strings.TrimSpace(os.Getenv(EncryptionKeyEnv))
	if raw == "" {
		return nil, fmt.Errorf("crypto: %s is not set", EncryptionKeyEnv)
	}

	keyBytes, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("crypto: %s is not valid hex: %w", EncryptionKeyEnv, err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("crypto: %s must be 64 hex chars (32 bytes), got %d bytes", EncryptionKeyEnv, len(keyBytes))
	}

	return NewKeyRing(map[int][]byte{1: keyBytes})
}

func (kr *KeyRing) Encrypt(plaintext []byte) (string, int, error) {
	key, ok := kr.keys[kr.currentVer]
	if !ok {
		return "", 0, fmt.Errorf("crypto: current key version %d not found in ring", kr.currentVer)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", 0, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", 0, fmt.Errorf("crypto: new gcm: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", 0, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), kr.currentVer, nil
}

func (kr *KeyRing) Decrypt(encoded string, keyVersion int) ([]byte, error) {
	key, ok := kr.keys[keyVersion]
	if !ok {
		return nil, fmt.Errorf("crypto: %w: version %d", ErrUnknownKeyVersion, keyVersion)
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: base64 decode: %w", err)
	}
	if len(data) < nonceSize {
		return nil, fmt.Errorf("crypto: ciphertext too short")
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
