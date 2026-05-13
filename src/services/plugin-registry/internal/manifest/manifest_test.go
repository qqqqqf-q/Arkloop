package manifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestExtractManifestFromBundleAcceptsJSONManifest(t *testing.T) {
	payload := `{"schemaVersion":1,"id":"demo.plugin","version":"1.0.0"}`
	data, err := ExtractManifestFromBundle(testBundle(t, "manifest.json", payload))
	if err != nil {
		t.Fatalf("extract manifest: %v", err)
	}
	if string(data) != payload {
		t.Fatalf("unexpected manifest: %s", data)
	}
}

func testBundle(t *testing.T, name, content string) []byte {
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
