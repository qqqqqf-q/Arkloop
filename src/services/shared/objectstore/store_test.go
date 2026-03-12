//go:build !desktop

package objectstore

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestNormalizeS3ConfigDefaultsRegion(t *testing.T) {
	cfg := normalizeS3Config(S3Config{Endpoint: " http://localhost:9000 ", AccessKey: " key ", SecretKey: " secret "})
	if cfg.Region != "us-east-1" {
		t.Fatalf("unexpected region: %q", cfg.Region)
	}
	if cfg.Endpoint != "http://localhost:9000" || cfg.AccessKey != "key" || cfg.SecretKey != "secret" {
		t.Fatalf("unexpected normalized config: %#v", cfg)
	}
}

func TestNewS3OpenerRejectsEmptyBucket(t *testing.T) {
	opener := NewS3Opener(S3Config{Endpoint: "http://localhost:9000", AccessKey: "key", SecretKey: "secret"})
	_, err := opener.Open(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

func TestIsNotFoundRecognizesOsErrNotExist(t *testing.T) {
	if !IsNotFound(os.ErrNotExist) {
		t.Fatal("expected os.ErrNotExist to be recognized")
	}
	if IsNotFound(errors.New("boom")) {
		t.Fatal("unexpected not found match")
	}
}

func TestExpirationLifecycleConfiguration(t *testing.T) {
	config := expirationLifecycleConfiguration(7)
	if config == nil {
		t.Fatal("expected lifecycle configuration")
	}
	if len(config.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(config.Rules))
	}
	rule := config.Rules[0]
	if rule.Status != types.ExpirationStatusEnabled {
		t.Fatalf("unexpected rule status: %s", rule.Status)
	}
	if rule.Expiration == nil || rule.Expiration.Days == nil || *rule.Expiration.Days != 7 {
		t.Fatalf("unexpected expiration days: %#v", rule.Expiration)
	}
	if rule.Filter == nil || rule.Filter.Prefix == nil {
		t.Fatalf("unexpected lifecycle filter: %#v", rule.Filter)
	}
	if *rule.Filter.Prefix != "" {
		t.Fatalf("unexpected filter prefix: %q", *rule.Filter.Prefix)
	}
}
