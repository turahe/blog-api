package auditqueue

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/security/secretbox"
)

type publisher struct {
	topic string
	sent  []*message.Message
	err   error
}

func (p *publisher) Publish(topic string, msgs ...*message.Message) error {
	if p.err != nil {
		return p.err
	}

	p.topic = topic
	p.sent = append(p.sent, msgs...)

	return nil
}

func (p *publisher) Close() error { return nil }

type inserter struct {
	batches [][]domain.Entry
	err     error
}

func (i *inserter) Insert(_ context.Context, entries []domain.Entry) error {
	if i.err != nil {
		return i.err
	}

	i.batches = append(i.batches, entries)

	return nil
}

func newBox(t *testing.T) *secretbox.Box {
	t.Helper()

	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)

	return box
}

func sampleEntries() []domain.Entry {
	actor := uuid.New()

	return []domain.Entry{
		{
			UUID: uuid.New(), Action: "me.profile.update", Category: "profile_update", ActorID: &actor,
			ResourceType: domain.ResourceUser, ResourceID: &actor, Result: domain.ResultSuccess, Status: 200,
			Changes:  map[string]domain.Change{"bio": {From: "old", To: "new"}},
			Metadata: map[string]any{"fields": []any{"bio"}},
			IP:       "203.0.113.7", UserAgent: "curl/8.0", RequestID: "req-1",
			OccurredAt: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC),
		},
		{UUID: uuid.New(), Action: "auth.login", Result: domain.ResultFailure, OccurredAt: time.Date(2026, 9, 25, 10, 0, 1, 0, time.UTC)},
	}
}

func TestSinkPublishesSealedBatchThatHandlerInserts(t *testing.T) {
	t.Parallel()

	box := newBox(t)
	pub := &publisher{}
	fallback := &inserter{}
	entries := sampleEntries()

	require.NoError(t, New(pub, "blog.audit.entries.recorded", box, fallback, nil).Insert(t.Context(), entries))
	require.Empty(t, fallback.batches)
	require.Equal(t, "blog.audit.entries.recorded", pub.topic)
	require.Len(t, pub.sent, 1)
	require.NotContains(t, string(pub.sent[0].Payload), "203.0.113.7")
	require.NotContains(t, string(pub.sent[0].Payload), "me.profile.update")

	repo := &inserter{}
	require.NoError(t, Handler(box, repo, slog.New(slog.DiscardHandler))(pub.sent[0]))
	require.Equal(t, [][]domain.Entry{entries}, repo.batches)
}

func TestSinkInsertsDirectlyWhenBrokerRefuses(t *testing.T) {
	t.Parallel()

	fallback := &inserter{}
	entries := sampleEntries()

	sink := New(&publisher{err: errors.New("broker down")}, "t", newBox(t), fallback, slog.New(slog.DiscardHandler))
	require.NoError(t, sink.Insert(t.Context(), entries))
	require.Equal(t, [][]domain.Entry{entries}, fallback.batches)

	fallback.err = errors.New("db down")

	require.Error(t, sink.Insert(t.Context(), entries), "a batch lost on both paths is reported to the recorder")
}

func TestSinkSkipsEmptyBatch(t *testing.T) {
	t.Parallel()

	pub := &publisher{}
	require.NoError(t, New(pub, "t", newBox(t), &inserter{}, nil).Insert(t.Context(), nil))
	require.Empty(t, pub.sent)
}

func TestHandlerRejectsMalformedBatchesPermanently(t *testing.T) {
	t.Parallel()

	box := newBox(t)
	other, err := secretbox.New([]byte("another-master-key-of-32-bytes!!"))
	require.NoError(t, err)

	foreign, err := Encode(other, sampleEntries())
	require.NoError(t, err)

	noUUID, err := box.Encrypt([]byte(`[{"action":"auth.login"}]`))
	require.NoError(t, err)

	for name, payload := range map[string]string{
		"not json":      `nope`,
		"no ciphertext": `{}`,
		"wrong key":     string(foreign),
		"no uuid":       `{"ciphertext":"` + noUUID + `"}`,
	} {
		err := Handler(box, &inserter{}, nil)(message.NewMessage("m", []byte(payload)))
		require.ErrorIs(t, err, messaging.ErrPermanent, name)
	}
}

func TestHandlerHidesDatabaseErrorAndRetries(t *testing.T) {
	t.Parallel()

	box := newBox(t)
	payload, err := Encode(box, sampleEntries())
	require.NoError(t, err)

	repo := &inserter{err: errors.New(`invalid input syntax for type inet: "203.0.113.7"`)}
	err = Handler(box, repo, slog.New(slog.DiscardHandler))(message.NewMessage("m", payload))
	require.ErrorIs(t, err, ErrInsertFailed)
	require.NotErrorIs(t, err, messaging.ErrPermanent)
	require.NotContains(t, err.Error(), "203.0.113.7")
}
