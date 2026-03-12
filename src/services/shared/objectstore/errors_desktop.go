//go:build desktop

package objectstore

import (
	"errors"
	"os"
)

// IsNotFound reports whether err represents a "not found" condition.
// In desktop builds only the local filesystem backend is available,
// so only os.ErrNotExist is checked.
func IsNotFound(err error) bool {
	return err != nil && errors.Is(err, os.ErrNotExist)
}
