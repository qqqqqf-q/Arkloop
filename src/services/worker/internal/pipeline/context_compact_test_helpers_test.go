package pipeline

import (
	"context"

	"arkloop/services/worker/internal/llm"
)

type compactSummaryGateway struct {
	requests []llm.Request
	summary  string
}

func (g *compactSummaryGateway) Stream(_ context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	g.requests = append(g.requests, request)
	if err := yield(llm.StreamLlmRequest{
		LlmCallID:    "compact-summary-call",
		ProviderKind: "stub",
		APIMode:      "responses",
		InputJSON: map[string]any{
			"messages": request.Messages,
		},
		PayloadJSON: request.ToJSON(),
	}); err != nil {
		return err
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: g.summary, Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{LlmCallID: "compact-summary-call"})
}
