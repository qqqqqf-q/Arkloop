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
	BackendS3         = "s3"
)

type RuntimeConfig struct {
	Backend  string
	RootDir  string
	S3Config S3Config
}

func LoadRuntimeConfigFromEnv() (RuntimeConfig, error) {
	return NormalizeRuntimeConfig(RuntimeConfig{
		Backend: strings.TrimSpace(os.Getenv(StorageBackendEnv)),
		RootDir: strings.TrimSpace(os.Getenv(StorageRootEnv)),
		S3Config: S3Config{
			Endpoint:  strings.TrimSpace(os.Getenv("ARKLOOP_S3_ENDPOINT")),
			AccessKey: strings.TrimSpace(os.Getenv("ARKLOOP_S3_ACCESS_KEY")),
			SecretKey: strings.TrimSpace(os.Getenv("ARKLOOP_S3_SECRET_KEY")),
			Region:    strings.TrimSpace(os.Getenv("ARKLOOP_S3_REGION")),
		},
	})
}

func NormalizeRuntimeConfig(cfg RuntimeConfig) (RuntimeConfig, error) {
	cfg.Backend = strings.ToLower(strings.TrimSpace(cfg.Backend))
	cfg.RootDir = strings.TrimSpace(cfg.RootDir)
	cfg.S3Config = normalizeS3Config(cfg.S3Config)

	if cfg.Backend == "" {
		switch {
		case cfg.RootDir != "":
			cfg.Backend = BackendFilesystem
		case cfg.S3Config.Endpoint != "":
			cfg.Backend = BackendS3
		default:
			return RuntimeConfig{}, nil
		}
	}

	switch cfg.Backend {
	case BackendFilesystem:
		if cfg.RootDir == "" {
			return RuntimeConfig{}, fmt.Errorf("storage root must not be empty for filesystem backend")
		}
	case BackendS3:
		if cfg.S3Config.Endpoint == "" {
			return RuntimeConfig{}, fmt.Errorf("s3 endpoint must not be empty for s3 backend")
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
	switch normalized.Backend {
	case BackendFilesystem:
		return NewFilesystemOpener(normalized.RootDir), nil
	case BackendS3:
		return newS3BucketOpener(normalized.S3Config), nil
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s", normalized.Backend)
	}
}
