package database

import (
	"errors"
	"fmt"
)

// ErrNoRows is returned when a query expected exactly one row but found none.
// Adapters must translate driver-specific "no rows" errors into this sentinel.
var ErrNoRows = errors.New("database: no rows in result set")

// IsNoRows reports whether err matches ErrNoRows (works with wrapped errors).
func IsNoRows(err error) bool {
	return errors.Is(err, ErrNoRows)
}

// DriverError wraps a database-driver-specific error so that driver types
// (e.g. pgconn.PgError) do not leak across package boundaries.
type DriverError struct {
	Driver string // e.g. "pgx", "sqlite"
	Err    error
}

func (e *DriverError) Error() string {
	return fmt.Sprintf("database driver %s: %v", e.Driver, e.Err)
}

func (e *DriverError) Unwrap() error {
	return e.Err
}

// IsDriverError reports whether err is (or wraps) a DriverError.
func IsDriverError(err error) bool {
	var de *DriverError
	return errors.As(err, &de)
}
