package email

import "context"

// NoopMailer 丢弃所有邮件，用于未配置 SMTP 的环境。
type NoopMailer struct{}

func (NoopMailer) Send(_ context.Context, _ Message) error {
	return nil
}
