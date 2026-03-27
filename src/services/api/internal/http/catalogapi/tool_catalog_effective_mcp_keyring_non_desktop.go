//go:build !desktop

package catalogapi

import (
	"arkloop/services/api/internal/crypto"
	"fmt"
)

type catalogKeyRing interface {
	Decrypt(encoded string, keyVersion int) ([]byte, error)
}

func newEffectiveCatalogKeyRing() (catalogKeyRing, error) {
	return crypto.NewKeyRingFromEnv()
}

func keyVersionFromPointer(ptr *int) (int, error) {
	if ptr == nil {
		return 0, fmt.Errorf("tool_catalog_effective_mcp: missing key version")
	}
	return *ptr, nil
}
