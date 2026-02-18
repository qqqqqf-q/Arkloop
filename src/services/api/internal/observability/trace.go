package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const TraceIDHeader = "X-Trace-Id"

type traceIDKey struct{}

func NewTraceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf)
}

func NormalizeTraceID(value string) string {
	candidate := strings.TrimSpace(value)
	if len(candidate) != 32 {
		return ""
	}

	var lowered strings.Builder
	lowered.Grow(32)
	for i := 0; i < len(candidate); i++ {
		ch := candidate[i]
		switch {
		case ch >= '0' && ch <= '9':
			lowered.WriteByte(ch)
		case ch >= 'a' && ch <= 'f':
			lowered.WriteByte(ch)
		case ch >= 'A' && ch <= 'F':
			lowered.WriteByte(ch + ('a' - 'A'))
		default:
			return ""
		}
	}
	return lowered.String()
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, ok := ctx.Value(traceIDKey{}).(string)
	if !ok {
		return ""
	}
	return value
}
