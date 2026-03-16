package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	bridgeAddrEnv        = "ARKLOOP_BRIDGE_ADDR"
	bridgeProjectDirEnv  = "ARKLOOP_BRIDGE_PROJECT_DIR"
	bridgeModulesFileEnv = "ARKLOOP_BRIDGE_MODULES_FILE"
	bridgeAuditLogEnv    = "ARKLOOP_BRIDGE_AUDIT_LOG"
	bridgeCORSOriginsEnv = "ARKLOOP_BRIDGE_CORS_ORIGINS"

	defaultBridgeAddr        = "127.0.0.1:19003"
	defaultModulesFileRel    = "install/modules.yaml"
)

var defaultBridgeCORSOrigins = []string{
	"http://localhost:19080",
	"http://localhost:19081",
	"http://localhost:19082",
	"http://localhost:19083",
	"http://localhost:19000",
	"http://localhost:19006",
	"http://127.0.0.1:19080",
	"http://127.0.0.1:19081",
	"http://127.0.0.1:19082",
	"http://127.0.0.1:19083",
	"http://127.0.0.1:19000",
	"http://127.0.0.1:19006",
}

type Config struct {
	Addr            string
	ProjectDir      string
	ModulesFile     string
	AuditLog        string
	CORSAllowedOrigins []string
}

func DefaultConfig() Config {
	return Config{
		Addr:               defaultBridgeAddr,
		CORSAllowedOrigins: append([]string(nil), defaultBridgeCORSOrigins...),
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if raw := strings.TrimSpace(os.Getenv(bridgeAddrEnv)); raw != "" {
		cfg.Addr = raw
	}

	if raw := strings.TrimSpace(os.Getenv(bridgeProjectDirEnv)); raw != "" {
		cfg.ProjectDir = raw
	} else {
		cfg.ProjectDir = detectProjectDir()
	}

	if raw := strings.TrimSpace(os.Getenv(bridgeModulesFileEnv)); raw != "" {
		cfg.ModulesFile = raw
	} else if cfg.ProjectDir != "" {
		cfg.ModulesFile = filepath.Join(cfg.ProjectDir, defaultModulesFileRel)
	}

	cfg.AuditLog = strings.TrimSpace(os.Getenv(bridgeAuditLogEnv))

	if raw := strings.TrimSpace(os.Getenv(bridgeCORSOriginsEnv)); raw != "" {
		cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, splitCSV(raw)...)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// detectProjectDir attempts to find the Arkloop project root by looking for
// compose.yaml, starting from the current working directory, then from the
// executable's directory, walking upward.
func detectProjectDir() string {
	// Try from current working directory first.
	if cwd, err := os.Getwd(); err == nil {
		if dir := walkUpForCompose(cwd); dir != "" {
			return dir
		}
	}
	// Fall back to executable's directory.
	if exe, err := os.Executable(); err == nil {
		if dir := walkUpForCompose(filepath.Dir(exe)); dir != "" {
			return dir
		}
	}
	return ""
}

func walkUpForCompose(start string) string {
	dir := start
	for {
		if fileExists(filepath.Join(dir, "compose.yaml")) {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("addr must not be empty")
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", c.Addr)
	if err != nil {
		return fmt.Errorf("addr invalid: %w", err)
	}

	// In containers, port mapping provides the security boundary.
	if !isLoopbackAddr(tcpAddr.IP) && os.Getenv("ARKLOOP_BRIDGE_CONTAINERIZED") != "true" {
		return fmt.Errorf("addr must be a loopback address (127.0.0.1, ::1, or localhost) for security, got: %s", c.Addr)
	}

	for _, origin := range c.CORSAllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("cors_allowed_origins must not contain *")
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid cors origin: %s", origin)
		}
	}

	return nil
}

// isLoopbackAddr checks whether an IP is a loopback address.
// Nil IP (from unresolved "localhost") is treated as loopback.
func isLoopbackAddr(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback()
}

func splitCSV(raw string) []string {
	items := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			items = append(items, value)
		}
	}
	return items
}
