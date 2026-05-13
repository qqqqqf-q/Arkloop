package pluginstore

import (
	"context"
	"strings"
	"testing"
)

func TestLocalStoreBlocksZipSlipPath(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	err = store.Write(context.Background(), "demo", "1.0.0", "../escape.txt", []byte("bad"))
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected zip-slip style path rejection, got %v", err)
	}
}

func TestLocalStoreAllowsColonPluginIDAndRemovesVersionRoot(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Write(context.Background(), "com.demo:plugin", "1.0.0", "asset.txt", []byte("ok")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := store.Remove(context.Background(), "com.demo:plugin", "1.0.0"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	exists, err := store.Exists("com.demo:plugin", "1.0.0", "asset.txt")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Fatalf("expected removed asset")
	}
}
