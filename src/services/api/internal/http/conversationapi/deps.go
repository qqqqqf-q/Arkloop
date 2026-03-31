package conversationapi

import (
	"context"

	"arkloop/services/shared/objectstore"
)

type SSEConfig struct {
	HeartbeatSeconds float64
	BatchLimit       int
}

type artifactStore interface {
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

type messageAttachmentStore interface {
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
	Delete(ctx context.Context, key string) error
}
