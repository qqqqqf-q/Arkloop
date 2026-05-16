package accountapi

import (
	"strings"
	"testing"

	"arkloop/services/shared/telegrambot"
)

func TestGroupCandidatesByProvider_selection(t *testing.T) {
	candidates := []telegramSelectorCandidate{
		{credentialName: "OpenAI", model: "gpt-4o", accountScoped: true},
		{credentialName: "OpenAI", model: "gpt-4o-mini", accountScoped: true},
		{credentialName: "Anthropic", model: "claude-sonnet-4-5", accountScoped: true},
	}

	data := GroupCandidatesByProvider(candidates, "openai^gpt-4o", true)

	if len(data.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(data.Providers))
	}
	if data.Providers[0].Name != "OpenAI" {
		t.Errorf("first provider: got %q", data.Providers[0].Name)
	}
	if len(data.Providers[0].Models) != 2 {
		t.Errorf("OpenAI models: got %d", len(data.Providers[0].Models))
	}
	if !data.Providers[0].Models[0].IsSelected {
		t.Error("gpt-4o should be selected")
	}
	if data.Providers[0].Models[1].IsSelected {
		t.Error("gpt-4o-mini should not be selected")
	}
}

func TestGroupCandidatesByProvider_bareModelMatch(t *testing.T) {
	candidates := []telegramSelectorCandidate{
		{credentialName: "cmd-cred", model: "gpt-command", accountScoped: true},
		{credentialName: "openrouter-main", model: "anthropic/claude-sonnet-4-5", accountScoped: true},
	}

	cases := []struct {
		name     string
		stored   string
		wantIdx  int
		selected bool
	}{
		{"canonical match", "cmd-cred^gpt-command", 0, true},
		{"bare model match", "gpt-command", 0, true},
		{"canonical with slash", "openrouter-main^anthropic/claude-sonnet-4-5", 1, true},
		{"bare model with slash", "anthropic/claude-sonnet-4-5", 1, true},
		{"no match", "nonexistent", -1, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := GroupCandidatesByProvider(candidates, tc.stored, true)
			var allModels []ModelChoice
			for _, pg := range data.Providers {
				allModels = append(allModels, pg.Models...)
			}
			for i, m := range allModels {
				if i == tc.wantIdx && m.IsSelected != tc.selected {
					t.Errorf("model %d (%s): IsSelected = %v, want %v", i, m.Selector, m.IsSelected, tc.selected)
				}
				if i != tc.wantIdx && m.IsSelected {
					t.Errorf("model %d (%s): unexpected IsSelected = true", i, m.Selector)
				}
			}
		})
	}
}

func TestRenderModelPickerText(t *testing.T) {
	data := ModelPickerData{
		CurrentDisplay: "openai^gpt-4o",
		Providers: []ProviderGroup{
			{Name: "OpenAI", Models: []ModelChoice{
				{Selector: "openai^gpt-4o", DisplayName: "gpt-4o", IsSelected: true},
				{Selector: "openai^gpt-4o-mini", DisplayName: "gpt-4o-mini", IsSelected: false},
			}},
		},
	}
	text := RenderModelPickerText(data)
	if !strings.Contains(text, "[OpenAI]") {
		t.Error("missing provider group header")
	}
	if !strings.Contains(text, "gpt-4o ✓") {
		t.Error("missing selected model marker")
	}
	if !strings.Contains(text, "gpt-4o-mini") {
		t.Error("missing unselected model")
	}
	if !strings.Contains(text, "切换: /model") {
		t.Error("missing usage hint")
	}
}

func TestParseCallbackData(t *testing.T) {
	cases := []struct {
		input  string
		kind   string
		valid  bool
	}{
		{"dismiss", "dismiss", true},
		{"mp", "providers", true},
		{"mp_0", "provider_models", true},
		{"mp_1_2", "provider_models", true},
		{"ms_5", "model_select", true},
		{"thk_medium", "think", true},
		{"prs_3", "persona", true},
		{"invalid", "", false},
		{"thk_invalid", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			parsed, ok := ParseCallbackData(tc.input)
			if ok != tc.valid {
				t.Errorf("ParseCallbackData(%q): ok = %v, want %v", tc.input, ok, tc.valid)
			}
			if ok && parsed.Kind != tc.kind {
				t.Errorf("ParseCallbackData(%q): kind = %q, want %q", tc.input, parsed.Kind, tc.kind)
			}
		})
	}
}

