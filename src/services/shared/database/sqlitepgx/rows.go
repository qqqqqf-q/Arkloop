//go:build desktop

package sqlitepgx

import (
	"database/sql"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// shimRows wraps *sql.Rows to implement pgx.Rows.
type shimRows struct {
	rows        *sql.Rows
	err         error // capture query error for deferred check
	releaseOnce sync.Once
	release     func()
}

func (r *shimRows) Close() {
	if r.rows != nil {
		r.rows.Close()
	}
	r.releaseGuard()
}

func (r *shimRows) Err() error {
	if r.err != nil {
		r.releaseGuard()
		return r.err
	}
	if r.rows != nil {
		err := r.rows.Err()
		if err != nil {
			r.releaseGuard()
		}
		return err
	}
	r.releaseGuard()
	return nil
}

func (r *shimRows) CommandTag() pgconn.CommandTag {
	return pgconn.NewCommandTag("")
}

func (r *shimRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *shimRows) Next() bool {
	if r.err != nil || r.rows == nil {
		r.releaseGuard()
		return false
	}
	ok := r.rows.Next()
	if !ok {
		r.releaseGuard()
	}
	return ok
}

func (r *shimRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return translateError(r.rows.Scan(wrapScanTargets(dest)...))
}

func (r *shimRows) Values() ([]any, error) {
	return nil, nil
}

func (r *shimRows) RawValues() [][]byte {
	return nil
}

func (r *shimRows) Conn() *pgx.Conn {
	return nil
}

func (r *shimRows) releaseGuard() {
	if r == nil {
		return
	}
	r.releaseOnce.Do(func() {
		if r.release != nil {
			r.release()
		}
	})
}
