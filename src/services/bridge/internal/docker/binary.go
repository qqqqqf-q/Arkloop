package docker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrDockerUnavailable = errors.New("docker cli unavailable")

func ResolveBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("ARKLOOP_DOCKER_BIN")); override != "" {
		resolved, err := resolveExplicitBinary(override)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
		}
		return resolved, nil
	}

	if resolved, err := exec.LookPath("docker"); err == nil {
		return resolved, nil
	}

	for _, candidate := range commonBinaryCandidates() {
		resolved, err := resolveExplicitBinary(candidate)
		if err == nil {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("%w: docker executable not found", ErrDockerUnavailable)
}

func resolveExplicitBinary(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", errors.New("empty docker binary path")
	}

	if strings.ContainsRune(candidate, os.PathSeparator) || filepath.IsAbs(candidate) {
		if err := isExecutable(candidate); err != nil {
			return "", fmt.Errorf("%s: %w", candidate, err)
		}
		return candidate, nil
	}

	resolved, err := exec.LookPath(candidate)
	if err != nil {
		return "", fmt.Errorf("%s: %w", candidate, err)
	}
	return resolved, nil
}

func isExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("is a directory")
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}

func commonBinaryCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/bin/docker",
			"/usr/local/bin/docker",
			"/usr/bin/docker",
			"/Applications/Docker.app/Contents/Resources/bin/docker",
		}
	case "windows":
		candidates := []string{
			`C:\Program Files\Docker\Docker\resources\bin\docker.exe`,
			`C:\Program Files (x86)\Docker\Docker\resources\bin\docker.exe`,
		}
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			candidates = append(candidates, filepath.Join(local, "Programs", "Docker", "Docker", "resources", "bin", "docker.exe"))
		}
		return candidates
	default:
		return []string{
			"/usr/bin/docker",
			"/usr/local/bin/docker",
			"/snap/bin/docker",
		}
	}
}
