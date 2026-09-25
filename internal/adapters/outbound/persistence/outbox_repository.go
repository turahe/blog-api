package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/platform/logging"
	"gorm.io/gorm"
)

// Envelope header names, matching the EventEnvelope trait in docs/architecture/asyncapi.yaml.
const (
	HeaderID            = "id"
	HeaderType          = "type"
	HeaderSchemaVersion = "schema_version"
	HeaderSource        = "source"
	HeaderTimestamp     = "timestamp"
	HeaderAggregateType = "aggregate_type"
	HeaderAggregateID   = "aggregate_id"
	HeaderActorID       = "actor_id"
	HeaderCorrelationID = "correlation_id"

	eventSource = "blog-api"
)

// OutboxRecord is one claimed outbox row, ready to publish.
type OutboxRecord struct {
	ID         int64
	UUID       uuid.UUID
	Topic      string
	Payload    []byte
	Headers    map[string]string
	Attempts   int
	OccurredAt time.Time
}

// OutboxOutcome is what happened when a claimed record was published.
type OutboxOutcome struct {
	Err error
	// NextAttemptAt schedules the retry; ignored when Err is nil or Failed is true.
	NextAttemptAt time.Time
	// Failed parks the record after its last attempt.
	Failed bool
}

// OutboxBacklog summarises rows not yet published.
type OutboxBacklog struct {
	Pending         int64
	Failed          int64
	OldestPendingAt *time.Time
}

// OutboxRepository appends domain events to outbox_events and hands them to the relay.
type OutboxRepository struct {
	db *gorm.DB
}

// NewOutboxRepository returns an OutboxRepository over db.
func NewOutboxRepository(db *gorm.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// Record implements event.Recorder. Called inside Transactor.InTx it joins that transaction.
func (r *OutboxRepository) Record(ctx context.Context, events ...event.Event) error {
	for _, e := range events {
		payload, err := json.Marshal(e.Payload)
		if err != nil {
			return fmt.Errorf("encode %s payload: %w", e.Type, err)
		}

		headers, err := json.Marshal(envelopeHeaders(ctx, e))
		if err != nil {
			return fmt.Errorf("encode %s headers: %w", e.Type, err)
		}

		var aggregateID *uuid.UUID
		if e.AggregateID != uuid.Nil {
			aggregateID = &e.AggregateID
		}

		if err := conn(ctx, r.db).Exec(`
			INSERT INTO outbox_events (uuid, topic, aggregate_type, aggregate_id, payload, headers, occurred_at, next_attempt_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.Type, e.AggregateType, aggregateID, string(payload), string(headers), e.OccurredAt, e.OccurredAt,
		).Error; err != nil {
			return fmt.Errorf("append %s to outbox: %w", e.Type, err)
		}
	}

	return nil
}

func envelopeHeaders(ctx context.Context, e event.Event) map[string]string {
	headers := map[string]string{
		HeaderID:            e.ID.String(),
		HeaderType:          e.Type,
		HeaderSchemaVersion: strconv.Itoa(max(e.SchemaVersion, 1)),
		HeaderSource:        eventSource,
		HeaderTimestamp:     e.OccurredAt.UTC().Format(time.RFC3339Nano),
		HeaderAggregateType: e.AggregateType,
	}

	if e.AggregateID != uuid.Nil {
		headers[HeaderAggregateID] = e.AggregateID.String()
	}

	if e.ActorID != nil {
		headers[HeaderActorID] = e.ActorID.String()
	}

	if id := logging.RequestID(ctx); id != "" {
		headers[HeaderCorrelationID] = id
	}

	return headers
}

type outboxRow struct {
	ID         int64
	UUID       uuid.UUID
	Topic      string
	Payload    string
	Headers    string
	Attempts   int
	OccurredAt time.Time
}

// RelayBatch claims up to limit due rows with FOR UPDATE SKIP LOCKED, so concurrent relays
// never publish the same row, calls publish for each in order, and stores every outcome
// before the claim is released. It returns how many rows it claimed.
func (r *OutboxRepository) RelayBatch(
	ctx context.Context, now time.Time, limit int, publish func(OutboxRecord) OutboxOutcome,
) (int, error) {
	claimed := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []outboxRow
		if err := tx.Raw(`
			SELECT id, uuid, topic, payload::text AS payload, headers::text AS headers, attempts, occurred_at
			FROM outbox_events
			WHERE published_at IS NULL AND failed_at IS NULL AND next_attempt_at <= ?
			ORDER BY next_attempt_at, id
			LIMIT ?
			FOR UPDATE SKIP LOCKED`, now, limit).Scan(&rows).Error; err != nil {
			return err
		}

		claimed = len(rows)

		for _, row := range rows {
			record := OutboxRecord{
				ID: row.ID, UUID: row.UUID, Topic: row.Topic, Payload: []byte(row.Payload),
				Attempts: row.Attempts, OccurredAt: row.OccurredAt,
			}
			if err := json.Unmarshal([]byte(row.Headers), &record.Headers); err != nil {
				return fmt.Errorf("decode outbox headers %s: %w", row.UUID, err)
			}

			if err := r.storeOutcome(tx, row.ID, now, publish(record)); err != nil {
				return err
			}
		}

		return nil
	})

	return claimed, err
}

func (r *OutboxRepository) storeOutcome(tx *gorm.DB, id int64, now time.Time, outcome OutboxOutcome) error {
	if outcome.Err == nil {
		return tx.Exec(`UPDATE outbox_events SET published_at = ?, attempts = attempts + 1, last_error = NULL WHERE id = ?`,
			now, id).Error
	}

	var failedAt *time.Time
	if outcome.Failed {
		failedAt = &now
	}

	return tx.Exec(`
		UPDATE outbox_events
		SET attempts = attempts + 1, last_error = ?, next_attempt_at = ?, failed_at = ?
		WHERE id = ?`, lastError(outcome.Err), outcome.NextAttemptAt, failedAt, id).Error
}

func lastError(err error) string {
	const maxRunes = 2000

	runes := []rune(err.Error())
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}

	return string(runes)
}

// PruneBefore deletes rows published before cutoff and returns how many it removed.
// Failed rows are kept for inspection.
func (r *OutboxRepository) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM outbox_events WHERE published_at IS NOT NULL AND published_at < ?`, cutoff)
	return res.RowsAffected, res.Error
}

// Backlog counts unpublished rows and finds the oldest one still due for delivery.
func (r *OutboxRepository) Backlog(ctx context.Context) (OutboxBacklog, error) {
	var row struct {
		Pending         int64
		Failed          int64
		OldestPendingAt *time.Time
	}

	err := r.db.WithContext(ctx).Raw(`
		SELECT count(*) FILTER (WHERE failed_at IS NULL) AS pending,
		       count(*) FILTER (WHERE failed_at IS NOT NULL) AS failed,
		       min(occurred_at) FILTER (WHERE failed_at IS NULL) AS oldest_pending_at
		FROM outbox_events
		WHERE published_at IS NULL`).Scan(&row).Error

	return OutboxBacklog(row), err
}

// Retry makes parked rows due again, for an operator after fixing the cause. It returns
// how many rows it reset.
func (r *OutboxRepository) Retry(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`
		UPDATE outbox_events SET failed_at = NULL, attempts = 0, next_attempt_at = ?
		WHERE published_at IS NULL AND failed_at IS NOT NULL`, now)

	return res.RowsAffected, res.Error
}
