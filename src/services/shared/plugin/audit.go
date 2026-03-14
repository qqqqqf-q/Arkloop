package plugin

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AuditSink interface {
	Name() string
	Emit(ctx context.Context, event AuditEvent) error
}

type AuditEvent struct {
	Timestamp  time.Time
	ActorID    uuid.UUID
	AccountID      uuid.UUID
	Action     string
	Resource   string
	ResourceID string
	Detail     map[string]any
	IP         string
	UserAgent  string
}
