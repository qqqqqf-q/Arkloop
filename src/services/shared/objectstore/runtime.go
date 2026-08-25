package objectstore

import (
	"fmt"
	"os"
	"strings"
)

const (
	StorageBackendEnv = "ARKLOOP_STORAGE_BACKEND"
	StorageRootEnv    = "ARKLOOP_STORAGE_ROOT"

	BackendFilesystem = "filesystem"
)

type RuntimeConfig struct {
	Backend string
	RootDir string
}

func LoadRuntimeConfigFromEnv() (RuntimeConfig, error) {
	return NormalizeRuntimeConfig(RuntimeConfig{
		Backend: strings.TrimSpace(os.Getenv(StorageBackendEnv)),
		RootDir: strings.TrimSpace(os.Getenv(StorageRootEnv)),
	})
}

func NormalizeRuntimeConfig(cfg RuntimeConfig) (RuntimeConfig, error) {
	cfg.Backend = strings.ToLower(strings.TrimSpace(cfg.Backend))
	cfg.RootDir = strings.TrimSpace(cfg.RootDir)

	if cfg.Backend == "" {
		if cfg.RootDir == "" {
			return RuntimeConfig{}, nil
		}
		cfg.Backend = BackendFilesystem
	}

	switch cfg.Backend {
	case BackendFilesystem:
		if cfg.RootDir == "" {
			return RuntimeConfig{}, fmt.Errorf("storage root must not be empty for filesystem backend")
		}
	default:
		return RuntimeConfig{}, fmt.Errorf("unsupported storage backend: %s", cfg.Backend)
	}

	return cfg, nil
}

func (c RuntimeConfig) Enabled() bool {
	return strings.TrimSpace(c.Backend) != ""
}

func (c RuntimeConfig) BucketOpener() (BucketOpener, error) {
	normalized, err := NormalizeRuntimeConfig(c)
	if err != nil {
		return nil, err
	}
	if !normalized.Enabled() {
		return nil, nil
	}
	if normalized.Backend != BackendFilesystem {
		return nil, fmt.Errorf("unsupported storage backend: %s", normalized.Backend)
	}
	return NewFilesystemOpener(normalized.RootDir), nil
}
