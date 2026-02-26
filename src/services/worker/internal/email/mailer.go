package email

import "context"

// Message 是一封待发送的邮件。
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Mailer 是邮件发送的抽象接口。
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
