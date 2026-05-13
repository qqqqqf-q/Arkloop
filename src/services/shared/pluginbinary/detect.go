package pluginbinary

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"arkloop/services/shared/pluginmanifest"
)

type DetectStatus string

const (
	StatusInstalled DetectStatus = "installed"
	StatusOutdated  DetectStatus = "outdated"
	StatusMissing   DetectStatus = "missing"
	StatusError     DetectStatus = "error"
)

type DetectOptions struct {
	InstallRoot string
	Resolver    pluginmanifest.PlaceholderContext
}

type DetectResult struct {
	Status            DetectStatus
	Path              string
	HelperAppPath     string
	HelperAppName     string
	HelperAppBundleID string
	Version           string
	Error             string
}

func DetectRuntime(ctx context.Context, runtime pluginmanifest.RuntimeConfig, opts DetectOptions) DetectResult {
	if len(runtime.Detect) == 0 {
		return DetectResult{Status: StatusMissing}
	}
	for _, detect := range runtime.Detect {
		result := detectRuntimePath(ctx, runtime.ID, detect, opts)
		if result.Status == StatusInstalled || result.Status == StatusOutdated || result.Status == StatusError {
			return result
		}
	}
	return DetectResult{Status: StatusMissing}
}

func detectRuntimePath(ctx context.Context, runtimeID string, detect pluginmanifest.RuntimeDetectConfig, opts DetectOptions) DetectResult {
	resolvedPath, err := pluginmanifest.ResolveString(detect.Path, opts.Resolver)
	if err != nil {
		return DetectResult{Status: StatusError, Error: err.Error()}
	}
	resolvedPath = resolveInstallPath(opts.InstallRoot, resolvedPath)
	helperAppPath := detectHelperAppPath(resolvedPath)
	helperAppName, helperAppBundleID := detectHelperAppInfo(ctx, helperAppPath)
	if _, err := os.Stat(resolvedPath); err != nil {
		if os.IsNotExist(err) {
			return DetectResult{Status: StatusMissing, Path: resolvedPath, HelperAppPath: helperAppPath, HelperAppName: helperAppName, HelperAppBundleID: helperAppBundleID}
		}
		return DetectResult{Status: StatusError, Path: resolvedPath, HelperAppPath: helperAppPath, HelperAppName: helperAppName, HelperAppBundleID: helperAppBundleID, Error: err.Error()}
	}
	version := ""
	if len(detect.VersionCommand) > 0 {
		args, err := pluginmanifest.ResolveStringSlice(detect.VersionCommand, opts.Resolver)
		if err != nil {
			return DetectResult{Status: StatusError, Path: resolvedPath, Error: err.Error()}
		}
		if len(args) > 0 && strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
			args = append([]string{resolvedPath}, args...)
		} else if args[0] == detect.Path {
			args[0] = resolvedPath
		}
		version, err = runVersionCommand(ctx, args)
		if err != nil {
			return DetectResult{Status: StatusError, Path: resolvedPath, HelperAppPath: helperAppPath, HelperAppName: helperAppName, HelperAppBundleID: helperAppBundleID, Error: err.Error()}
		}
	}
	if detect.VersionMin != "" && compareVersion(version, detect.VersionMin) < 0 {
		return DetectResult{Status: StatusOutdated, Path: resolvedPath, HelperAppPath: helperAppPath, HelperAppName: helperAppName, HelperAppBundleID: helperAppBundleID, Version: version}
	}
	_ = runtimeID
	return DetectResult{Status: StatusInstalled, Path: resolvedPath, HelperAppPath: helperAppPath, HelperAppName: helperAppName, HelperAppBundleID: helperAppBundleID, Version: version}
}

func runVersionCommand(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("version_command must not be empty")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run version_command: %w", err)
	}
	version := parseVersion(output.String())
	if version == "" {
		return "", fmt.Errorf("version_command did not print a version")
	}
	return version, nil
}

func resolveInstallPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(root) == "" {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}

func detectHelperAppPath(executablePath string) string {
	slashPath := filepath.ToSlash(strings.TrimSpace(executablePath))
	marker := ".app/Contents/MacOS/"
	index := strings.Index(slashPath, marker)
	if index < 0 {
		return ""
	}
	return filepath.FromSlash(slashPath[:index+len(".app")])
}

var versionPattern = regexp.MustCompile(`\d+(?:\.\d+){0,2}(?:[-+][0-9A-Za-z.-]+)?`)

func parseVersion(output string) string {
	return versionPattern.FindString(output)
}

func compareVersion(current, minimum string) int {
	currentParts := versionNumbers(current)
	minimumParts := versionNumbers(minimum)
	for i := 0; i < 3; i++ {
		if currentParts[i] > minimumParts[i] {
			return 1
		}
		if currentParts[i] < minimumParts[i] {
			return -1
		}
	}
	return 0
}

func versionNumbers(version string) [3]int {
	var parts [3]int
	version = parseVersion(version)
	raw := strings.FieldsFunc(version, func(r rune) bool {
		return r == '.' || r == '-' || r == '+'
	})
	for i := 0; i < len(raw) && i < 3; i++ {
		value, _ := strconv.Atoi(raw[i])
		parts[i] = value
	}
	return parts
}
