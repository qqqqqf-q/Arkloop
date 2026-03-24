package app

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewApplicationRejectsNilLogger(t *testing.T) {
	_, err := NewApplication(DefaultConfig(), nil)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestApplicationRunStopsOnContextCancel(t *testing.T) {
	logger := slog.Default()
	application, err := NewApplication(DefaultConfig(), logger)
	if err != nil {
		t.Fatalf("NewApplication returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)

	go func() {
		resultCh <- application.Run(ctx)
	}()

	select {
	case <-resultCh:
		t.Fatal("Run should block before cancellation")
	case <-time.After(40 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

func TestNewApplicationRejectsInvalidConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Concurrency = 0

	logger := slog.Default()
	_, err := NewApplication(cfg, logger)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("unexpected error: %v", err)
	}
}
