package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/platform/logging"
	"gorm.io/gorm"
)

func outboxCount(t *testing.T, tx *gorm.DB, where string, args ...any) int64 {
	t.Helper()

	var n int64
	require.NoError(t, tx.Raw("SELECT count(*) FROM outbox_events WHERE "+where, args...).Scan(&n).Error)

	return n
}

func TestTransactorCommitsAndRollsBackOutboxWithBusinessWrite(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	transactor := NewTransactor(tx)
	outbox := NewOutboxRepository(tx)
	users := NewUserRepository(tx)
	author := insertUser(t, tx)

	failed := errors.New("business rule failed")
	rolledBack := event.New(event.PostCreated, event.AggregatePost, uuid.New(), &author, time.Now(), map[string]string{"k": "v"})

	err := transactor.InTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, outbox.Record(ctx, rolledBack))

		require.NoError(t, users.RecordLogin(ctx, author, time.Now()))

		return failed
	})
	require.ErrorIs(t, err, failed)
	require.Zero(t, outboxCount(t, tx, "uuid = ?", rolledBack.ID))

	user, err := users.FindByID(t.Context(), author)
	require.NoError(t, err)
	require.Nil(t, user.LastLoginAt, "business write rolled back with the event")

	committed := event.New(event.PostCreated, event.AggregatePost, uuid.New(), nil, time.Now(), map[string]string{})

	require.NoError(t, transactor.InTx(t.Context(), func(ctx context.Context) error {
		return transactor.InTx(ctx, func(ctx context.Context) error {
			return outbox.Record(ctx, committed)
		})
	}))
	require.EqualValues(t, 1, outboxCount(t, tx, "uuid = ?", committed.ID))
}

func TestOutboxRecordWritesEnvelopeHeaders(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewOutboxRepository(tx)
	actor, aggregate := uuid.New(), uuid.New()
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	e := event.New(event.PostPublished, event.AggregatePost, aggregate, &actor, at, map[string]any{"title": "Hello"})
	ctx := logging.WithRequestID(t.Context(), "req-123")
	require.NoError(t, repo.Record(ctx, e))

	var row struct {
		Topic         string
		AggregateType string
		AggregateID   uuid.UUID
		Payload       string
		Headers       string
	}
	require.NoError(t, tx.Raw(`SELECT topic, aggregate_type, aggregate_id, payload::text AS payload, headers::text AS headers
		FROM outbox_events WHERE uuid = ?`, e.ID).Scan(&row).Error)

	require.Equal(t, event.PostPublished, row.Topic)
	require.Equal(t, event.AggregatePost, row.AggregateType)
	require.Equal(t, aggregate, row.AggregateID)
	require.JSONEq(t, `{"title":"Hello"}`, row.Payload)

	var headers map[string]string
	require.NoError(t, json.Unmarshal([]byte(row.Headers), &headers))
	require.Equal(t, map[string]string{
		HeaderID:            e.ID.String(),
		HeaderType:          event.PostPublished,
		HeaderSchemaVersion: "1",
		HeaderSource:        "blog-api",
		HeaderTimestamp:     "2026-09-25T10:00:00Z",
		HeaderAggregateType: event.AggregatePost,
		HeaderAggregateID:   aggregate.String(),
		HeaderActorID:       actor.String(),
		HeaderCorrelationID: "req-123",
	}, headers)
}

func TestOutboxRelayBatchOutcomes(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewOutboxRepository(tx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	ok := event.New(event.PostCreated, event.AggregatePost, uuid.New(), nil, now.Add(-3*time.Second), map[string]int{"n": 1})
	retry := event.New(event.PostCreated, event.AggregatePost, uuid.New(), nil, now.Add(-2*time.Second), map[string]int{"n": 2})
	parked := event.New(event.PostCreated, event.AggregatePost, uuid.New(), nil, now.Add(-time.Second), map[string]int{"n": 3})
	future := event.New(event.PostCreated, event.AggregatePost, uuid.New(), nil, now.Add(time.Hour), map[string]int{"n": 4})
	require.NoError(t, repo.Record(t.Context(), ok, retry, parked, future))

	var seen []uuid.UUID

	claimed, err := repo.RelayBatch(t.Context(), now, 10, func(rec OutboxRecord) OutboxOutcome {
		seen = append(seen, rec.UUID)
		require.Equal(t, rec.UUID.String(), rec.Headers[HeaderID])

		switch rec.UUID {
		case retry.ID:
			return OutboxOutcome{Err: errors.New("broker down"), NextAttemptAt: now.Add(time.Minute)}
		case parked.ID:
			return OutboxOutcome{Err: errors.New("rejected"), Failed: true}
		default:
			return OutboxOutcome{}
		}
	})
	require.NoError(t, err)
	require.Equal(t, 3, claimed)
	require.Equal(t, []uuid.UUID{ok.ID, retry.ID, parked.ID}, seen, "due rows in order; future row skipped")

	require.EqualValues(t, 1, outboxCount(t, tx, "uuid = ? AND published_at IS NOT NULL AND attempts = 1", ok.ID))
	require.EqualValues(t, 1, outboxCount(t, tx,
		"uuid = ? AND published_at IS NULL AND failed_at IS NULL AND attempts = 1 AND last_error = 'broker down' AND next_attempt_at = ?",
		retry.ID, now.Add(time.Minute)))
	require.EqualValues(t, 1, outboxCount(t, tx, "uuid = ? AND failed_at IS NOT NULL AND last_error = 'rejected'", parked.ID))

	claimed, err = repo.RelayBatch(t.Context(), now, 10, func(OutboxRecord) OutboxOutcome {
		t.Fatal("nothing is due")
		return OutboxOutcome{}
	})
	require.NoError(t, err)
	require.Zero(t, claimed)

	backlog, err := repo.Backlog(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 2, backlog.Pending, "retry and future")
	require.EqualValues(t, 1, backlog.Failed)
	require.NotNil(t, backlog.OldestPendingAt)
	require.WithinDuration(t, retry.OccurredAt, *backlog.OldestPendingAt, time.Millisecond)

	reset, err := repo.Retry(t.Context(), now)
	require.NoError(t, err)
	require.EqualValues(t, 1, reset)
	require.EqualValues(t, 1, outboxCount(t, tx, "uuid = ? AND failed_at IS NULL AND attempts = 0", parked.ID))

	pruned, err := repo.PruneBefore(t.Context(), now.Add(time.Second))
	require.NoError(t, err)
	require.EqualValues(t, 1, pruned)
	require.Zero(t, outboxCount(t, tx, "uuid = ?", ok.ID))
}
