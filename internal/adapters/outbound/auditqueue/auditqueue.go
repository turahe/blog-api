// Package auditqueue moves audit batches from API replicas to app worker over the message
// broker, so no API replica spends database time on audit_logs inserts. Entries carry client
// IPs and user agents, so each batch is encrypted with APP_ENCRYPTION_KEY; the broker only
// ever sees ciphertext.
package auditqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/audit/ports"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

// Box encrypts and decrypts batch payloads.
type Box interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(ciphertext string) ([]byte, error)
}

// Command is the audit.entries.recorded payload.
type Command struct {
	Ciphertext string `json:"ciphertext"`
}

// entry is the sealed wire form of domain.Entry; the fields must stay identical so the two
// convert into each other.
type entry struct {
	UUID           uuid.UUID                `json:"uuid"`
	Action         string                   `json:"action"`
	Category       string                   `json:"category,omitempty"`
	ActorID        *uuid.UUID               `json:"actor_id,omitempty"`
	ImpersonatorID *uuid.UUID               `json:"impersonator_id,omitempty"`
	ResourceType   string                   `json:"resource_type,omitempty"`
	ResourceID     *uuid.UUID               `json:"resource_id,omitempty"`
	Result         string                   `json:"result"`
	Status         int                      `json:"status,omitempty"`
	Changes        map[string]domain.Change `json:"changes,omitempty"`
	Metadata       map[string]any           `json:"metadata,omitempty"`
	IP             string                   `json:"ip,omitempty"`
	UserAgent      string                   `json:"user_agent,omitempty"`
	RequestID      string                   `json:"request_id,omitempty"`
	OccurredAt     time.Time                `json:"occurred_at"`
}

// Sink implements ports.Inserter by publishing each batch as one message for app worker.
// When the broker refuses a batch it is inserted through fallback, so an outage costs
// latency rather than audit history.
type Sink struct {
	pub      message.Publisher
	topic    string
	box      Box
	fallback ports.Inserter
	logger   *slog.Logger
}

var _ ports.Inserter = (*Sink)(nil)

// New returns a Sink publishing to topic, sealed by box.
func New(pub message.Publisher, topic string, box Box, fallback ports.Inserter, logger *slog.Logger) *Sink {
	if logger == nil {
		logger = slog.Default()
	}

	return &Sink{pub: pub, topic: topic, box: box, fallback: fallback, logger: logger}
}

// Insert publishes entries, or inserts them directly when publishing fails.
func (s *Sink) Insert(ctx context.Context, entries []domain.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	err := s.publish(ctx, entries)
	if err == nil {
		return nil
	}

	s.logger.WarnContext(ctx, "auditqueue: publish failed, inserting directly", "entries", len(entries), "error", err)

	return s.fallback.Insert(ctx, entries)
}

func (s *Sink) publish(ctx context.Context, entries []domain.Entry) error {
	payload, err := Encode(s.box, entries)
	if err != nil {
		return err
	}

	msg := message.NewMessage(watermill.NewUUID(), payload)
	msg.SetContext(ctx)

	return s.pub.Publish(s.topic, msg)
}

// Encode returns the command payload for entries.
func Encode(box Box, entries []domain.Entry) ([]byte, error) {
	wire := make([]entry, len(entries))
	for i, e := range entries {
		wire[i] = entry(e)
	}

	plaintext, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode audit batch: %w", err)
	}

	ciphertext, err := box.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt audit batch: %w", err)
	}

	return json.Marshal(Command{Ciphertext: ciphertext})
}

// ErrMalformed marks a batch that can never be stored; it is dead-lettered without retries.
var ErrMalformed = fmt.Errorf("malformed audit batch: %w", messaging.ErrPermanent)

// ErrInsertFailed is returned when the insert fails. The database error is only logged: it can
// echo column values such as client IPs, and the poison queue copies handler errors into
// plaintext broker headers.
var ErrInsertFailed = errors.New("insert audit batch failed")

// Decode opens a command payload.
func Decode(box Box, payload []byte) ([]domain.Entry, error) {
	var cmd Command
	if err := json.Unmarshal(payload, &cmd); err != nil || cmd.Ciphertext == "" {
		return nil, fmt.Errorf("%w: payload is not a command", ErrMalformed)
	}

	plaintext, err := box.Decrypt(cmd.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot decrypt", ErrMalformed)
	}

	var wire []entry
	if err := json.Unmarshal(plaintext, &wire); err != nil {
		return nil, fmt.Errorf("%w: sealed batch is invalid", ErrMalformed)
	}

	entries := make([]domain.Entry, len(wire))
	for i, e := range wire {
		if e.UUID == uuid.Nil || e.Action == "" {
			return nil, fmt.Errorf("%w: entry %d has no uuid or action", ErrMalformed, i)
		}

		entries[i] = domain.Entry(e)
	}

	return entries, nil
}

// Handler returns a worker handler that opens a batch and inserts it with repo. repo must
// ignore entries it already stored, since brokers redeliver.
func Handler(box Box, repo ports.Inserter, logger *slog.Logger) message.NoPublishHandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(msg *message.Message) error {
		entries, err := Decode(box, msg.Payload)
		if err != nil {
			return err
		}

		if err := repo.Insert(msg.Context(), entries); err != nil {
			logger.WarnContext(msg.Context(), "auditqueue: insert failed",
				"message_id", msg.UUID, "entries", len(entries), "error", err)

			return ErrInsertFailed
		}

		return nil
	}
}
