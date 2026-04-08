package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type titleWriteUnitTestDB struct {
	tx       *titleWriteUnitTestTx
	beginErr error
}

func (db titleWriteUnitTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (db titleWriteUnitTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return titleWriteUnitTestRow{err: errors.New("unexpected QueryRow call")}
}

func (db titleWriteUnitTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec call")
}

func (db titleWriteUnitTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if db.beginErr != nil {
		return nil, db.beginErr
	}
	return db.tx, nil
}

type titleWriteUnitTestTx struct {
	dedupeExists bool
	nextSeq      int64
	execErrors   map[string]error
	execCalls    []string
	committed    bool
	rolledBack   bool
}

func (tx *titleWriteUnitTestTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

func (tx *titleWriteUnitTestTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *titleWriteUnitTestTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (tx *titleWriteUnitTestTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}

func (tx *titleWriteUnitTestTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (tx *titleWriteUnitTestTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (tx *titleWriteUnitTestTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}

func (tx *titleWriteUnitTestTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.execCalls = append(tx.execCalls, sql)
	for fragment, err := range tx.execErrors {
		if strings.Contains(sql, fragment) {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *titleWriteUnitTestTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (tx *titleWriteUnitTestTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "SELECT EXISTS"):
		return titleWriteUnitTestRow{values: []any{tx.dedupeExists}}
	case strings.Contains(sql, "COALESCE(MAX(seq), 0) + 1"):
		return titleWriteUnitTestRow{values: []any{tx.nextSeq}}
	default:
		return titleWriteUnitTestRow{err: errors.New("unexpected QueryRow SQL")}
	}
}

func (tx *titleWriteUnitTestTx) Conn() *pgx.Conn {
	return nil
}

type titleWriteUnitTestRow struct {
	values []any
	err    error
}

func (r titleWriteUnitTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, target := range dest {
		switch ptr := target.(type) {
		case *bool:
			value, ok := r.values[i].(bool)
			if !ok {
				return errors.New("unexpected bool scan type")
			}
			*ptr = value
		case *int64:
			value, ok := r.values[i].(int64)
			if !ok {
				return errors.New("unexpected int64 scan type")
			}
			*ptr = value
		default:
			return errors.New("unexpected scan target type")
		}
	}
	return nil
}

func TestWriteThreadTitleAndEventOnce_Success(t *testing.T) {
	tx := &titleWriteUnitTestTx{nextSeq: 7}
	db := titleWriteUnitTestDB{tx: tx}

	seq, emitted, err := writeThreadTitleAndEventOnce(context.Background(), db, uuid.New(), uuid.New(), "new title")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !emitted {
		t.Fatal("expected emitted=true")
	}
	if seq != 7 {
		t.Fatalf("expected seq=7, got %d", seq)
	}
	if !tx.committed {
		t.Fatal("expected transaction to commit")
	}
	if len(tx.execCalls) != 3 {
		t.Fatalf("expected 3 Exec calls, got %d", len(tx.execCalls))
	}
	if !strings.Contains(tx.execCalls[0], "SELECT 1 FROM runs") {
		t.Fatalf("expected first Exec to lock run, got %q", tx.execCalls[0])
	}
	if !strings.Contains(tx.execCalls[1], "UPDATE threads SET title") {
		t.Fatalf("expected second Exec to update title, got %q", tx.execCalls[1])
	}
	if !strings.Contains(tx.execCalls[2], "INSERT INTO run_events") {
		t.Fatalf("expected third Exec to insert event, got %q", tx.execCalls[2])
	}
}

func TestWriteThreadTitleAndEventOnce_SkipWhenEventAlreadyExists(t *testing.T) {
	tx := &titleWriteUnitTestTx{
		dedupeExists: true,
		nextSeq:      9,
	}
	db := titleWriteUnitTestDB{tx: tx}

	seq, emitted, err := writeThreadTitleAndEventOnce(context.Background(), db, uuid.New(), uuid.New(), "new title")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if emitted {
		t.Fatal("expected emitted=false when event already exists")
	}
	if seq != 0 {
		t.Fatalf("expected seq=0, got %d", seq)
	}
	if tx.committed {
		t.Fatal("did not expect commit on dedupe skip")
	}
	if len(tx.execCalls) != 1 || !strings.Contains(tx.execCalls[0], "SELECT 1 FROM runs") {
		t.Fatalf("expected only run lock Exec call, got %#v", tx.execCalls)
	}
}

func TestWriteThreadTitleAndEventOnce_InsertFailureRollsBack(t *testing.T) {
	tx := &titleWriteUnitTestTx{
		nextSeq: 4,
		execErrors: map[string]error{
			"INSERT INTO run_events": errors.New("insert failed"),
		},
	}
	db := titleWriteUnitTestDB{tx: tx}

	seq, emitted, err := writeThreadTitleAndEventOnce(context.Background(), db, uuid.New(), uuid.New(), "new title")
	if err == nil {
		t.Fatal("expected error on insert failure")
	}
	if emitted {
		t.Fatal("expected emitted=false on insert failure")
	}
	if seq != 0 {
		t.Fatalf("expected seq=0 on failure, got %d", seq)
	}
	if tx.committed {
		t.Fatal("did not expect commit on failure")
	}
	if !tx.rolledBack {
		t.Fatal("expected rollback via defer")
	}
}
