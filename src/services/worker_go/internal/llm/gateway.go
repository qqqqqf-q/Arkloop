package llm

import "context"

type StreamEvent any

type Gateway interface {
	Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error
}

