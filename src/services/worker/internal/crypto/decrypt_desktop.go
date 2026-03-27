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
