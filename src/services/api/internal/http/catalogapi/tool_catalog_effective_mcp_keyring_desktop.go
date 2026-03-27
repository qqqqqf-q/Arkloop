//go:build desktop

package catalogapi

import (
	"arkloop/services/shared/desktop"
	"fmt"
)

type catalogKeyRing interface {
	Decrypt(encoded string, keyVersion int) ([]byte, error)
}

func newEffectiveCatalogKeyRing() (catalogKeyRing, error) {
	kr, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		return nil, fmt.Errorf("tool_catalog_effective_mcp: load desktop key ring: %w", err)
	}
	return kr, nil
}

func keyVersionFromPointer(ptr *int) (int, error) {
	if ptr == nil {
		return 0, fmt.Errorf("tool_catalog_effective_mcp: missing key version")
	}
	return *ptr, nil
}
