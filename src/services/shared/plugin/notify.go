package plugin

import (
	"context"

	"github.com/google/uuid"
)

type NotificationChannel interface {
	Name() string
	Send(ctx context.Context, notification Notification) (deliveryRef string, err error)
}

type Notification struct {
	EventType string
	AccountID     uuid.UUID
	Title     string
	Body      string
	Metadata  map[string]any
}
