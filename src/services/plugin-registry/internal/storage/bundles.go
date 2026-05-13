package storage

import (
	"context"
	"fmt"

	"arkloop/services/shared/objectstore"
)

const BundleContentType = "application/gzip"

type BundleStore interface {
	PutBundle(ctx context.Context, key string, data []byte, sha256Hex string) error
	GetBundle(ctx context.Context, key string) ([]byte, string, error)
}

type ObjectBundleStore struct {
	store objectstore.Store
}

func NewObjectBundleStore(store objectstore.Store) *ObjectBundleStore {
	return &ObjectBundleStore{store: store}
}

func (s *ObjectBundleStore) PutBundle(ctx context.Context, key string, data []byte, sha256Hex string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("bundle store is not configured")
	}
	return s.store.PutObject(ctx, key, data, objectstore.PutOptions{
		ContentType: BundleContentType,
		Metadata: map[string]string{
			"sha256": sha256Hex,
		},
	})
}

func (s *ObjectBundleStore) GetBundle(ctx context.Context, key string) ([]byte, string, error) {
	if s == nil || s.store == nil {
		return nil, "", fmt.Errorf("bundle store is not configured")
	}
	return s.store.GetWithContentType(ctx, key)
}
