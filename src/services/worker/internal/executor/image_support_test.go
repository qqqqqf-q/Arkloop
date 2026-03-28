package executor

import (
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

func TestApplyImageFilter_KeepImageWhenModelSupportsVision(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeImage, Attachment: &messagecontent.AttachmentRef{Filename: "x.png"}},
			},
		},
	}
	route := &routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"input_modalities": []string{"text", "image"},
				},
			},
		},
	}
	out := applyImageFilter(route, msgs, false)
	if got := out[0].Content[0].Kind(); got != messagecontent.PartTypeImage {
		t.Fatalf("expected image part unchanged, got %s", got)
	}
}

func TestApplyImageFilter_DowngradeToReadHintWhenBridgeEnabled(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeImage, Attachment: &messagecontent.AttachmentRef{Filename: "demo.png"}},
			},
		},
	}
	route := &routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"input_modalities": []string{"text"},
				},
			},
		},
	}
	out := applyImageFilter(route, msgs, true)
	if got := out[0].Content[0].Kind(); got != messagecontent.PartTypeText {
		t.Fatalf("expected text placeholder, got %s", got)
	}
	text := out[0].Content[0].Text
	if !strings.Contains(text, "read 工具") {
		t.Fatalf("expected read hint, got %q", text)
	}
}

func TestApplyImageFilter_DowngradeWithoutReadHintWhenBridgeDisabled(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeImage, Attachment: &messagecontent.AttachmentRef{Filename: "demo.png"}},
			},
		},
	}
	route := &routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"input_modalities": []string{"text"},
				},
			},
		},
	}
	out := applyImageFilter(route, msgs, false)
	if got := out[0].Content[0].Kind(); got != messagecontent.PartTypeText {
		t.Fatalf("expected text placeholder, got %s", got)
	}
	text := out[0].Content[0].Text
	if strings.Contains(text, "read 工具") {
		t.Fatalf("expected no read hint when bridge disabled, got %q", text)
	}
	if !strings.Contains(text, "未配置可用的图片读取能力") {
		t.Fatalf("expected unavailable message, got %q", text)
	}
}
