// Package event lets core services record domain events without knowing how they are
// delivered. Events are appended to a transactional outbox and relayed to the message
// broker later, so recording must happen inside the business transaction (see Transactor).
package event

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Event is one domain fact. Payload must marshal to a JSON object and must not carry
// secrets or personal data beyond identifiers.
type Event struct {
	ID            uuid.UUID
	Type          string
	SchemaVersion int
	AggregateType string
	AggregateID   uuid.UUID
	ActorID       *uuid.UUID
	OccurredAt    time.Time
	Payload       any
}

// New returns a version 1 event with a fresh ID.
func New(typ, aggregateType string, aggregateID uuid.UUID, actorID *uuid.UUID, at time.Time, payload any) Event {
	return Event{
		ID:            uuid.New(),
		Type:          typ,
		SchemaVersion: 1,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		ActorID:       actorID,
		OccurredAt:    at.UTC(),
		Payload:       payload,
	}
}

// Recorder appends events for later delivery. Called inside Transactor.InTx, the events
// commit or roll back with the business change.
type Recorder interface {
	Record(ctx context.Context, events ...Event) error
}

// Transactor runs fn in one database transaction. Repositories and the Recorder called
// with the ctx passed to fn join that transaction; nested calls reuse it.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Discard is a Recorder that drops events, for deployments without a message broker.
type Discard struct{}

// Record implements Recorder.
func (Discard) Record(context.Context, ...Event) error { return nil }

// NoTx is a Transactor that runs fn directly, for tests and stores without transactions.
type NoTx struct{}

// InTx implements Transactor.
func (NoTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }
