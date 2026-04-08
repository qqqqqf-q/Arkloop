//go:build desktop

package desktop

import (
	"context"
	"testing"
	"time"

	"arkloop/services/shared/database/sqlitepgx"
)

func TestSharedSQLitePoolSetGetClear(t *testing.T) {
	t.Cleanup(func() {
		ClearSharedSQLitePool()
	})
	ClearSharedSQLitePool()
	if GetSharedSQLitePool() != nil {
		t.Fatal("expected nil pool")
	}

	db, err := sqlitepgx.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	p := sqlitepgx.New(db.Unwrap())
	SetSharedSQLitePool(p)
	if GetSharedSQLitePool() != p {
		t.Fatal("GetSharedSQLitePool mismatch")
	}
	ClearSharedSQLitePool()
	if GetSharedSQLitePool() != nil {
		t.Fatal("expected nil after clear")
	}
}

func TestSQLiteCloserRegisterClose(t *testing.T) {
	t.Cleanup(func() {
		_ = CloseRegisteredSQLite()
		SetSidecarProcess(false)
	})

	var calls int
	RegisterSQLiteCloser(func() error {
		calls++
		return nil
	})
	if err := CloseRegisteredSQLite(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("close calls=%d", calls)
	}
	if err := CloseRegisteredSQLite(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected second close noop, calls=%d", calls)
	}
}

func TestSharedSQLiteWriteExecutorSetGet(t *testing.T) {
	origin := GetSharedSQLiteWriteExecutor()
	t.Cleanup(func() {
		SetSharedSQLiteWriteExecutor(origin)
	})

	executor := sqlitepgx.NewSerialWriteExecutor()
	SetSharedSQLiteWriteExecutor(executor)
	if GetSharedSQLiteWriteExecutor() != executor {
		t.Fatal("GetSharedSQLiteWriteExecutor mismatch")
	}

	guard, err := GetSharedSQLiteWriteExecutor().AcquireWrite(context.Background())
	if err != nil {
		t.Fatalf("acquire write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := GetSharedSQLiteWriteExecutor().AcquireWrite(ctx); err == nil {
		t.Fatal("expected blocked acquire to timeout")
	}
	guard.Release()
}
