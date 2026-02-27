package browser

import (
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var (
	NavigateAgentSpec = tools.AgentToolSpec{
		Name:        "browser_navigate",
		Version:     "1",
		Description: "navigate to URL in headless browser, returns screenshot and page content",
		RiskLevel:   tools.RiskLevelMedium,
		SideEffects: true,
	}
	InteractAgentSpec = tools.AgentToolSpec{
		Name:        "browser_interact",
		Version:     "1",
		Description: "interact with page elements (click, type, scroll, select, hover)",
		RiskLevel:   tools.RiskLevelMedium,
		SideEffects: true,
	}
	ExtractAgentSpec = tools.AgentToolSpec{
		Name:        "browser_extract",
		Version:     "1",
		Description: "extract structured content from current page",
		RiskLevel:   tools.RiskLevelLow,
		SideEffects: false,
	}
	ScreenshotAgentSpec = tools.AgentToolSpec{
		Name:        "browser_screenshot",
		Version:     "1",
		Description: "take screenshot of current page",
		RiskLevel:   tools.RiskLevelLow,
		SideEffects: false,
	}
	SessionCloseAgentSpec = tools.AgentToolSpec{
		Name:        "browser_session_close",
		Version:     "1",
		Description: "close browser session and clear all state",
		RiskLevel:   tools.RiskLevelLow,
		SideEffects: true,
	}
)

var (
	NavigateLlmSpec = llm.ToolSpec{
		Name:        "browser_navigate",
		Description: stringPtr("navigate to a URL in headless browser, returns screenshot and page content"),
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":           map[string]any{"type": "string", "format": "uri"},
				"wait_until":    map[string]any{"type": "string", "enum": []string{"load", "domcontentloaded", "networkidle"}},
				"fresh_session": map[string]any{"type": "boolean"},
			},
			"required":             []string{"url"},
			"additionalProperties": false,
		},
	}
	InteractLlmSpec = llm.ToolSpec{
		Name:        "browser_interact",
		Description: stringPtr("interact with page elements: click, type, scroll, select, hover"),
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":   map[string]any{"type": "string", "enum": []string{"click", "type", "scroll", "select", "hover"}},
				"selector": map[string]any{"type": "string"},
				"value":    map[string]any{"type": "string"},
				"coordinates": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"x": map[string]any{"type": "number"},
						"y": map[string]any{"type": "number"},
					},
					"required": []string{"x", "y"},
				},
			},
			"required":             []string{"action"},
			"additionalProperties": false,
		},
	}
	ExtractLlmSpec = llm.ToolSpec{
		Name:        "browser_extract",
		Description: stringPtr("extract structured content from current page"),
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode":     map[string]any{"type": "string", "enum": []string{"text", "accessibility", "html_clean"}},
				"selector": map[string]any{"type": "string"},
			},
			"required":             []string{"mode"},
			"additionalProperties": false,
		},
	}
	ScreenshotLlmSpec = llm.ToolSpec{
		Name:        "browser_screenshot",
		Description: stringPtr("take screenshot of current page"),
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"full_page": map[string]any{"type": "boolean"},
				"selector":  map[string]any{"type": "string"},
				"quality":   map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			},
			"additionalProperties": false,
		},
	}
	SessionCloseLlmSpec = llm.ToolSpec{
		Name:        "browser_session_close",
		Description: stringPtr("close browser session and clear all state"),
		JSONSchema: map[string]any{
			"type":                 "object",
			"properties":          map[string]any{},
			"additionalProperties": false,
		},
	}
)

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		NavigateAgentSpec,
		InteractAgentSpec,
		ExtractAgentSpec,
		ScreenshotAgentSpec,
		SessionCloseAgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		NavigateLlmSpec,
		InteractLlmSpec,
		ExtractLlmSpec,
		ScreenshotLlmSpec,
		SessionCloseLlmSpec,
	}
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
