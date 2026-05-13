//go:build desktop

package crypto

import (
	"fmt"

	"arkloop/services/shared/desktop"
)

func DecryptGCM(encoded string) ([]byte, error) {
	return decryptWithVersion(encoded, 1)
}

func DecryptWithKeyVersion(encoded string, keyVersion int) ([]byte, error) {
	return decryptWithVersion(encoded, keyVersion)
}

func EncryptWithCurrentKey(plaintext []byte) (string, int, error) {
	keyRing, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("crypto: load encryption key: %w", err)
	}
	encoded, keyVersion, err := keyRing.Encrypt(plaintext)
	if err != nil {
		return "", 0, fmt.Errorf("crypto: encrypt: %w", err)
	}
	return encoded, keyVersion, nil
}

func decryptWithVersion(encoded string, keyVersion int) ([]byte, error) {
	keyRing, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		return nil, fmt.Errorf("crypto: load encryption key: %w", err)
	}
	plaintext, err := keyRing.Decrypt(encoded, keyVersion)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
