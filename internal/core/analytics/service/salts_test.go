package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memSaltStore struct {
	mu        sync.Mutex
	salts     map[string][]byte
	keepFroms []string
	fail      bool
	calls     int
}

func (m *memSaltStore) DailySalt(_ context.Context, day string, fresh []byte, keepFrom string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	if m.fail {
		return nil, errors.New("database down")
	}

	m.keepFroms = append(m.keepFroms, keepFrom)

	if m.salts == nil {
		m.salts = map[string][]byte{}
	}

	if _, ok := m.salts[day]; !ok {
		m.salts[day] = fresh
	}

	return m.salts[day], nil
}

func anonymousHash(t *testing.T, ingest *Ingest, sink *memSink) string {
	t.Helper()

	_, err := ingest.PageView(t.Context(), Meta{IP: "203.0.113.9", UserAgent: browserUA}, PageViewInput{SessionID: uuid.New(), Path: "/"})
	require.NoError(t, err)

	events := sink.all()

	return events[len(events)-1].Visitor.Hash
}

func TestReplicasSharingSaltsHashVisitorsAlike(t *testing.T) {
	t.Parallel()

	store := &memSaltStore{}
	hasher := newProcessHasher()
	clock := &fixedClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	sinkA, sinkB := &memSink{}, &memSink{}
	a := NewIngest(sinkA, hasher, seqIDs{id: uuid.New()}, clock).WithSalts(store, nil)
	b := NewIngest(sinkB, hasher, seqIDs{id: uuid.New()}, clock).WithSalts(store, nil)

	today := anonymousHash(t, a, sinkA)
	assert.Equal(t, today, anonymousHash(t, b, sinkB), "one visitor, one hash across replicas")
	assert.Equal(t, today, anonymousHash(t, a, sinkA))
	assert.Equal(t, 2, store.calls, "each replica loads the day's salt once")
	assert.Equal(t, []string{"2026-09-24", "2026-09-24"}, store.keepFroms)

	clock.now = clock.now.Add(24 * time.Hour)

	assert.NotEqual(t, today, anonymousHash(t, a, sinkA), "a new day, a new salt")
	assert.Len(t, store.salts, 2)
	assert.False(t, bytes.Equal(store.salts["2026-09-25"], store.salts["2026-09-26"]))
}

func TestReplicasWithoutSharedSaltsDisagree(t *testing.T) {
	t.Parallel()

	hasher := newProcessHasher()
	clock := &fixedClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	sinkA, sinkB := &memSink{}, &memSink{}
	a := NewIngest(sinkA, hasher, seqIDs{id: uuid.New()}, clock)
	b := NewIngest(sinkB, hasher, seqIDs{id: uuid.New()}, clock)

	assert.NotEqual(t, anonymousHash(t, a, sinkA), anonymousHash(t, b, sinkB))
}

func TestSaltStoreOutageFallsBackThenRecovers(t *testing.T) {
	t.Parallel()

	store := &memSaltStore{fail: true}
	sink := &memSink{}
	clock := &fixedClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}

	var logs bytes.Buffer

	ingest := NewIngest(sink, newProcessHasher(), seqIDs{id: uuid.New()}, clock).
		WithSalts(store, slog.New(slog.NewTextHandler(&logs, nil)))

	local := anonymousHash(t, ingest, sink)
	assert.Equal(t, local, anonymousHash(t, ingest, sink), "the stand-in salt holds until the retry")
	assert.Equal(t, 1, store.calls, "a failing store is not asked on every event")
	assert.Contains(t, logs.String(), "analytics visitor salt unavailable")

	store.fail = false
	clock.now = clock.now.Add(saltRetry)

	shared := anonymousHash(t, ingest, sink)
	assert.NotEqual(t, local, shared, "the shared salt replaces the stand-in once it loads")
	assert.Equal(t, shared, anonymousHash(t, ingest, sink))
	assert.Equal(t, 2, store.calls)
}
