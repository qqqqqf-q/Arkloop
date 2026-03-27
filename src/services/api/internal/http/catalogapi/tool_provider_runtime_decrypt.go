//go:build !desktop

package catalogapi

import (
	"context"
	"fmt"

	sharedencryption "arkloop/services/shared/encryption"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

func toolProviderSecretDecrypter() sharedtoolruntime.ProviderSecretDecrypter {
	keyRing, err := sharedencryption.NewKeyRingFromEnv()
	if err != nil {
		return func(_ context.Context, _ string, _ *int, _ string) (*string, error) {
			return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
		}
	}
	return func(_ context.Context, encrypted string, keyVersion *int, providerName string) (*string, error) {
		if keyVersion == nil {
			return nil, fmt.Errorf("tool_provider_configs decrypt: missing key version for %s", providerName)
		}
		plaintext, err := keyRing.Decrypt(encrypted, *keyVersion)
		if err != nil {
			return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
		}
		value := string(plaintext)
		return &value, nil
	}
}
