package config

import "fmt"

func RegisterTrackA(r *Registry) error {
	if r == nil {
		return fmt.Errorf("registry must not be nil")
	}

	entries := []Entry{
		{
			Key:         "email.from",
			Type:        TypeString,
			Default:     "",
			Description: "SMTP 发件人地址，留空表示禁用邮件发送",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_EMAIL_FROM"},
		},
		{
			Key:         "email.smtp_host",
			Type:        TypeString,
			Default:     "",
			Description: "SMTP Host",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SMTP_HOST"},
		},
		{
			Key:         "email.smtp_port",
			Type:        TypeInt,
			Default:     "587",
			Description: "SMTP 端口",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SMTP_PORT"},
		},
		{
			Key:         "email.smtp_user",
			Type:        TypeString,
			Default:     "",
			Description: "SMTP 用户名",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SMTP_USER"},
		},
		{
			Key:         "email.smtp_pass",
			Type:        TypeString,
			Default:     "",
			Description: "SMTP 密码",
			Sensitive:   true,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SMTP_PASS"},
		},
		{
			Key:         "email.smtp_tls_mode",
			Type:        TypeString,
			Default:     "starttls",
			Description: "SMTP TLS 模式：starttls/tls/none",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SMTP_TLS_MODE"},
		},

		{
			Key:         "web_search.provider",
			Type:        TypeString,
			Default:     "",
			Description: "Web Search Provider：searxng/tavily",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "web_search.searxng_base_url",
			Type:        TypeString,
			Default:     "",
			Description: "SearXNG Base URL",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "web_search.tavily_api_key",
			Type:        TypeString,
			Default:     "",
			Description: "Tavily API Key",
			Sensitive:   true,
			Scope:       ScopeBoth,
		},

		{
			Key:         "web_fetch.provider",
			Type:        TypeString,
			Default:     "basic",
			Description: "Web Fetch Provider：basic/firecrawl/jina",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "web_fetch.firecrawl_api_key",
			Type:        TypeString,
			Default:     "",
			Description: "Firecrawl API Key",
			Sensitive:   true,
			Scope:       ScopeBoth,
		},
		{
			Key:         "web_fetch.firecrawl_base_url",
			Type:        TypeString,
			Default:     "",
			Description: "Firecrawl Base URL",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "web_fetch.jina_api_key",
			Type:        TypeString,
			Default:     "",
			Description: "Jina API Key",
			Sensitive:   true,
			Scope:       ScopeBoth,
		},

		{
			Key:         "openviking.base_url",
			Type:        TypeString,
			Default:     "",
			Description: "OpenViking Base URL",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "openviking.root_api_key",
			Type:        TypeString,
			Default:     "",
			Description: "OpenViking Root API Key",
			Sensitive:   true,
			Scope:       ScopePlatform,
		},

		{
			Key:         "turnstile.secret_key",
			Type:        TypeString,
			Default:     "",
			Description: "Turnstile Secret Key",
			Sensitive:   true,
			Scope:       ScopePlatform,
		},
		{
			Key:         "turnstile.site_key",
			Type:        TypeString,
			Default:     "",
			Description: "Turnstile Site Key",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "turnstile.allowed_host",
			Type:        TypeString,
			Default:     "",
			Description: "Turnstile Allowed Host",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},

		{
			Key:         "gateway.ip_mode",
			Type:        TypeString,
			Default:     "direct",
			Description: "Gateway IP 模式：direct/cloudflare/trusted_proxy",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "gateway.trusted_cidrs",
			Type:        TypeString,
			Default:     "",
			Description: "Gateway 可信代理 CIDR 列表",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "gateway.risk_reject_threshold",
			Type:        TypeInt,
			Default:     "0",
			Description: "Gateway 风险拒绝阈值（0-100）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "gateway.ratelimit_capacity",
			Type:        TypeNumber,
			Default:     "600",
			Description: "Gateway Rate Limit Capacity",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_RATELIMIT_CAPACITY"},
		},
		{
			Key:         "gateway.ratelimit_rate_per_minute",
			Type:        TypeNumber,
			Default:     "300",
			Description: "Gateway Rate Limit Per Minute",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_RATELIMIT_RATE_PER_MINUTE"},
		},

		{
			Key:         "llm.retry.max_attempts",
			Type:        TypeInt,
			Default:     "3",
			Description: "LLM 重试最大次数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "llm.retry.base_delay_ms",
			Type:        TypeInt,
			Default:     "1000",
			Description: "LLM 重试基础延迟（毫秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},

		{
			Key:         "app.base_url",
			Type:        TypeString,
			Default:     "",
			Description: "应用基础 URL，用于邮件中的链接等",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_APP_BASE_URL"},
		},
		{
			Key:         "title_summarizer.agent_config_id",
			Type:        TypeString,
			Default:     "",
			Description: "标题摘要生成器使用的 Agent Config ID",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}
