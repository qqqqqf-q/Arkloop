package docker

import (
	"errors"
	"testing"
	"time"
)

func TestNewOperation(t *testing.T) {
	op := NewOperation("redis", "install")

	if op.ID == "" {
		t.Error("expected non-empty ID")
	}
	if op.Status != OperationPending {
		t.Errorf("Status = %q, want %q", op.Status, OperationPending)
	}
	if op.ModuleID != "redis" {
		t.Errorf("ModuleID = %q, want %q", op.ModuleID, "redis")
	}
	if op.Action != "install" {
		t.Errorf("Action = %q, want %q", op.Action, "install")
	}
}

func TestOperationAppendAndLines(t *testing.T) {
	op := NewOperation("redis", "install")
	op.AppendLog("line1")
	op.AppendLog("line2")
	op.AppendLog("line3")

	all := op.Lines(0)
	if len(all) != 3 {
		t.Fatalf("Lines(0) returned %d lines, want 3", len(all))
	}
	if all[0] != "line1" || all[1] != "line2" || all[2] != "line3" {
		t.Errorf("Lines(0) = %v", all)
	}

	fromTwo := op.Lines(2)
	if len(fromTwo) != 1 {
		t.Fatalf("Lines(2) returned %d lines, want 1", len(fromTwo))
	}
	if fromTwo[0] != "line3" {
		t.Errorf("Lines(2)[0] = %q, want %q", fromTwo[0], "line3")
	}

	past := op.Lines(10)
	if past != nil {
		t.Errorf("Lines(10) = %v, want nil", past)
	}
}

func TestOperationComplete(t *testing.T) {
	op := NewOperation("redis", "start")
	op.Complete(nil)

	if op.Status != OperationCompleted {
		t.Errorf("Status = %q, want %q", op.Status, OperationCompleted)
	}
	if err := op.Wait(); err != nil {
		t.Errorf("Wait() = %v, want nil", err)
	}
}

func TestOperationCompleteFailed(t *testing.T) {
	op := NewOperation("redis", "start")
	op.Complete(errors.New("exit code 1"))

	if op.Status != OperationFailed {
		t.Errorf("Status = %q, want %q", op.Status, OperationFailed)
	}
	if err := op.Wait(); err == nil {
		t.Error("Wait() = nil, want error")
	}
}

func TestOperationDone(t *testing.T) {
	op := NewOperation("redis", "stop")

	go func() {
		time.Sleep(10 * time.Millisecond)
		op.Complete(nil)
	}()

	select {
	case <-op.Done():
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Done() channel did not close within timeout")
	}
}

func TestOperationStoreAddAndGet(t *testing.T) {
	store := NewOperationStore()
	op := NewOperation("redis", "install")

	store.Add(op)

	got, ok := store.Get(op.ID)
	if !ok {
		t.Fatal("Get returned false for known operation")
	}
	if got.ID != op.ID {
		t.Errorf("got ID = %q, want %q", got.ID, op.ID)
	}

	_, ok = store.Get("unknown-id")
	if ok {
		t.Error("Get returned true for unknown operation")
	}
}
