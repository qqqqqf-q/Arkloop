//go:build darwin

package pluginbinary

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectHelperAppInfo(ctx context.Context, helperAppPath string) (string, string) {
	if strings.TrimSpace(helperAppPath) == "" {
		return "", ""
	}
	infoPlist := filepath.Join(helperAppPath, "Contents", "Info.plist")
	name := plistValue(ctx, infoPlist, "CFBundleDisplayName")
	if name == "" {
		name = plistValue(ctx, infoPlist, "CFBundleName")
	}
	return name, plistValue(ctx, infoPlist, "CFBundleIdentifier")
}

func plistValue(ctx context.Context, path string, key string) string {
	output, err := exec.CommandContext(ctx, "plutil", "-extract", key, "raw", "-o", "-", path).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
