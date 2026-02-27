package browser

import (
	"os"
	"strings"
)

const browserBaseURLEnv = "ARKLOOP_BROWSER_BASE_URL"

func BaseURLFromEnv() string {
	raw := strings.TrimSpace(os.Getenv(browserBaseURLEnv))
	return strings.TrimRight(raw, "/")
}
