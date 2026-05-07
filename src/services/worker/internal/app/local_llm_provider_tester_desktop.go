//go:build desktop

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/localproviders"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

const desktopLLMProviderModelTestTimeout = 15 * time.Second

func (e *DesktopEngine) TestLLMProviderModel(ctx context.Context, req desktop.LLMProviderModelTestRequest) error {
	if e == nil {
		return errors.New("desktop engine unavailable")
	}
	selected, err := resolveDesktopLocalProviderTestRoute(ctx, req)
	if err != nil {
		return err
	}
	gateway, err := desktopGatewayFromRoute(selected, e.auxGateway, e.emitDebugEvents, 16384)
	if err != nil {
		return err
	}

	testCtx, cancel := context.WithTimeout(ctx, desktopLLMProviderModelTestTimeout)
	defer cancel()

	maxOutputTokens := 32
	request := llm.Request{
		Model: selected.Route.Model,
		Messages: []llm.Message{
			textMessage("system", "You are a helpful assistant."),
			textMessage("user", "ping"),
		},
		MaxOutputTokens: &maxOutputTokens,
	}
	completed := false
	if err := gateway.Stream(testCtx, request, func(event llm.StreamEvent) error {
		switch value := event.(type) {
		case llm.StreamRunFailed:
			return value.Error
		case llm.StreamRunCompleted:
			completed = true
		}
		return nil
	}); err != nil {
		return err
	}
	if !completed {
		return errors.New("model test did not complete")
	}
	return nil
}

func resolveDesktopLocalProviderTestRoute(ctx context.Context, req desktop.LLMProviderModelTestRequest) (routing.SelectedProviderRoute, error) {
	providerUUID, err := uuid.Parse(strings.TrimSpace(req.ProviderID))
	if err != nil {
		return routing.SelectedProviderRoute{}, fmt.Errorf("invalid provider id: %w", err)
	}
	modelUUID, err := uuid.Parse(strings.TrimSpace(req.ModelID))
	if err != nil {
		return routing.SelectedProviderRoute{}, fmt.Errorf("invalid model id: %w", err)
	}
	providerID, ok := localproviders.ProviderIDFromUUID(providerUUID)
	if !ok {
		return routing.SelectedProviderRoute{}, fmt.Errorf("provider not found")
	}

	resolver := localproviders.NewResolver(localproviders.Options{})
	for _, status := range resolver.ProviderStatuses(ctx) {
		if status.ID != providerID {
			continue
		}
		for _, model := range status.Models {
			if localproviders.RouteUUID(providerID, model.ID) != modelUUID {
				continue
			}
			selected, ok := routing.LocalProviderSelectedRoute(status, model)
			if !ok {
				return routing.SelectedProviderRoute{}, fmt.Errorf("model not found")
			}
			return selected, nil
		}
		return routing.SelectedProviderRoute{}, fmt.Errorf("model not found")
	}
	return routing.SelectedProviderRoute{}, fmt.Errorf("provider not found")
}

func textMessage(role string, text string) llm.Message {
	return llm.Message{
		Role: strings.TrimSpace(role),
		Content: []llm.ContentPart{
			{Type: messagecontent.PartTypeText, Text: text},
		},
	}
}
