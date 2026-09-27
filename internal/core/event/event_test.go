package event_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
)

type txKey struct{}

// fakeTx marks the ctx it hands to fn so callers can tell fn ran inside it.
type fakeTx struct {
	err error
}

func (f fakeTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if f.err != nil {
		return f.err
	}

	return fn(context.WithValue(ctx, txKey{}, true))
}

func TestNew(t *testing.T) {
	t.Parallel()

	aggregate, actor := uuid.New(), uuid.New()
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600))

	e := event.New(event.PostCreated, "post", aggregate, &actor, at, map[string]string{"slug": "hello"})

	require.NotEqual(t, uuid.Nil, e.ID)
	require.Equal(t, event.PostCreated, e.Type)
	require.Equal(t, 1, e.SchemaVersion)
	require.Equal(t, "post", e.AggregateType)
	require.Equal(t, aggregate, e.AggregateID)
	require.Equal(t, &actor, e.ActorID)
	require.Equal(t, time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC), e.OccurredAt)
	require.Equal(t, map[string]string{"slug": "hello"}, e.Payload)

	require.NotEqual(t, e.ID, event.New(event.PostCreated, "post", aggregate, nil, at, nil).ID)
}

func TestUnitInTx(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name     string
		tx       event.Transactor
		fnErr    error
		wantErr  error
		wantRan  bool
		wantInTx bool
	}{
		{name: "zero value runs fn directly", wantRan: true},
		{name: "zero value returns fn error", fnErr: boom, wantErr: boom, wantRan: true},
		{name: "runs fn inside the transaction", tx: fakeTx{}, wantRan: true, wantInTx: true},
		{name: "returns fn error from the transaction", tx: fakeTx{}, fnErr: boom, wantErr: boom, wantRan: true, wantInTx: true},
		{name: "transaction failure skips fn", tx: fakeTx{err: boom}, wantErr: boom},
		{name: "NoTx runs fn directly", tx: event.NoTx{}, wantRan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var ran, inTx bool

			err := event.Unit{Tx: tt.tx}.InTx(t.Context(), func(ctx context.Context) error {
				ran = true
				inTx, _ = ctx.Value(txKey{}).(bool)

				return tt.fnErr
			})

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantRan, ran)
			require.Equal(t, tt.wantInTx, inTx)
		})
	}
}

func TestUnitRecord(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	one := event.New(event.PostCreated, "post", uuid.New(), nil, time.Now(), nil)
	two := event.New(event.PostPublished, "post", uuid.New(), nil, time.Now(), nil)

	tests := []struct {
		name     string
		recorder *eventtest.Recorder
		events   []event.Event
		wantErr  error
		want     []string
	}{
		{name: "forwards events in order", recorder: &eventtest.Recorder{}, events: []event.Event{one, two}, want: []string{event.PostCreated, event.PostPublished}},
		{name: "skips the recorder without events", recorder: &eventtest.Recorder{Err: boom}, want: []string{}},
		{name: "returns the recorder error", recorder: &eventtest.Recorder{Err: boom}, events: []event.Event{one}, wantErr: boom, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := event.Unit{Recorder: tt.recorder}.Record(t.Context(), tt.events...)

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, tt.recorder.Types())
		})
	}
}

func TestUnitRecordWithoutRecorderDropsEvents(t *testing.T) {
	t.Parallel()

	e := event.New(event.PostCreated, "post", uuid.New(), nil, time.Now(), nil)

	require.NoError(t, event.Unit{}.Record(t.Context(), e))
	require.NoError(t, event.Unit{Recorder: event.Discard{}}.Record(t.Context(), e))
}
