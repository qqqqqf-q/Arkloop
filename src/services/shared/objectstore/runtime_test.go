package objectstore

import "testing"

func TestNormalizeRuntimeConfigInfersFilesystemFromRootDir(t *testing.T) {
	cfg, err := NormalizeRuntimeConfig(RuntimeConfig{
		RootDir: "/tmp/arkloop-storage",
	})
	if err != nil {
		t.Fatalf("normalize runtime config: %v", err)
	}
	if cfg.Backend != BackendFilesystem {
		t.Fatalf("unexpected backend: %s", cfg.Backend)
	}
}

func TestNormalizeRuntimeConfigHonorsExplicitBackend(t *testing.T) {
	cfg, err := NormalizeRuntimeConfig(RuntimeConfig{
		Backend: BackendFilesystem,
		RootDir: "/tmp/arkloop-storage",
	})
	if err != nil {
		t.Fatalf("normalize runtime config: %v", err)
	}
	if cfg.Backend != BackendFilesystem {
		t.Fatalf("unexpected backend: %s", cfg.Backend)
	}
}

func TestNormalizeRuntimeConfigRejectsUnsupportedBackend(t *testing.T) {
	if _, err := NormalizeRuntimeConfig(RuntimeConfig{Backend: "s3"}); err == nil {
		t.Fatalf("expected error for unsupported backend")
	}
}
