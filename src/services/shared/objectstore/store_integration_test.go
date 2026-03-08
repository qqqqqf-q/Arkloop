package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

func TestStorePutObjectHeadAndContentType(t *testing.T) {
	endpoint, accessKey, secretKey, bucket, region := s3EnvOrSkip(t)

	store, err := New(context.Background(), endpoint, accessKey, secretKey, bucket, region)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}

	key := fmt.Sprintf("test/integration/%s", t.Name())
	metadata := map[string]string{"Owner": "arkloop", " Thread ": "demo"}
	payload := []byte("hello metadata")

	if err := store.PutObject(context.Background(), key, payload, PutOptions{ContentType: "text/plain", Metadata: metadata}); err != nil {
		t.Fatalf("put object: %v", err)
	}
	defer func() { _ = store.Delete(context.Background(), key) }()

	head, err := store.Head(context.Background(), key)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.ContentType != "text/plain" {
		t.Fatalf("unexpected content type: %q", head.ContentType)
	}
	if head.Size != int64(len(payload)) {
		t.Fatalf("unexpected size: got %d want %d", head.Size, len(payload))
	}
	if head.Metadata["owner"] != "arkloop" || head.Metadata["thread"] != "demo" {
		t.Fatalf("unexpected metadata: %#v", head.Metadata)
	}
	if strings.TrimSpace(head.ETag) == "" {
		t.Fatal("expected etag")
	}

	data, contentType, err := store.GetWithContentType(context.Background(), key)
	if err != nil {
		t.Fatalf("get with content type: %v", err)
	}
	if contentType != "text/plain" || !bytes.Equal(data, payload) {
		t.Fatalf("unexpected object: contentType=%q data=%q", contentType, data)
	}
}

func TestStoreLifecycleConfiguration(t *testing.T) {
	endpoint, accessKey, secretKey, bucket, region := s3EnvOrSkip(t)

	store, err := New(context.Background(), endpoint, accessKey, secretKey, bucket, region)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}

	configurer, ok := store.(LifecycleConfigurator)
	if !ok {
		t.Fatalf("store does not implement lifecycle configurator: %T", store)
	}
	if err := configurer.SetLifecycleExpirationDays(context.Background(), 3); err != nil {
		t.Fatalf("set lifecycle expiration days: %v", err)
	}

	s3Store, ok := store.(*S3Store)
	if !ok {
		t.Fatalf("unexpected store type: %T", store)
	}
	out, err := s3Store.client.GetBucketLifecycleConfiguration(context.Background(), &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Fatalf("get lifecycle configuration: %v", err)
	}
	if len(out.Rules) != 1 {
		t.Fatalf("expected one lifecycle rule, got %d", len(out.Rules))
	}
	rule := out.Rules[0]
	if rule.Expiration == nil || rule.Expiration.Days == nil || *rule.Expiration.Days != 3 {
		t.Fatalf("unexpected lifecycle rule: %#v", rule)
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
