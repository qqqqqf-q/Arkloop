package redis

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNewClientPing(t *testing.T) {
	redisURL := strings.TrimSpace(os.Getenv("ARKLOOP_REDIS_URL"))
	if redisURL == "" {
		t.Skip("ARKLOOP_REDIS_URL not set")
	}

	client, err := NewClient(context.Background(), redisURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
}

func TestNewClientRejectsEmptyURL(t *testing.T) {
	_, err := NewClient(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty url, got nil")
	}
}

func TestNewClientRejectsInvalidURL(t *testing.T) {
	_, err := NewClient(context.Background(), "not-a-valid-url://??")
	if err == nil {
		t.Fatal("expected error for invalid url, got nil")
	}
}
