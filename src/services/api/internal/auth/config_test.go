package auth

import "testing"

func TestLoadConfigFromEnv_DefaultRefreshTokenTTL(t *testing.T) {
	t.Setenv(jwtSecretEnv, "test-secret-should-be-long-enough-32chars")
	t.Setenv(refreshTokenTTLEnv, "")

	cfg, err := LoadConfigFromEnv(true)
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg == nil {
		t.Fatal("config must not be nil")
	}
	if cfg.RefreshTokenTTLSeconds != defaultRefreshTokenTTL {
		t.Fatalf("RefreshTokenTTLSeconds=%d want=%d", cfg.RefreshTokenTTLSeconds, defaultRefreshTokenTTL)
	}
}

func TestLoadConfigFromEnv_OverrideRefreshTokenTTL(t *testing.T) {
	t.Setenv(jwtSecretEnv, "test-secret-should-be-long-enough-32chars")
	t.Setenv(refreshTokenTTLEnv, "60")

	cfg, err := LoadConfigFromEnv(true)
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg == nil {
		t.Fatal("config must not be nil")
	}
	if cfg.RefreshTokenTTLSeconds != 60 {
		t.Fatalf("RefreshTokenTTLSeconds=%d want=60", cfg.RefreshTokenTTLSeconds)
	}
}
