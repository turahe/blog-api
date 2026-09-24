// Package ports defines the outbound mail contract used by NotificationService.
package ports

import "context"

// Message is one plain-text email. Callers must not put secrets in Subject.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer delivers a single message. Implementations own retries and transport errors.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
