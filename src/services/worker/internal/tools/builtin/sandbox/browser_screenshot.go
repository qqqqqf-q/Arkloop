package sandbox

import "strings"

const screenshotTimeoutMs = 15_000
const autoScreenshotPath = "/tmp/output/browser-screenshot.png"
const autoScreenshotMinYieldTimeMs = 1_500
const browserAutoPollAttempts = 3

// shouldAutoScreenshot returns true if the browser command triggers visual
// changes that warrant an automatic screenshot capture.
var autoScreenshotPrefixes = []string{
	"navigate ",
	"back",
	"forward",
	"click ",
	"scroll ",
	"tab select ",
	"select ",
	"fill ",
	"type ",
	"press ",
	"drag ",
	"upload ",
	"dialog ",
}

func shouldAutoScreenshot(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)
	for _, prefix := range autoScreenshotPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
		// exact match for commands without arguments (back, forward)
		if prefix == lower || prefix == lower+" " {
			return true
		}
	}
	return false
}

// mergeScreenshotArtifacts appends screenshot artifacts from the screenshot
// exec result into the primary result's artifacts array and sets the
// has_screenshot marker.
func mergeScreenshotArtifacts(primary *map[string]any, screenshotResultJSON map[string]any) {
	if primary == nil || screenshotResultJSON == nil {
		return
	}
	p := *primary

	screenshotArtifacts, ok := screenshotResultJSON["artifacts"]
	if !ok {
		return
	}
	arr, ok := screenshotArtifacts.([]artifactRef)
	if !ok {
		return
	}
	if len(arr) == 0 {
		return
	}

	existing, _ := p["artifacts"].([]artifactRef)
	p["artifacts"] = append(existing, arr...)
	p["has_screenshot"] = true
}

func buildAutoScreenshotCommand() string {
	return "screenshot " + autoScreenshotPath
}

func effectiveBrowserYieldTimeMs(command string, requested int) int {
	if shouldAutoScreenshot(command) && requested > 0 && requested < autoScreenshotMinYieldTimeMs {
		return autoScreenshotMinYieldTimeMs
	}
	return requested
}

func browserContinuationYieldTimeMs(requested int) int {
	if requested <= 0 || requested < autoScreenshotMinYieldTimeMs {
		return autoScreenshotMinYieldTimeMs
	}
	return requested
}
