package mcp

import (
	"context"
	"errors"
	"testing"
)

func TestSDKClientErrorClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "cancelled", err: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySDKError(tt.err)
			var timeoutErr TimeoutError
			if !errors.As(got, &timeoutErr) {
				t.Fatalf("expected TimeoutError, got: %T: %v", got, got)
			}
		})
	}
}
