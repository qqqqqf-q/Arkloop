//go:build !desktop

package main

import (
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/sandbox/internal/app"
	"arkloop/services/shared/objectstore"
)

func TestInitDepsWithFilesystemBackend(t *testing.T) {
	templatesPath := filepath.Join(t.TempDir(), "templates.json")
	if err := os.WriteFile(templatesPath, []byte(`[
		{
			"id":"python3.12-lite",
			"kernel_image_path":"/opt/sandbox/vmlinux",
			"rootfs_path":"/opt/sandbox/rootfs.ext4",
			"tier":"lite",
			"languages":["python"]
		},
		{
			"id":"chromium-browser",
			"kernel_image_path":"/opt/sandbox/vmlinux",
			"rootfs_path":"/opt/sandbox/chromium.ext4",
			"tier":"browser",
			"languages":["shell"]
		}
	]`), 0o600); err != nil {
		t.Fatalf("write templates file: %v", err)
	}

	cfg := app.DefaultConfig()
	cfg.StorageBackend = objectstore.BackendFilesystem
	cfg.StorageRoot = t.TempDir()
	cfg.SocketBaseDir = t.TempDir()
	cfg.TemplatesPath = templatesPath

	store, registry, _, err := initDeps(cfg)
	if err != nil {
		t.Fatalf("init deps: %v", err)
	}
	if store == nil {
		t.Fatal("expected snapshot store")
	}
	if registry == nil {
		t.Fatal("expected template registry")
	}
	if _, ok := registry.Get("chromium-browser"); !ok {
		t.Fatal("expected chromium-browser template")
	}
}
