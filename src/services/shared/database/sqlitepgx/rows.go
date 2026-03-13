//go:build desktop

package sqlitepgx

import (
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// shimRows wraps *sql.Rows to implement pgx.Rows.
type shimRows struct {
	rows *sql.Rows
	err  error // capture query error for deferred check
}

func (r *shimRows) Close() {
	if r.rows != nil {
		r.rows.Close()
	}
}

func (r *shimRows) Err() error {
	if r.err != nil {
		return r.err
	}
	if r.rows != nil {
		return r.rows.Err()
	}
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
		return false
	}
	return r.rows.Next()
}

func (r *shimRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return translateError(r.rows.Scan(dest...))
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
