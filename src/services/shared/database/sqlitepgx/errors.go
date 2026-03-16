//go:build desktop

package sqlitepgx

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
)

// translateError maps database/sql sentinel errors to their pgx equivalents
// so callers using errors.Is(err, pgx.ErrNoRows) work transparently.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return pgx.ErrNoRows
	}
	return err
}
