//go:build desktop

package crypto

import (
	"fmt"

	"arkloop/services/shared/desktop"
)

func DecryptGCM(encoded string) ([]byte, error) {
	keyRing, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		return nil, fmt.Errorf("crypto: load encryption key: %w", err)
	}
	plaintext, err := keyRing.Decrypt(encoded, 1)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
