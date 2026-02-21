package crypto

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
	encryptionKeyEnv = "ARKLOOP_ENCRYPTION_KEY"
	nonceSize        = 12 // AES-GCM 标准 nonce 长度
)

var ErrUnknownKeyVersion = errors.New("unknown key version")

// KeyRing 持有多版本加密 key，支持密钥轮换。
// 加密始终使用 currentVer，解密根据存储的 key_version 选 key。
type KeyRing struct {
	keys       map[int][32]byte
	currentVer int
}

// NewKeyRing 从版本号 -> raw key（32 字节）的 map 构造 KeyRing。
// currentVer 取 map 中最大的版本号。
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

// NewKeyRingFromEnv 从 ARKLOOP_ENCRYPTION_KEY 环境变量（64 位 hex，32 字节）
// 构造 version=1 的 KeyRing。
func NewKeyRingFromEnv() (*KeyRing, error) {
	raw := strings.TrimSpace(os.Getenv(encryptionKeyEnv))
	if raw == "" {
		return nil, fmt.Errorf("crypto: %s is not set", encryptionKeyEnv)
	}

	keyBytes, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("crypto: %s is not valid hex: %w", encryptionKeyEnv, err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("crypto: %s must be 64 hex chars (32 bytes), got %d bytes", encryptionKeyEnv, len(keyBytes))
	}

	return NewKeyRing(map[int][]byte{1: keyBytes})
}

// Encrypt 用 currentVer 的 key 对明文执行 AES-256-GCM 加密。
// 返回 base64(nonce || ciphertext+tag) 和使用的 key version。
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

	// Seal 将 ciphertext+tag 追加到 nonce 后，结果为 nonce || ciphertext || tag
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	encoded := base64.StdEncoding.EncodeToString(sealed)
	return encoded, kr.currentVer, nil
}

// Decrypt 用指定 keyVersion 解密 base64(nonce || ciphertext+tag)。
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

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
