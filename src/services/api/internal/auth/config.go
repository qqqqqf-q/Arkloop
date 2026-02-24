package auth

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	jwtSecretEnv            = "ARKLOOP_AUTH_JWT_SECRET"
	accessTokenTTLEnv       = "ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS"
	defaultAccessTokenTTL   = 2592000 // 30 days
	minJWTSecretLengthBytes = 32
)

type Config struct {
	JWTSecret             string
	AccessTokenTTLSeconds int
}

func LoadConfigFromEnv(required bool) (*Config, error) {
	secret := strings.TrimSpace(os.Getenv(jwtSecretEnv))
	if secret == "" {
		if required {
			return nil, fmt.Errorf("missing environment variable %s", jwtSecretEnv)
		}
		return nil, nil
	}
	if len(secret) < minJWTSecretLengthBytes {
		return nil, fmt.Errorf("%s too short, minimum %d characters", jwtSecretEnv, minJWTSecretLengthBytes)
	}

	ttlSeconds := defaultAccessTokenTTL
	if raw := strings.TrimSpace(os.Getenv(accessTokenTTLEnv)); raw != "" {
		parsed, err := parsePositiveInt(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", accessTokenTTLEnv, err)
		}
		ttlSeconds = parsed
	}

	cfg := &Config{
		JWTSecret:             secret,
		AccessTokenTTLSeconds: ttlSeconds,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("auth config must not be nil")
	}
	if strings.TrimSpace(c.JWTSecret) == "" {
		return fmt.Errorf("jwt_secret must not be empty")
	}
	if len(c.JWTSecret) < minJWTSecretLengthBytes {
		return fmt.Errorf("jwt_secret too short, minimum %d characters", minJWTSecretLengthBytes)
	}
	if c.AccessTokenTTLSeconds <= 0 {
		return fmt.Errorf("access_token_ttl_seconds must be positive")
	}
	return nil
}

func parsePositiveInt(raw string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be positive")
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return parsed, nil
}
