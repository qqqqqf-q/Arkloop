package llm

import (
	"strings"
	"testing"
)

func TestSanitizeDebugPayloadJSON_RedactsDataURLs(t *testing.T) {
	payload := map[string]any{
		"input": []map[string]any{{
			"content": []map[string]any{{
				"type": "input_image",
				"image_url": map[string]any{
					"url": "data:image/png;base64," + strings.Repeat("A", 120),
				},
			}},
		}},
	}

	sanitized, hints := sanitizeDebugPayloadJSON(payload)
	input := sanitized["input"].([]map[string]any)
	content := input[0]["content"].([]map[string]any)
	imageURL := content[0]["image_url"].(map[string]any)
	got := imageURL["url"].(string)
	if got == payload["input"].([]map[string]any)[0]["content"].([]map[string]any)[0]["image_url"].(map[string]any)["url"] {
		t.Fatal("expected data url to be redacted")
	}
	if hints["data_url_redactions"] != 1 {
		t.Fatalf("unexpected redaction hints: %#v", hints)
	}
}

func TestSanitizeDebugPayloadJSON_RedactsAnthropicBase64Source(t *testing.T) {
	payload := map[string]any{
		"messages": []map[string]any{{
			"content": []map[string]any{{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/png",
					"data":       strings.Repeat("QkFC", 40),
				},
			}},
		}},
	}

	sanitized, hints := sanitizeDebugPayloadJSON(payload)
	messages := sanitized["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	source := content[0]["source"].(map[string]any)
	if got := source["data"].(string); got != "[base64 redacted ~160 chars]" {
		t.Fatalf("unexpected redacted data: %q", got)
	}
	if hints["base64_field_redactions"] != 1 {
		t.Fatalf("unexpected redaction hints: %#v", hints)
	}
}
