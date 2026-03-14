package config

import "testing"

func TestParseProfileValue(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantProv  string
		wantModel string
		wantErr   bool
	}{
		{"valid anthropic", "anthropic^claude-sonnet-4-5", "anthropic", "claude-sonnet-4-5", false},
		{"valid openai", "openai^gpt-4o", "openai", "gpt-4o", false},
		{"with spaces", " anthropic ^ claude-haiku-3-5 ", "anthropic", "claude-haiku-3-5", false},
		{"empty", "", "", "", true},
		{"no separator", "anthropic-claude", "", "", true},
		{"empty provider", "^model", "", "", true},
		{"empty model", "provider^", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProfileValue(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseProfileValue(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseProfileValue(%q) error = %v", tt.input, err)
				return
			}
			if got.Provider != tt.wantProv {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.wantProv)
			}
			if got.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", got.Model, tt.wantModel)
			}
		})
	}
}
