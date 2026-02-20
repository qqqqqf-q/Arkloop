package app

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	dotenvEnableEnv = "ARKLOOP_LOAD_DOTENV"
	dotenvFileEnv   = "ARKLOOP_DOTENV_FILE"
)

var (
	dotenvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	dotenvMu          sync.Mutex
	loadedDotenvPaths = map[string]struct{}{}
)

func LoadDotenvIfEnabled(override bool) (bool, error) {
	rawEnabled, ok := os.LookupEnv(dotenvEnableEnv)
	if !ok {
		return false, nil
	}

	enabled, err := parseBool(rawEnabled)
	if err != nil {
		return false, fmt.Errorf("%s: %w", dotenvEnableEnv, err)
	}
	if !enabled {
		return false, nil
	}

	path, err := resolveDotenvPath()
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	dotenvMu.Lock()
	if _, loaded := loadedDotenvPaths[absPath]; loaded {
		dotenvMu.Unlock()
		return false, nil
	}
	dotenvMu.Unlock()

	values, err := readDotenvFile(absPath)
	if err != nil {
		return false, err
	}

	for key, value := range values {
		if !override {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
		}
		_ = os.Setenv(key, value)
	}

	dotenvMu.Lock()
	loadedDotenvPaths[absPath] = struct{}{}
	dotenvMu.Unlock()
	return true, nil
}

func resolveDotenvPath() (string, error) {
	raw := strings.TrimSpace(os.Getenv(dotenvFileEnv))
	repoRoot := findRepoRoot()

	if raw != "" {
		expanded := expandUser(raw)
		if filepath.IsAbs(expanded) {
			if fileExists(expanded) {
				return expanded, nil
			}
			return "", fmt.Errorf("%s: file does not exist: %s", dotenvFileEnv, raw)
		}

		if fileExists(expanded) {
			return expanded, nil
		}
		if repoRoot != "" {
			candidate := filepath.Join(repoRoot, expanded)
			if fileExists(candidate) {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("%s: file does not exist: %s", dotenvFileEnv, raw)
	}

	if repoRoot == "" {
		return "", nil
	}
	candidate := filepath.Join(repoRoot, ".env")
	if fileExists(candidate) {
		return candidate, nil
	}
	return "", nil
}

func findRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		return ""
	}

	dir := cwd
	for {
		if hasRepoMarker(dir) {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

func hasRepoMarker(dir string) bool {
	if fileExists(filepath.Join(dir, "pyproject.toml")) {
		return true
	}
	if fileExists(filepath.Join(dir, ".git")) {
		return true
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() || info.IsDir()
}

func expandUser(path string) string {
	if path == "" {
		return path
	}
	if path[0] != '~' {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return path
		}
		suffix := strings.TrimPrefix(path, "~")
		suffix = strings.TrimPrefix(suffix, string(os.PathSeparator))
		if suffix == "" {
			return home
		}
		return filepath.Join(home, suffix)
	}
	return path
}

func readDotenvFile(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})

	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		key, value, ok := parseDotenvLine(scanner.Text())
		if !ok {
			continue
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func parseDotenvLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	idx := strings.Index(line, "=")
	if idx <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:idx])
	if key == "" || !dotenvKeyRegex.MatchString(key) {
		return "", "", false
	}

	value := strings.TrimSpace(line[idx+1:])
	if value == "" {
		return key, "", true
	}

	if len(value) >= 2 {
		quote := value[0]
		if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
			return key, value[1 : len(value)-1], true
		}
	}

	trimmed := stripInlineComment(value)
	return key, trimmed, true
}

func stripInlineComment(value string) string {
	for i := 1; i < len(value); i++ {
		if value[i] != '#' {
			continue
		}
		prev := value[i-1]
		if prev != ' ' && prev != '\t' {
			continue
		}
		start := i - 1
		for start > 0 {
			ch := value[start-1]
			if ch != ' ' && ch != '\t' {
				break
			}
			start--
		}
		return strings.TrimSpace(value[:start])
	}
	return strings.TrimSpace(value)
}

func parseBool(raw string) (bool, error) {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	switch cleaned {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("must be a boolean (0/1, true/false)")
	}
}
