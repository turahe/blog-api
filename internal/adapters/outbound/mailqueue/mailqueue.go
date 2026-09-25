// Package mailqueue hands rendered emails to app worker through the outbox. Messages carry
// raw reset and verification tokens, so the command stores them encrypted with
// APP_ENCRYPTION_KEY; the broker and outbox only ever see ciphertext.
package mailqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

// Box encrypts and decrypts command payloads.
type Box interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(ciphertext string) ([]byte, error)
}

// Command is the notification.email.requested payload.
type Command struct {
	Ciphertext string `json:"ciphertext"`
}

type sealed struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// Mailer implements ports.Mailer by recording an email command. When the command cannot be
// stored it sends through fallback so the email is not lost.
type Mailer struct {
	recorder event.Recorder
	box      Box
	fallback ports.Mailer
	logger   *slog.Logger
	now      func() time.Time
}

var _ ports.Mailer = (*Mailer)(nil)

// New returns a Mailer that records commands with recorder, sealed by box.
func New(recorder event.Recorder, box Box, fallback ports.Mailer, logger *slog.Logger) *Mailer {
	if logger == nil {
		logger = slog.Default()
	}

	return &Mailer{recorder: recorder, box: box, fallback: fallback, logger: logger, now: time.Now}
}

// Send records msg for the worker, or sends it inline when recording fails.
func (m *Mailer) Send(ctx context.Context, msg ports.Message) error {
	err := m.enqueue(ctx, msg)
	if err == nil {
		return nil
	}

	m.logger.WarnContext(ctx, "mailqueue: enqueue failed, sending inline", "error", err)

	return m.fallback.Send(ctx, msg)
}

func (m *Mailer) enqueue(ctx context.Context, msg ports.Message) error {
	plaintext, err := json.Marshal(sealed(msg))
	if err != nil {
		return fmt.Errorf("encode email: %w", err)
	}

	ciphertext, err := m.box.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt email: %w", err)
	}

	cmd := event.New(event.NotificationEmailRequested, event.AggregateEmail, uuid.Nil, nil, m.now(),
		Command{Ciphertext: ciphertext})

	return m.recorder.Record(ctx, cmd)
}

// ErrMalformed marks a command that can never be sent; it is dead-lettered without retries.
var ErrMalformed = fmt.Errorf("malformed email command: %w", messaging.ErrPermanent)

// ErrSendFailed is returned when the mailer fails. The mailer's own error is only logged: SMTP
// replies often echo the recipient address, and the poison queue copies handler errors into
// plaintext broker headers.
var ErrSendFailed = errors.New("send email failed")

// Handler returns a worker handler that decrypts a command and sends it with mailer.
// Errors never include the message body or recipient.
func Handler(box Box, mailer ports.Mailer, logger *slog.Logger) message.NoPublishHandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(msg *message.Message) error {
		var cmd Command
		if err := json.Unmarshal(msg.Payload, &cmd); err != nil || cmd.Ciphertext == "" {
			return fmt.Errorf("%w: payload is not a command", ErrMalformed)
		}

		plaintext, err := box.Decrypt(cmd.Ciphertext)
		if err != nil {
			return fmt.Errorf("%w: cannot decrypt", ErrMalformed)
		}

		var email sealed
		if err := json.Unmarshal(plaintext, &email); err != nil || email.To == "" {
			return fmt.Errorf("%w: sealed email is invalid", ErrMalformed)
		}

		if err := mailer.Send(msg.Context(), ports.Message(email)); err != nil {
			logger.WarnContext(msg.Context(), "mailqueue: send failed", "message_id", msg.UUID, "error", err)

			return ErrSendFailed
		}

		return nil
	}
}