func TestCallbackDataLength(t *testing.T) {
	// Telegram callback_data 硬限制 64 bytes
	cases := []string{
		"dismiss",
		"mp",
		"mp_0",
		"mp_99",
		"mp_0_0",
		"mp_99_99",
		"ms_0",
		"ms_999",
		"thk_minimal",
		"prs_0",
		"prs_99",
	}
	for _, cb := range cases {
		if len(cb) > 64 {
			t.Errorf("callback_data %q exceeds 64 bytes: %d", cb, len(cb))
		}
	}
}

func TestBuildTelegramProvidersKeyboard(t *testing.T) {
	data := ModelPickerData{
		Providers: []ProviderGroup{
			{Name: "OpenAI", Models: []ModelChoice{{IsSelected: true}}},
			{Name: "Anthropic", Models: []ModelChoice{{IsSelected: false}}},
		},
	}
	kb := BuildTelegramProvidersKeyboard(data)
	if kb == nil {
		t.Fatal("expected keyboard")
	}
	// 2 provider buttons (1 row of 2) + dismiss row = 2 rows
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.InlineKeyboard))
	}
	// Selected provider has ●
	if !strings.Contains(kb.InlineKeyboard[0][0].Text, "●") {
		t.Error("selected provider should have ● marker")
	}
}

func TestBuildTelegramModelQuickKeyboard(t *testing.T) {
	data := ModelPickerData{
		CurrentSelector: "openai^gpt-4o",
		Providers: []ProviderGroup{
			{Name: "OpenAI", Models: []ModelChoice{
				{Selector: "openai^gpt-4o", DisplayName: "gpt-4o", IsSelected: true},
				{Selector: "openai^gpt-4o-mini", DisplayName: "gpt-4o-mini", IsSelected: false},
			}},
			{Name: "Anthropic", Models: []ModelChoice{
				{Selector: "anthropic^claude-sonnet-4-5", DisplayName: "claude-sonnet-4-5", IsSelected: false},
			}},
		},
	}
	kb := BuildTelegramModelQuickKeyboard(data)
	if kb == nil {
		t.Fatal("expected keyboard")
	}
	// Should show current provider models + "查看全部" + dismiss
	lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
	if lastRow[0].CallbackData != cbModelProviders {
		t.Errorf("second-to-last row should be 'view all' button, got callback %q", lastRow[0].CallbackData)
	}
}

func TestFlattenModelChoices(t *testing.T) {
	data := ModelPickerData{
		Providers: []ProviderGroup{
			{Name: "OpenAI", Models: []ModelChoice{
				{Selector: "openai^gpt-4o", DisplayName: "gpt-4o"},
				{Selector: "openai^gpt-4o-mini", DisplayName: "gpt-4o-mini"},
			}},
			{Name: "Anthropic", Models: []ModelChoice{
				{Selector: "anthropic^claude-sonnet-4-5", DisplayName: "claude-sonnet-4-5"},
			}},
		},
	}
	flat := FlattenModelChoices(data)
	if len(flat) != 3 {
		t.Fatalf("expected 3 flat models, got %d", len(flat))
	}
	if flat[2].Selector != "anthropic^claude-sonnet-4-5" {
		t.Errorf("third flat model: got %q", flat[2].Selector)
	}
}

func TestBuildTelegramInteractive_nil(t *testing.T) {
	kb := BuildTelegramInteractive(nil)
	if kb != nil {
		t.Error("expected nil for nil reply")
	}
	kb = BuildTelegramInteractive(&CommandReply{Text: "hello"})
	if kb != nil {
		t.Error("expected nil for reply without Interactive")
	}
}

var _ = telegrambot.InlineKeyboardMarkup{}
