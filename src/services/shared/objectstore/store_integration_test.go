package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func s3EnvOrSkip(t *testing.T) (endpoint, accessKey, secretKey, bucket, region string) {
	t.Helper()
	endpoint = strings.TrimSpace(os.Getenv("ARKLOOP_S3_ENDPOINT"))
	accessKey = strings.TrimSpace(os.Getenv("ARKLOOP_S3_ACCESS_KEY"))
	secretKey = strings.TrimSpace(os.Getenv("ARKLOOP_S3_SECRET_KEY"))
	bucket = strings.TrimSpace(os.Getenv("ARKLOOP_S3_BUCKET"))
	region = strings.TrimSpace(os.Getenv("ARKLOOP_S3_REGION"))

	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("ARKLOOP_S3_ENDPOINT / ARKLOOP_S3_ACCESS_KEY / ARKLOOP_S3_SECRET_KEY not set")
	}
	if bucket == "" {
		bucket = "arkloop-test"
	}
	return
}

func TestStoreConnectivity(t *testing.T) {
	endpoint, accessKey, secretKey, bucket, region := s3EnvOrSkip(t)

	store, err := New(context.Background(), endpoint, accessKey, secretKey, bucket, region)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	_ = store
}

func TestStorePutGetDelete(t *testing.T) {
	endpoint, accessKey, secretKey, bucket, region := s3EnvOrSkip(t)

	store, err := New(context.Background(), endpoint, accessKey, secretKey, bucket, region)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}

	key := fmt.Sprintf("test/integration/%s", t.Name())
	payload := []byte("hello arkloop objectstore")

	if err := store.Put(context.Background(), key, payload); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch: got %q, want %q", got, payload)
	}

	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// 删除后 Get 应报错
	_, err = store.Get(context.Background(), key)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestNewRejectsEmptyEndpoint(t *testing.T) {
	_, err := New(context.Background(), "", "key", "secret", "bucket", "")
	if err == nil {
		t.Fatal("expected error for empty endpoint, got nil")
	}
}

func TestNewRejectsEmptyAccessKey(t *testing.T) {
	_, err := New(context.Background(), "http://localhost:9000", "", "secret", "bucket", "")
	if err == nil {
		t.Fatal("expected error for empty access key, got nil")
	}
}

func TestNewRejectsEmptySecretKey(t *testing.T) {
	_, err := New(context.Background(), "http://localhost:9000", "key", "", "bucket", "")
	if err == nil {
		t.Fatal("expected error for empty secret key, got nil")
	}
}

func TestNewRejectsEmptyBucket(t *testing.T) {
	_, err := New(context.Background(), "http://localhost:9000", "key", "secret", "", "")
	if err == nil {
		t.Fatal("expected error for empty bucket, got nil")
	}
}
