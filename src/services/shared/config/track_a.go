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
			Key:         "nowledge.base_url",
			Type:        TypeString,
			Default:     "",
			Description: "Nowledge Base URL",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_NOWLEDGE_BASE_URL"},
		},
		{
			Key:         "nowledge.api_key",
			Type:        TypeString,
			Default:     "",
			Description: "Nowledge API Key",
			Sensitive:   true,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_NOWLEDGE_API_KEY"},
		},
		{
			Key:         "nowledge.request_timeout_ms",
			Type:        TypeInt,
			Default:     "30000",
			Description: "Nowledge request timeout in milliseconds",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS"},
		},
		{
			Key:         "nowledge.max_context_results",
			Type:        TypeInt,
			Default:     "5",
			Description: "Max recalled memories injected per turn (1-20)",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_NOWLEDGE_MAX_CONTEXT_RESULTS"},
		},
		{
			Key:         "nowledge.recall_min_score",
			Type:        TypeInt,
			Default:     "0",
			Description: "Min recall score threshold 0-100 (0 = no filter)",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_NOWLEDGE_RECALL_MIN_SCORE"},
		},

		{
			Key:         "llm.retry.max_attempts",
			Type:        TypeInt,
			Default:     "10",
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
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}
