package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPMailer 使用标准 net/smtp 发送邮件。
// 支持 STARTTLS（端口 587）和 TLS（端口 465）。
type SMTPMailer struct {
	cfg Config
}

func (m *SMTPMailer) Send(_ context.Context, msg Message) error {
	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprintf("%d", m.cfg.Port))

	body := m.buildRaw(msg)

	switch m.cfg.TLSMode {
	case TLSModeTLS:
		return m.sendTLS(addr, body, msg.To)
	default: // starttls or none
		return m.sendSMTP(addr, body, msg.To)
	}
}

func (m *SMTPMailer) sendSMTP(addr string, body []byte, to string) error {
	var auth smtp.Auth
	if m.cfg.User != "" {
		host, _, _ := net.SplitHostPort(addr)
		auth = smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, host)
	}

	return smtp.SendMail(addr, auth, m.cfg.From, []string{to}, body)
}

func (m *SMTPMailer) sendTLS(addr string, body []byte, to string) error {
	host, _, _ := net.SplitHostPort(addr)
	tlsCfg := &tls.Config{ServerName: host}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("smtp tls dial: %w", err)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Quit() //nolint:errcheck

	if m.cfg.User != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	defer wc.Close() //nolint:errcheck

	if _, err := wc.Write(body); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	return nil
}

func (m *SMTPMailer) buildRaw(msg Message) []byte {
	var b strings.Builder

	b.WriteString("From: " + m.cfg.From + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + msg.Subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.HTML != "" && msg.Text != "" {
		boundary := "boundary_arkloop_alt"
		b.WriteString(`Content-Type: multipart/alternative; boundary="` + boundary + `"` + "\r\n\r\n")
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(msg.Text + "\r\n\r\n")
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(msg.HTML + "\r\n\r\n")
		b.WriteString("--" + boundary + "--\r\n")
	} else if msg.HTML != "" {
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(msg.HTML)
	} else {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(msg.Text)
	}

	return []byte(b.String())
}
