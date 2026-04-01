package docker

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

type OperationStatus string

const (
	OperationPending   OperationStatus = "pending"
	OperationRunning   OperationStatus = "running"
	OperationCompleted OperationStatus = "completed"
	OperationFailed    OperationStatus = "failed"
)

type Operation struct {
	ID        string          `json:"id"`
	ModuleID  string          `json:"module_id"`
	Action    string          `json:"action"`
	Status    OperationStatus `json:"status"`
	CreatedAt time.Time       `json:"created_at"`

	mu         sync.Mutex
	lines      []string
	done       chan struct{}
	err        error
	cancelFunc context.CancelFunc
	cancelled  bool
	pid        int
}

func NewOperation(moduleID, action string) *Operation {
	return &Operation{
		ID:        uuid.New().String(),
		ModuleID:  moduleID,
		Action:    action,
		Status:    OperationPending,
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
	}
}

// Cancel requests cancellation of the operation's context and kills the
// process group so that child processes spawned by docker compose are
// also terminated.
func (o *Operation) Cancel() {
	o.mu.Lock()
	o.cancelled = true
	pid := o.pid
	o.mu.Unlock()
	if o.cancelFunc != nil {
		o.cancelFunc()
	}
	if pid > 0 {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
}

// IsCancelled reports whether Cancel has been called.
func (o *Operation) IsCancelled() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cancelled
}

// SetPID records the process ID so Cancel can kill the process group.
func (o *Operation) SetPID(pid int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pid = pid
}

// SetCancelFunc assigns the context cancel function for this operation.
func (o *Operation) SetCancelFunc(cancel context.CancelFunc) {
	o.cancelFunc = cancel
}

// AppendLog adds a log line to the operation.
func (o *Operation) AppendLog(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lines = append(o.lines, line)
}

// Complete marks the operation as finished. A nil error means success.
func (o *Operation) Complete(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.err = err
	if err != nil {
		o.Status = OperationFailed
	} else {
		o.Status = OperationCompleted
	}
	close(o.done)
}

// Lines returns log lines starting from offset.
func (o *Operation) Lines(offset int) []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if offset >= len(o.lines) {
		return nil
	}
	dst := make([]string, len(o.lines)-offset)
	copy(dst, o.lines[offset:])
	return dst
}

// Wait blocks until the operation completes and returns its error.
func (o *Operation) Wait() error {
	<-o.done
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

// Done returns a channel that is closed when the operation finishes.
func (o *Operation) Done() <-chan struct{} {
	return o.done
}

// OperationStore is a thread-safe in-memory store for operations.
type OperationStore struct {
	mu  sync.RWMutex
	ops map[string]*Operation
}

func NewOperationStore() *OperationStore {
	return &OperationStore{
		ops: make(map[string]*Operation),
	}
}

func (s *OperationStore) Add(op *Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops[op.ID] = op
}

func (s *OperationStore) Get(id string) (*Operation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	op, ok := s.ops[id]
	return op, ok
}
