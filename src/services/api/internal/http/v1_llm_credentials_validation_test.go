//go:build !desktop

package http

import "testing"

func TestValidateAdvancedJSONForProvider_NonAnthropicIgnored(t *testing.T) {
	advanced := map[string]any{
		"anthropic_version": "2023-06-01",
		"extra_headers":     map[string]any{"invalid-header": "x"},
	}
	if err := validateAdvancedJSONForProvider("openai", advanced); err != nil {
		t.Fatalf("expected nil error for non-anthropic provider, got %v", err)
	}
}

func TestValidateAnthropicAdvancedJSON_Valid(t *testing.T) {
	advanced := map[string]any{
		"anthropic_version": "2023-06-01",
		"extra_headers": map[string]any{
			"anthropic-beta": "prompt-caching-2024-07-31",
		},
		"metadata": map[string]any{"user_id": "u1"},
	}
	if err := validateAdvancedJSONForProvider("anthropic", advanced); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateAnthropicAdvancedJSON_InvalidVersion(t *testing.T) {
	cases := []map[string]any{
		{"anthropic_version": 123},
		{"anthropic_version": " "},
	}
	for idx, advanced := range cases {
		if err := validateAdvancedJSONForProvider("anthropic", advanced); err == nil {
			t.Fatalf("case %d: expected validation error, got nil", idx)
		}
	}
}

func TestValidateAnthropicAdvancedJSON_InvalidExtraHeaders(t *testing.T) {
	cases := []map[string]any{
		{"extra_headers": "bad"},
		{"extra_headers": map[string]any{"x-custom": "v"}},
		{"extra_headers": map[string]any{"anthropic-beta": 1}},
		{"extra_headers": map[string]any{"anthropic-beta": "   "}},
	}
	for idx, advanced := range cases {
		if err := validateAdvancedJSONForProvider("anthropic", advanced); err == nil {
			t.Fatalf("case %d: expected validation error, got nil", idx)
		}
	}
}
