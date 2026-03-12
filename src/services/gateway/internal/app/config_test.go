package app

import (
	"testing"
	"time"
)

func TestDefaultConfigIncludesLocalCORSOrigins(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.CORSAllowedOrigins) != 3 {
		t.Fatalf("unexpected default origins: %#v", cfg.CORSAllowedOrigins)
	}
	if cfg.CORSAllowedOrigins[0] != "http://localhost:19080" || cfg.CORSAllowedOrigins[1] != "http://localhost:19081" || cfg.CORSAllowedOrigins[2] != "http://localhost:19082" {
		t.Fatalf("unexpected default origins: %#v", cfg.CORSAllowedOrigins)
	}
}

func TestDefaultConfigUsesGatewayRedisTimeout(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RedisTimeout != 150*time.Millisecond {
		t.Fatalf("unexpected default redis timeout: %s", cfg.RedisTimeout)
	}
}

func TestLoadConfigFromEnvParsesCORSAllowedOrigins(t *testing.T) {
	t.Setenv(corsAllowedOriginsEnv, "https://app.example.com, https://console.example.com")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("unexpected origins: %#v", cfg.CORSAllowedOrigins)
	}
	if cfg.CORSAllowedOrigins[0] != "https://app.example.com" || cfg.CORSAllowedOrigins[1] != "https://console.example.com" {
		t.Fatalf("unexpected origins: %#v", cfg.CORSAllowedOrigins)
	}
}


func TestLoadConfigFromEnvParsesFrontendUpstream(t *testing.T) {
	t.Setenv(gatewayFrontendEnv, "http://console-lite:80")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.FrontendUpstream != "http://console-lite:80" {
		t.Fatalf("unexpected frontend upstream: %q", cfg.FrontendUpstream)
	}
}
