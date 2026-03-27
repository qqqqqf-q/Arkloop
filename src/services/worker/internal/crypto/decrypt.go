//go:build !desktop

package crypto

import (
	"fmt"

	sharedencryption "arkloop/services/shared/encryption"
)

const EncryptionKeyEnv = sharedencryption.EncryptionKeyEnv

func DecryptGCM(encoded string) ([]byte, error) {
	return decryptWithVersion(encoded, 1)
}

func DecryptWithKeyVersion(encoded string, keyVersion int) ([]byte, error) {
	return decryptWithVersion(encoded, keyVersion)
}

func decryptWithVersion(encoded string, keyVersion int) ([]byte, error) {
	keyRing, err := sharedencryption.NewKeyRingFromEnv()
	if err != nil {
		return nil, err
	}
	plaintext, err := keyRing.Decrypt(encoded, keyVersion)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
