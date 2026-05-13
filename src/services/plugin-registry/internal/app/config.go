package app

import (
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	addrEnv       = "ARKLOOP_PLUGIN_REGISTRY_ADDR"
	databaseEnv   = "ARKLOOP_DATABASE_URL"
	storageDirEnv = "ARKLOOP_PLUGIN_REGISTRY_STORAGE_DIR"
	adminTokenEnv = "ARKLOOP_PLUGIN_REGISTRY_ADMIN_TOKEN"

	defaultAddr = "0.0.0.0:19004"
)

type Config struct {
	Addr        string
	DatabaseURL string
	StorageDir  string
	AdminToken  string
}

func DefaultConfig() Config {
	return Config{Addr: defaultAddr}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()
	if raw := strings.TrimSpace(os.Getenv(addrEnv)); raw != "" {
		cfg.Addr = raw
	}
	cfg.DatabaseURL = strings.TrimSpace(os.Getenv(databaseEnv))
	cfg.StorageDir = strings.TrimSpace(os.Getenv(storageDirEnv))
	cfg.AdminToken = strings.TrimSpace(os.Getenv(adminTokenEnv))
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("%s must not be empty", addrEnv)
	}
	if _, err := net.ResolveTCPAddr("tcp", c.Addr); err != nil {
		return fmt.Errorf("%s is invalid: %w", addrEnv, err)
	}
	return nil
}
