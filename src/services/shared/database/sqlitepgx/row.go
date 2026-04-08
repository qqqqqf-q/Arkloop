//go:build desktop

package sqlitepgx

import (
	"database/sql"
	"sync"
)

// shimRow wraps *sql.Row to implement pgx.Row.
type shimRow struct {
	row         *sql.Row
	err         error // pre-query error, returned on Scan
	releaseOnce sync.Once
	release     func()
}

func (r *shimRow) Scan(dest ...any) error {
	defer r.releaseGuard()
	if r.err != nil {
		return translateError(r.err)
	}
	return translateError(r.row.Scan(wrapScanTargets(dest)...))
}

func (r *shimRow) releaseGuard() {
	if r == nil {
		return
	}
	r.releaseOnce.Do(func() {
		if r.release != nil {
			r.release()
		}
	})
}
