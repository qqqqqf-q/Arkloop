package crypto

import (
	"fmt"

	sharedencryption "arkloop/services/shared/encryption"
)

const EncryptionKeyEnv = sharedencryption.EncryptionKeyEnv

func DecryptGCM(encoded string) ([]byte, error) {
	keyRing, err := sharedencryption.NewKeyRingFromEnv()
	if err != nil {
		return nil, err
	}
	plaintext, err := keyRing.Decrypt(encoded, 1)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
