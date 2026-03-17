//go:build desktop

package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	"arkloop/services/shared/database"

	_ "modernc.org/sqlite" // SQLite driver registration.
)

// Pool wraps *sql.DB to implement database.DB.
type Pool struct {
	db *sql.DB
}

// New creates a database.DB backed by an existing *sql.DB handle.
func New(db *sql.DB) *Pool {
	return &Pool{db: db}
}

// Open opens a SQLite database with sensible defaults for an embedded single-writer workload.
func Open(dataSourceName string) (*Pool, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &Pool{db: db}, nil
}

// Unwrap returns the underlying *sql.DB for code that needs direct access.
func (p *Pool) Unwrap() *sql.DB {
	return p.db
}

func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	r, err := p.db.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return result{r: r}, nil
}

func (p *Pool) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := p.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return &rows{rows: r}, nil
}

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{row: p.db.QueryRowContext(ctx, sql, args...)}
}

func (p *Pool) Begin(ctx context.Context) (database.Tx, error) {
	t, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, translateError(err)
	}
	return &tx{tx: t}, nil
}

func (p *Pool) Close() error {
	return p.db.Close()
}

func (p *Pool) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// tx wraps *sql.Tx to implement database.Tx.
type tx struct {
	tx *sql.Tx
}

func (t *tx) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	r, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return result{r: r}, nil
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := t.tx.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return &rows{rows: r}, nil
}

func (t *tx) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{row: t.tx.QueryRowContext(ctx, sql, args...)}
}

func (t *tx) Commit(_ context.Context) error {
	return translateError(t.tx.Commit())
}

func (t *tx) Rollback(_ context.Context) error {
	return translateError(t.tx.Rollback())
}

// row wraps *sql.Row to implement database.Row with error translation.
type row struct {
	row *sql.Row
}

func (r *row) Scan(dest ...any) error {
	wrapped := wrapTimeTargets(dest)
	return translateError(r.row.Scan(wrapped...))
}

// rows wraps *sql.Rows to implement database.Rows.
type rows struct {
	rows *sql.Rows
}

func (r *rows) Next() bool             { return r.rows.Next() }
func (r *rows) Scan(dest ...any) error { return translateError(r.rows.Scan(wrapTimeTargets(dest)...)) }
func (r *rows) Close()                 { r.rows.Close() }
func (r *rows) Err() error             { return translateError(r.rows.Err()) }

// sqlite 常见时间格式
var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTime(s string) (time.Time, error) {
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("sqliteadapter: cannot parse %q as time", s)
}

// timeScanner 实现 sql.Scanner，将 TEXT/string 自动转换为 time.Time。
type timeScanner struct {
	dest reflect.Value // 指向 *time.Time 或 *(*time.Time) 的 reflect.Value
	ptr  bool          // true = 目标是 **time.Time（即字段类型 *time.Time）
}

func (ts *timeScanner) Scan(src any) error {
	if src == nil {
		if ts.ptr {
			ts.dest.Set(reflect.Zero(ts.dest.Type()))
		} else {
			ts.dest.Set(reflect.ValueOf(time.Time{}))
		}
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		if ts.ptr {
			ts.dest.Set(reflect.ValueOf(&v))
		} else {
			ts.dest.Set(reflect.ValueOf(v))
		}
	case string:
		t, err := parseTime(v)
		if err != nil {
			return err
		}
		if ts.ptr {
			ts.dest.Set(reflect.ValueOf(&t))
		} else {
			ts.dest.Set(reflect.ValueOf(t))
		}
	case []byte:
		t, err := parseTime(string(v))
		if err != nil {
			return err
		}
		if ts.ptr {
			ts.dest.Set(reflect.ValueOf(&t))
		} else {
			ts.dest.Set(reflect.ValueOf(t))
		}
	default:
		return fmt.Errorf("sqliteadapter: cannot scan %T into time.Time", src)
	}
	return nil
}

var (
	timeType    = reflect.TypeOf(time.Time{})
	timePtrType = reflect.TypeOf((*time.Time)(nil))
)

// wrapTimeTargets 遍历 Scan 目标参数，将 *time.Time 和 **time.Time 替换为 timeScanner。
func wrapTimeTargets(dest []any) []any {
	out := make([]any, len(dest))
	for i, d := range dest {
		v := reflect.ValueOf(d)
		if v.Kind() == reflect.Ptr && !v.IsNil() {
			elem := v.Elem()
			switch elem.Type() {
			case timeType:
				out[i] = &timeScanner{dest: elem, ptr: false}
				continue
			case timePtrType:
				out[i] = &timeScanner{dest: elem, ptr: true}
				continue
			}
		}
		out[i] = d
	}
	return out
}

// result wraps sql.Result to implement database.Result.
type result struct {
	r sql.Result
}

func (r result) RowsAffected() int64 {
	n, _ := r.r.RowsAffected()
	return n
}

// translateError converts database/sql errors to database package errors.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %v", database.ErrNoRows, err)
	}
	return err
}
