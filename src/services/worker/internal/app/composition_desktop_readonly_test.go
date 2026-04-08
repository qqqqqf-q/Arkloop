//go:build desktop

package app

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type writeCountGuard struct{}

func (writeCountGuard) Release() {}

type writeCountExecutor struct {
	calls atomic.Int64
}

func (e *writeCountExecutor) AcquireWrite(context.Context) (sqlitepgx.WriteGuard, error) {
	e.calls.Add(1)
	return writeCountGuard{}, nil
}

func (e *writeCountExecutor) Count() int64 {
	return e.calls.Load()
}

type beginOptionsRecorderDB struct {
	inner data.DesktopDB

	mu   sync.Mutex
	opts []pgx.TxOptions
}

func (d *beginOptionsRecorderDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return d.inner.Exec(ctx, sql, args...)
}

func (d *beginOptionsRecorderDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return d.inner.Query(ctx, sql, args...)
}

func (d *beginOptionsRecorderDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return d.inner.QueryRow(ctx, sql, args...)
}

func (d *beginOptionsRecorderDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	d.mu.Lock()
	d.opts = append(d.opts, txOptions)
	d.mu.Unlock()
	return d.inner.BeginTx(ctx, txOptions)
}

func (d *beginOptionsRecorderDB) beginCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.opts)
}

func (d *beginOptionsRecorderDB) lastBeginOption() (pgx.TxOptions, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.opts) == 0 {
		return pgx.TxOptions{}, false
	}
	return d.opts[len(d.opts)-1], true
}

func openReadonlyGuardTestDB(t *testing.T) (*beginOptionsRecorderDB, *writeCountExecutor) {
	t.Helper()

	sqlitePool, err := sqliteadapter.AutoMigrate(context.Background(), filepath.Join(t.TempDir(), "desktop-readonly.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlitePool.Close()
	})

	writeExec := &writeCountExecutor{}
	db := sqlitepgx.NewWithWriteExecutor(sqlitePool.Unwrap(), writeExec)
	return &beginOptionsRecorderDB{inner: db}, writeExec
}

func TestFetchLatestDesktopInputUsesReadOnlyTx(t *testing.T) {
	ctx := context.Background()
	db, writeExec := openReadonlyGuardTestDB(t)

	accountID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedDesktopRunBindingAccount(t, db, accountID, userID)
	seedDesktopRunBindingThread(t, db, accountID, threadID, nil, &userID)
	seedDesktopRunBindingRun(t, db, accountID, threadID, &userID, runID)

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin input tx: %v", err)
	}
	event := events.NewEmitter("readonly-input").Emit(pipeline.EventTypeInputProvided, map[string]any{
		"content": "input from user",
	}, nil, nil)
	if _, err := (data.DesktopRunEventsRepository{}).AppendRunEvent(ctx, tx, runID, event); err != nil {
		t.Fatalf("append input event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit input event: %v", err)
	}

	baselineWrites := writeExec.Count()
	baselineBegins := db.beginCount()
	content, seq, ok := fetchLatestDesktopInput(ctx, db, runID, 0)
	if !ok {
		t.Fatal("expected input event to be read")
	}
	if content != "input from user" {
		t.Fatalf("unexpected input content: %q", content)
	}
	if seq <= 0 {
		t.Fatalf("expected positive seq, got %d", seq)
	}
	if got := writeExec.Count(); got != baselineWrites {
		t.Fatalf("fetchLatestDesktopInput should not acquire write entry, writes before=%d after=%d", baselineWrites, got)
	}
	if got := db.beginCount(); got != baselineBegins+1 {
		t.Fatalf("expected one extra BeginTx call, before=%d after=%d", baselineBegins, got)
	}
	last, ok := db.lastBeginOption()
	if !ok || last.AccessMode != pgx.ReadOnly {
		t.Fatalf("expected read-only tx for input fetch, got %#v", last)
	}
}

func TestReadDesktopCancelEventUsesReadOnlyTx(t *testing.T) {
	ctx := context.Background()
	db, writeExec := openReadonlyGuardTestDB(t)

	accountID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedDesktopRunBindingAccount(t, db, accountID, userID)
	seedDesktopRunBindingThread(t, db, accountID, threadID, nil, &userID)
	seedDesktopRunBindingRun(t, db, accountID, threadID, &userID, runID)

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin cancel tx: %v", err)
	}
	cancelEvent := events.NewEmitter("readonly-cancel").Emit("run.cancel_requested", map[string]any{"reason": "test"}, nil, nil)
	if _, err := (data.DesktopRunEventsRepository{}).AppendRunEvent(ctx, tx, runID, cancelEvent); err != nil {
		t.Fatalf("append cancel event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit cancel event: %v", err)
	}

	baselineWrites := writeExec.Count()
	baselineBegins := db.beginCount()
	cancelType, err := readDesktopCancelEvent(ctx, db, runID)
	if err != nil {
		t.Fatalf("readDesktopCancelEvent: %v", err)
	}
	if cancelType != "run.cancel_requested" {
		t.Fatalf("unexpected cancel event type: %q", cancelType)
	}
	if got := writeExec.Count(); got != baselineWrites {
		t.Fatalf("readDesktopCancelEvent should not acquire write entry, writes before=%d after=%d", baselineWrites, got)
	}
	if got := db.beginCount(); got != baselineBegins+1 {
		t.Fatalf("expected one extra BeginTx call, before=%d after=%d", baselineBegins, got)
	}
	last, ok := db.lastBeginOption()
	if !ok || last.AccessMode != pgx.ReadOnly {
		t.Fatalf("expected read-only tx for cancel lookup, got %#v", last)
	}
}

func TestLoadDesktopRoutingConfigUsesReadOnlyTx(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	keyBytes := [32]byte{}
	for idx := range keyBytes {
		keyBytes[idx] = byte(idx + 1)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "encryption.key"), []byte(hex.EncodeToString(keyBytes[:])),
		0o600); err != nil {
		t.Fatalf("write encryption key: %v", err)
	}

	db, writeExec := openReadonlyGuardTestDB(t)
	baselineWrites := writeExec.Count()
	baselineBegins := db.beginCount()

	_, err := loadDesktopRoutingConfig(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "no active credentials found in database") {
		t.Fatalf("unexpected routing config error: %v", err)
	}
	if got := writeExec.Count(); got != baselineWrites {
		t.Fatalf("loadDesktopRoutingConfig should not acquire write entry, writes before=%d after=%d", baselineWrites, got)
	}
	if got := db.beginCount(); got != baselineBegins+1 {
		t.Fatalf("expected one extra BeginTx call, before=%d after=%d", baselineBegins, got)
	}
	last, ok := db.lastBeginOption()
	if !ok || last.AccessMode != pgx.ReadOnly {
		t.Fatalf("expected read-only tx for routing config, got %#v", last)
	}
}
