package docker

import (
	"context"
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

// Cancel requests cancellation of the operation's context.
func (o *Operation) Cancel() {
	if o.cancelFunc != nil {
		o.cancelFunc()
	}
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
