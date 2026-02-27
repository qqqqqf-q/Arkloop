package openviking

import (
	"context"
	"fmt"
	"os"
	"strings"

	"arkloop/services/worker/internal/memory"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	envBaseURL    = "ARKLOOP_OPENVIKING_BASE_URL"
	envRootAPIKey = "ARKLOOP_OPENVIKING_ROOT_API_KEY"

	settingBaseURL    = "openviking.base_url"
	settingRootAPIKey = "openviking.root_api_key"
)

// Config 保存 OpenViking HTTP 客户端连接参数。
type Config struct {
	BaseURL    string
	RootAPIKey string
}

// Enabled 仅当 BaseURL 非空时视为启用。
func (c Config) Enabled() bool {
	return strings.TrimSpace(c.BaseURL) != ""
}

// LoadConfigFromEnv 从环境变量加载配置，供本地开发或无 DB 时使用。
func LoadConfigFromEnv() Config {
	return Config{
		BaseURL:    strings.TrimSpace(os.Getenv(envBaseURL)),
		RootAPIKey: strings.TrimSpace(os.Getenv(envRootAPIKey)),
	}
}

// LoadConfigFromDB 从 platform_settings 读取 openviking.* 配置。
// 返回 (cfg, true, nil) 表示 DB 中存在配置；(zero, false, nil) 表示未配置，调用方应回退到 ENV。
func LoadConfigFromDB(ctx context.Context, pool *pgxpool.Pool) (Config, bool, error) {
	rows, err := pool.Query(ctx,
		`SELECT key, value FROM platform_settings WHERE key LIKE 'openviking.%'`)
	if err != nil {
		return Config{}, false, fmt.Errorf("query openviking config: %w", err)
	}
	defer rows.Close()

	m := make(map[string]string, 2)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Config{}, false, err
		}
		m[k] = v
	}
	if rows.Err() != nil {
		return Config{}, false, rows.Err()
	}

	baseURL := strings.TrimSpace(m[settingBaseURL])
	if baseURL == "" {
		return Config{}, false, nil
	}

	return Config{
		BaseURL:    baseURL,
		RootAPIKey: strings.TrimSpace(m[settingRootAPIKey]),
	}, true, nil
}

// NewProvider 根据配置返回 MemoryProvider；cfg 未启用时返回 nil（调用方应跳过 Memory）。
func NewProvider(cfg Config) memory.MemoryProvider {
	if !cfg.Enabled() {
		return nil
	}
	return newClient(cfg.BaseURL, cfg.RootAPIKey)
}
