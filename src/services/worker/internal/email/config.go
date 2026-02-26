package email

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	envFrom    = "ARKLOOP_EMAIL_FROM"
	envHost    = "ARKLOOP_SMTP_HOST"
	envPort    = "ARKLOOP_SMTP_PORT"
	envUser    = "ARKLOOP_SMTP_USER"
	envPass    = "ARKLOOP_SMTP_PASS"
	envTLSMode = "ARKLOOP_SMTP_TLS_MODE"

	defaultPort    = 587
	defaultTLSMode = TLSModeStartTLS
)

type TLSMode string

const (
	TLSModeStartTLS TLSMode = "starttls"
	TLSModeTLS      TLSMode = "tls"
	TLSModeNone     TLSMode = "none"
)

// Config 保存 SMTP 连接参数。From 为空表示邮件功能未启用。
type Config struct {
	From    string
	Host    string
	Port    int
	User    string
	Pass    string
	TLSMode TLSMode
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.From) != ""
}

func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("ARKLOOP_SMTP_HOST is required when ARKLOOP_EMAIL_FROM is set")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("ARKLOOP_SMTP_PORT must be in range 1-65535")
	}
	switch c.TLSMode {
	case TLSModeStartTLS, TLSModeTLS, TLSModeNone:
	default:
		return fmt.Errorf("ARKLOOP_SMTP_TLS_MODE must be starttls, tls, or none")
	}
	return nil
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		Port:    defaultPort,
		TLSMode: defaultTLSMode,
	}

	if v := lookupEnv(envFrom); v != "" {
		cfg.From = v
	}
	if v := lookupEnv(envHost); v != "" {
		cfg.Host = v
	}
	if v := lookupEnv(envPort); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("%s: must be an integer", envPort)
		}
		cfg.Port = p
	}
	if v := lookupEnv(envUser); v != "" {
		cfg.User = v
	}
	if v := lookupEnv(envPass); v != "" {
		cfg.Pass = v
	}
	if v := lookupEnv(envTLSMode); v != "" {
		cfg.TLSMode = TLSMode(strings.ToLower(v))
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// NewMailer 根据配置返回合适的 Mailer。未启用时返回 NoopMailer。
func NewMailer(cfg Config) Mailer {
	if !cfg.Enabled() {
		return NoopMailer{}
	}
	return &SMTPMailer{cfg: cfg}
}

func lookupEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
