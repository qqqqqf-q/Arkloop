//go:build desktop

package sqlitepgx

import (
	"database/sql"
)

// shimRow wraps *sql.Row to implement pgx.Row.
type shimRow struct {
	row *sql.Row
	err error // pre-query error, returned on Scan
}

func (r *shimRow) Scan(dest ...any) error {
	if r.err != nil {
		return translateError(r.err)
	}
	return translateError(r.row.Scan(wrapScanTargets(dest)...))
}
