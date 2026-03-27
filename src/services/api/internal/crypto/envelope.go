package crypto

import sharedencryption "arkloop/services/shared/encryption"

const encryptionKeyEnv = sharedencryption.EncryptionKeyEnv

var ErrUnknownKeyVersion = sharedencryption.ErrUnknownKeyVersion

type KeyRing = sharedencryption.KeyRing

func NewKeyRing(keys map[int][]byte) (*KeyRing, error) {
	return sharedencryption.NewKeyRing(keys)
}

func NewKeyRingFromEnv() (*KeyRing, error) {
	return sharedencryption.NewKeyRingFromEnv()
}
