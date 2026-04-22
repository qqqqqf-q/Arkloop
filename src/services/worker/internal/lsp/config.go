//go:build desktop

package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// knownServer describes a well-known language server for auto-detection.
type knownServer struct {
	Command    string
	Args       []string
	Extensions []string
	// IDEGlobs are glob patterns relative to IDE extension dirs.
	// Empty means PATH-only detection (no IDE fallback).
	IDEGlobs []string
}

var knownServers = map[string]knownServer{
	"gopls": {
		Command:    "gopls",
		Args:       []string{"serve"},
		Extensions: []string{".go"},
		IDEGlobs:   []string{"golang.go-*/bin/gopls"},
	},
	"typescript-language-server": {
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
	},
	"pyright-langserver": {
		Command:    "pyright-langserver",
		Args:       []string{"--stdio"},
		Extensions: []string{".py"},
	},
	"rust-analyzer": {
		Command:    "rust-analyzer",
		Args:       []string{},
		Extensions: []string{".rs"},
		IDEGlobs:   []string{"rust-lang.rust-analyzer-*/server/rust-analyzer"},
	},
}

type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

type ServerConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Extensions  []string          `json:"extensions"`
	InitOptions json.RawMessage   `json:"initOptions,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// ideExtensionDirs returns paths to known IDE extension directories.
func ideExtensionDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dirs := []string{
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
		filepath.Join(home, ".cursor", "extensions"),
	}
	var result []string
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			result = append(result, d)
		}
	}
	return result
}

// autoDetectServers finds well-known language servers on the system.
// PATH hits take priority over IDE extension binaries.
func autoDetectServers(logger *slog.Logger) map[string]ServerConfig {
	detected := make(map[string]ServerConfig)
	ideDirs := ideExtensionDirs()

	for name, ks := range knownServers {
		// try PATH first
		if path, err := exec.LookPath(ks.Command); err == nil {
			logger.Info("auto-detected LSP server in PATH", "server", name, "path", path)
			detected[name] = ServerConfig{
				Command:    ks.Command,
				Args:       ks.Args,
				Extensions: ks.Extensions,
			}
			continue
		}

		// fallback: scan IDE extension dirs
		if len(ks.IDEGlobs) == 0 {
			continue
		}
		found := false
		for _, ideDir := range ideDirs {
			if found {
				break
			}
			for _, pattern := range ks.IDEGlobs {
				matches, _ := filepath.Glob(filepath.Join(ideDir, pattern))
				if len(matches) == 0 {
					continue
				}
				// use the last match (highest version lexicographically)
				bin := matches[len(matches)-1]
				// on non-windows, check executable bit
				if runtime.GOOS != "windows" {
					if info, err := os.Stat(bin); err != nil || info.Mode()&0111 == 0 {
						continue
					}
				}
				logger.Info("auto-detected LSP server in IDE extensions", "server", name, "path", bin)
				detected[name] = ServerConfig{
					Command:    bin,
					Args:       ks.Args,
					Extensions: ks.Extensions,
				}
				found = true
				break
			}
		}
	}
	return detected
}

// mergeAutoDetected runs auto-detection and adds servers to cfg
// only if no existing server already claims their extensions.
func mergeAutoDetected(cfg *Config, logger *slog.Logger) {
	detected := autoDetectServers(logger)
	if len(detected) == 0 {
		return
	}

	// collect extensions already claimed by lsp.json
	claimed := make(map[string]bool)
	for _, sc := range cfg.Servers {
		for _, ext := range sc.Extensions {
			claimed[strings.ToLower(ext)] = true
		}
	}

	var added []string
	for name, sc := range detected {
		conflict := false
		for _, ext := range sc.Extensions {
			if claimed[strings.ToLower(ext)] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		cfg.Servers[name] = sc
		for _, ext := range sc.Extensions {
			claimed[strings.ToLower(ext)] = true
		}
		added = append(added, name)
	}

	if len(added) > 0 {
		logger.Info("auto-detected LSP servers", "count", len(added), "servers", added)
	}
}

// LoadConfig reads LSP configuration from ~/.arkloop/lsp.json.
// Returns an empty config (not an error) if the file doesn't exist.
func LoadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	path := filepath.Join(home, ".arkloop", "lsp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := &Config{Servers: map[string]ServerConfig{}}
			mergeAutoDetected(cfg, slog.Default())
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]ServerConfig{}
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	mergeAutoDetected(&cfg, slog.Default())
	return &cfg, nil
}

// Validate checks config integrity: required fields, extension format,
// and no duplicate extensions across servers.
func Validate(cfg *Config) error {
	extOwner := make(map[string]string) // ext -> server name

	for name, sc := range cfg.Servers {
		if strings.TrimSpace(sc.Command) == "" {
			return fmt.Errorf("server %q: command is required", name)
		}
		if len(sc.Extensions) == 0 {
			return fmt.Errorf("server %q: at least one extension is required", name)
		}
		for _, ext := range sc.Extensions {
			if !strings.HasPrefix(ext, ".") {
				return fmt.Errorf("server %q: extension %q must start with '.'", name, ext)
			}
			lower := strings.ToLower(ext)
			if prev, ok := extOwner[lower]; ok {
				return fmt.Errorf("duplicate extension %q: claimed by both %q and %q", ext, prev, name)
			}
			extOwner[lower] = name
		}

		if _, err := exec.LookPath(sc.Command); err != nil {
			slog.Warn("lsp server command not found in PATH", "server", name, "command", sc.Command)
		}
	}
	return nil
}

func (c *Config) IsEmpty() bool {
	return len(c.Servers) == 0
}

// ServerForExtension returns the server name and config for a given file extension.
// ext should be lowercase with a leading dot (e.g. ".go").
func (c *Config) ServerForExtension(ext string) (string, *ServerConfig, bool) {
	lower := strings.ToLower(ext)
	for name, sc := range c.Servers {
		for _, e := range sc.Extensions {
			if strings.ToLower(e) == lower {
				return name, &sc, true
			}
		}
	}
	return "", nil, false
}
