package http

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/plugin-registry/internal/data"
	"arkloop/services/plugin-registry/internal/storage"
	"arkloop/services/shared/objectstore"
)

const adminToken = "test-admin-token"

func TestAdminUploadSearchAndManifestDownload(t *testing.T) {
	handler := newTestHandler(t)
	manifestBody := []byte(`{
		"schemaVersion": 1,
		"id": "demo.plugin",
		"name": "Demo Plugin",
		"version": "1.2.3",
		"description": "searchable plugin",
		"host_requirement": "desktop_local",
		"platforms": ["darwin", "linux"]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins", bytes.NewReader(manifestBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins?q=searchable&host=desktop_local&platform=darwin", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var search struct {
		Items []data.Plugin `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &search); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(search.Items) != 1 || search.Items[0].ID != "demo.plugin" {
		t.Fatalf("unexpected search items: %#v", search.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/demo.plugin/versions/1.2.3/manifest", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q", got)
	}
	var downloaded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &downloaded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if downloaded["id"] != "demo.plugin" || downloaded["version"] != "1.2.3" {
		t.Fatalf("unexpected manifest: %#v", downloaded)
	}
}

func TestBundleDownloadMissingReturns404(t *testing.T) {
	handler := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins", strings.NewReader(`{
		"schemaVersion": 1,
		"id": "bundleless.plugin",
		"version": "1.0.0"
	}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/bundleless.plugin/versions/1.0.0/bundle", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bundle status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMultipartBundleUploadAndDownload(t *testing.T) {
	handler := newTestHandlerWithBundleStore(t)
	manifestBody := []byte(`{
		"schemaVersion": 1,
		"id": "bundle.plugin",
		"version": "2.0.0"
	}`)
	bundle := tarGzip(t, map[string]string{
		"manifest.yaml": string(manifestBody),
		"bin/server":    "#!/bin/sh\n",
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	manifestPart, err := writer.CreateFormField("manifest")
	if err != nil {
		t.Fatalf("create manifest field: %v", err)
	}
	if _, err := manifestPart.Write(manifestBody); err != nil {
		t.Fatalf("write manifest field: %v", err)
	}
	bundlePart, err := writer.CreateFormFile("bundle", "bundle.tar.gz")
	if err != nil {
		t.Fatalf("create bundle field: %v", err)
	}
	if _, err := bundlePart.Write(bundle); err != nil {
		t.Fatalf("write bundle field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins", &body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/bundle.plugin/versions/2.0.0/bundle", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bundle status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), bundle) {
		t.Fatalf("downloaded bundle mismatch")
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return newTestHandlerWithStore(t, nil)
}

func newTestHandlerWithBundleStore(t *testing.T) http.Handler {
	t.Helper()
	opener := objectstore.NewFilesystemOpener(t.TempDir())
	store, err := opener.Open(context.Background(), "plugin-registry")
	if err != nil {
		t.Fatalf("open bundle store: %v", err)
	}
	return newTestHandlerWithStore(t, storage.NewObjectBundleStore(store))
}

func newTestHandlerWithStore(t *testing.T, bundles storage.BundleStore) http.Handler {
	t.Helper()
	registry, err := NewHandler(data.NewMemoryRepository(), bundles, adminToken)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	mux := http.NewServeMux()
	registry.RegisterRoutes(mux)
	return mux
}

func tarGzip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		data := []byte(content)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return out.Bytes()
}
