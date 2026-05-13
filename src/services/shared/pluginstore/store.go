package pluginstore

import (
	"context"
	"io"
)

type Store interface {
	Path(pluginID, version, relPath string) (string, error)
	Exists(pluginID, version, relPath string) (bool, error)
	Read(ctx context.Context, pluginID, version, relPath string) ([]byte, error)
	Write(ctx context.Context, pluginID, version, relPath string, data []byte) error
	Open(ctx context.Context, pluginID, version, relPath string) (io.ReadCloser, error)
	Root(pluginID, version string) (string, error)
	Remove(ctx context.Context, pluginID, version string) error
}
