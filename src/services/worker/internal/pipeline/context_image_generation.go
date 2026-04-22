package pipeline

import (
	"context"
	"fmt"

	"arkloop/services/worker/internal/llm"
)

// ResolveImageGenerateGatewayConfig returns the route config used by image_generate.
// It uses the current selected route for the active run.
func (rc *RunContext) ResolveImageGenerateGatewayConfig(ctx context.Context) (llm.ResolvedGatewayConfig, string, string, error) {
	if rc == nil {
		return llm.ResolvedGatewayConfig{}, "", "", fmt.Errorf("run context unavailable")
	}
	selected := rc.SelectedRoute
	if selected == nil {
		return llm.ResolvedGatewayConfig{}, "", "", fmt.Errorf("image generation route not initialized")
	}

	cfg, err := ResolveGatewayConfigFromSelectedRoute(*selected, false, rc.LlmMaxResponseBytes)
	if err != nil {
		return llm.ResolvedGatewayConfig{}, "", "", err
	}
	return cfg, string(selected.Credential.ProviderKind), selected.Route.Model, nil
}
