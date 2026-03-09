package sandbox

import "testing"

func TestShouldAutoScreenshot(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		// visual commands → true
		{"navigate https://example.com", true},
		{"Navigate https://example.com", true},
		{"back", true},
		{"forward", true},
		{"click @e3", true},
		{"scroll down", true},
		{"scroll up", true},
		{"scroll to @e5", true},
		{"tab select 2", true},
		{"select @e1 option1", true},
		{"fill {\"@e1\":\"val\"}", true},
		{"type @e2 hello world", true},
		{"press Enter", true},
		{"drag @e1 @e2", true},
		{"upload @e3 /tmp/file.pdf", true},
		{"dialog accept", true},

		// data commands → false
		{"snapshot", false},
		{"screenshot", false},
		{"console", false},
		{"network", false},
		{"cookie get", false},
		{"cookie set {}", false},
		{"tab list", false},
		{"evaluate document.title", false},
		{"close", false},
		{"hover @e1", false},

		// edge cases
		{"", false},
		{"   ", false},
		{"  navigate https://example.com  ", true},
		{"CLICK @e1", true},
	}

	for _, tc := range cases {
		got := shouldAutoScreenshot(tc.command)
		if got != tc.want {
			t.Errorf("shouldAutoScreenshot(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestMergeScreenshotArtifacts(t *testing.T) {
	t.Run("appends screenshot artifacts to primary", func(t *testing.T) {
		primary := map[string]any{
			"stdout": "ok",
		}
		screenshot := map[string]any{
			"artifacts": []artifactRef{
				{Key: "org/sess/1/screenshot.png", Filename: "screenshot.png", Size: 1024, MimeType: "image/png"},
			},
		}
		mergeScreenshotArtifacts(&primary, screenshot)

		artifacts, ok := primary["artifacts"].([]artifactRef)
		if !ok || len(artifacts) != 1 {
			t.Fatalf("expected 1 artifact, got %v", primary["artifacts"])
		}
		if artifacts[0].Key != "org/sess/1/screenshot.png" {
			t.Errorf("unexpected artifact key: %s", artifacts[0].Key)
		}
		if v, _ := primary["has_screenshot"].(bool); !v {
			t.Error("expected has_screenshot=true")
		}
	})

	t.Run("merges with existing artifacts", func(t *testing.T) {
		primary := map[string]any{
			"artifacts": []artifactRef{
				{Key: "existing.txt", Filename: "existing.txt", Size: 100, MimeType: "text/plain"},
			},
		}
		screenshot := map[string]any{
			"artifacts": []artifactRef{
				{Key: "screenshot.png", Filename: "screenshot.png", Size: 2048, MimeType: "image/png"},
			},
		}
		mergeScreenshotArtifacts(&primary, screenshot)

		artifacts, ok := primary["artifacts"].([]artifactRef)
		if !ok || len(artifacts) != 2 {
			t.Fatalf("expected 2 artifacts, got %v", primary["artifacts"])
		}
	})

	t.Run("no-op when screenshot has no artifacts", func(t *testing.T) {
		primary := map[string]any{"stdout": "ok"}
		screenshot := map[string]any{}
		mergeScreenshotArtifacts(&primary, screenshot)

		if _, ok := primary["has_screenshot"]; ok {
			t.Error("should not set has_screenshot when no artifacts")
		}
	})

	t.Run("no-op with nil inputs", func(t *testing.T) {
		mergeScreenshotArtifacts(nil, nil)

		primary := map[string]any{}
		mergeScreenshotArtifacts(&primary, nil)
		if _, ok := primary["has_screenshot"]; ok {
			t.Error("should not set has_screenshot with nil screenshot")
		}
	})
}
