package pluginstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"arkloop/services/shared/objectstore"
)

type ObjectStore struct {
	blobs  objectstore.BlobStore
	prefix string
}

func NewObjectStore(blobs objectstore.BlobStore, prefix string) (*ObjectStore, error) {
	if blobs == nil {
		return nil, fmt.Errorf("plugin object store blob store must not be nil")
	}
	return &ObjectStore{blobs: blobs, prefix: strings.Trim(strings.TrimSpace(prefix), "/")}, nil
}

func (s *ObjectStore) Read(ctx context.Context, pluginID, version, relPath string) ([]byte, error) {
	key, err := s.key(pluginID, version, relPath)
	if err != nil {
		return nil, err
	}
	return s.blobs.Get(ctx, key)
}

func (s *ObjectStore) Write(ctx context.Context, pluginID, version, relPath string, data []byte) error {
	key, err := s.key(pluginID, version, relPath)
	if err != nil {
		return err
	}
	return s.blobs.Put(ctx, key, data)
}

func (s *ObjectStore) Exists(ctx context.Context, pluginID, version, relPath string) (bool, error) {
	key, err := s.key(pluginID, version, relPath)
	if err != nil {
		return false, err
	}
	if _, err := s.blobs.Head(ctx, key); err != nil {
		if objectstore.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *ObjectStore) Open(ctx context.Context, pluginID, version, relPath string) (io.ReadCloser, error) {
	data, err := s.Read(ctx, pluginID, version, relPath)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *ObjectStore) Path(pluginID, version, relPath string) (string, error) {
	return s.key(pluginID, version, relPath)
}

func (s *ObjectStore) Root(pluginID, version string) (string, error) {
	pluginID, version, err := validateIdentity(pluginID, version)
	if err != nil {
		return "", err
	}
	parts := []string{pluginID, version}
	if s.prefix != "" {
		parts = append([]string{s.prefix}, parts...)
	}
	return path.Join(parts...), nil
}

func (s *ObjectStore) Remove(ctx context.Context, pluginID, version string) error {
	root, err := s.Root(pluginID, version)
	if err != nil {
		return err
	}
	objects, err := s.blobs.ListPrefix(ctx, strings.TrimSuffix(root, "/")+"/")
	if err != nil {
		return err
	}
	for _, object := range objects {
		if err := s.blobs.Delete(ctx, object.Key); err != nil {
			return err
		}
	}
	return nil
}

func (s *ObjectStore) key(pluginID, version, relPath string) (string, error) {
	pluginID, version, err := validateIdentity(pluginID, version)
	if err != nil {
		return "", err
	}
	cleaned, err := objectRelPath(relPath)
	if err != nil {
		return "", err
	}
	parts := []string{pluginID, version, cleaned}
	if s.prefix != "" {
		parts = append([]string{s.prefix}, parts...)
	}
	return path.Join(parts...), nil
}

func objectRelPath(relPath string) (string, error) {
	relPath = strings.TrimSpace(path.Clean(strings.ReplaceAll(relPath, "\\", "/")))
	if relPath == "." || relPath == "" || strings.HasPrefix(relPath, "/") || relPath == ".." || strings.HasPrefix(relPath, "../") {
		return "", fmt.Errorf("plugin object path is invalid")
	}
	return relPath, nil
}
