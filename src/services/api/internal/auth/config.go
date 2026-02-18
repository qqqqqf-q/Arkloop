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
	defaultAccessTokenTTL   = 3600
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
			return nil, fmt.Errorf("缺少环境变量 %s", jwtSecretEnv)
		}
		return nil, nil
	}
	if len(secret) < minJWTSecretLengthBytes {
		return nil, fmt.Errorf("%s 太短，至少 %d 字符", jwtSecretEnv, minJWTSecretLengthBytes)
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
		return fmt.Errorf("auth config 不能为空")
	}
	if strings.TrimSpace(c.JWTSecret) == "" {
		return fmt.Errorf("jwt_secret 不能为空")
	}
	if len(c.JWTSecret) < minJWTSecretLengthBytes {
		return fmt.Errorf("jwt_secret 太短，至少 %d 字符", minJWTSecretLengthBytes)
	}
	if c.AccessTokenTTLSeconds <= 0 {
		return fmt.Errorf("access_token_ttl_seconds 必须为正数")
	}
	return nil
}

func parsePositiveInt(raw string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("必须为正整数")
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("必须为正整数")
	}
	return parsed, nil
}
