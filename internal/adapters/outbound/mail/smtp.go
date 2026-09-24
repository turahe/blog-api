// Package mail sends plain-text email over SMTP. Mailpit is the local receiver.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/turahe/blog-api/internal/core/notification/ports"
)

// SMTP delivers messages with net/smtp. Empty username skips authentication.
type SMTP struct {
	host     string
	port     int
	username string
	password string
	from     string
	fromAddr string
	timeout  time.Duration
}

var _ ports.Mailer = (*SMTP)(nil)

// NewSMTP returns an SMTP mailer. from is the envelope and header address.
func NewSMTP(host string, port int, username, password, from string) (*SMTP, error) {
	host = strings.TrimSpace(host)
	from = strings.TrimSpace(from)

	if host == "" {
		return nil, errors.New("smtp host is required")
	}

	if port < 1 || port > 65535 {
		return nil, errors.New("smtp port out of range")
	}

	parsed, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("smtp from address: %w", err)
	}

	return &SMTP{
		host: host, port: port, username: username, password: password,
		from: from, fromAddr: parsed.Address, timeout: 10 * time.Second,
	}, nil
}

// Send delivers one plain-text message. It returns when the server accepts the data.
func (s *SMTP) Send(ctx context.Context, msg ports.Message) error {
	recipient, body, err := s.compose(msg)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{Timeout: s.timeout}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}

	_ = conn.SetDeadline(time.Now().Add(s.timeout))

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()

		return fmt.Errorf("smtp client: %w", err)
	}

	defer func() { _ = client.Close() }()

	return s.deliver(client, recipient, body)
}

// compose validates the headers and renders the message.
func (s *SMTP) compose(msg ports.Message) (recipient string, body []byte, err error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(msg.To))
	if err != nil {
		return "", nil, fmt.Errorf("recipient: %w", err)
	}

	subject := strings.TrimSpace(msg.Subject)
	if subject == "" || strings.ContainsAny(subject, "\r\n") || strings.ContainsAny(parsed.Address, "\r\n") {
		return "", nil, errors.New("refusing email header with line breaks or an empty subject")
	}

	body = []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.from, parsed.Address, subject, msg.Text,
	))

	return parsed.Address, body, nil
}

func (s *SMTP) deliver(client *smtp.Client, recipient string, body []byte) error {
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(s.fromAddr); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}

	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp finish: %w", err)
	}

	return client.Quit()
}
