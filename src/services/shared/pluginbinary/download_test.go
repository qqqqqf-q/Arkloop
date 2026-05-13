package pluginbinary

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"arkloop/services/shared/pluginstore"
)

func TestExtractTarGzipBlocksZipSlip(t *testing.T) {
	store, err := pluginstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	archive := makeTarGzip(t, "../escape", "bad")
	err = ExtractTarGzip(context.Background(), store, "demo", "1.0.0", archive)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected zip-slip rejection, got %v", err)
	}
}

func TestExtractTarGzipBlocksZipSlipUnderTargetDir(t *testing.T) {
	store, err := pluginstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	archive := makeTarGzip(t, "../escape", "bad")
	err = ExtractTarGzip(context.Background(), store, "demo", "1.0.0", archive, "runtime")
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected target-dir zip-slip rejection, got %v", err)
	}
}

func TestExtractTarGzipBlocksAbsolutePath(t *testing.T) {
	store, err := pluginstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	archive := makeTarGzip(t, "/tmp/escape", "bad")
	err = ExtractTarGzip(context.Background(), store, "demo", "1.0.0", archive)
	if err == nil || !strings.Contains(err.Error(), "relative") {
		t.Fatalf("expected absolute path rejection, got %v", err)
	}
}

func TestDownloadAndExtractMapsBinaryTargetPath(t *testing.T) {
	store, err := pluginstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	archive := makeTarGzip(t, "cua-driver-0.0.15-darwin-arm64/cua-driver", "bin")
	if err := extractTarGzip(context.Background(), store, "demo", "1.0.0", archive, "runtime", "cua-driver"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	data, err := store.Read(context.Background(), "demo", "1.0.0", "runtime/cua-driver")
	if err != nil {
		t.Fatalf("read mapped binary: %v", err)
	}
	if string(data) != "bin" {
		t.Fatalf("unexpected binary content: %q", data)
	}
}

func makeTarGzip(t *testing.T, name, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content))}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tarWriter.Write([]byte(content)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buffer.Bytes()
}
