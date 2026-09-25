package service

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/140.0 Safari/537.36"

type memSink struct {
	mu     sync.Mutex
	events []domain.Event
}

func (s *memSink) Enqueue(e domain.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, e)

	return true
}

func (s *memSink) all() []domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]domain.Event(nil), s.events...)
}

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type seqIDs struct{ id uuid.UUID }

func (s seqIDs) New() uuid.UUID { return s.id }

type prefixHasher struct{}

// MAC returns a 64-character stand-in that exposes what was hashed, for assertions.
func (prefixHasher) MAC(value string) string {
	out := []byte(value)
	for len(out) < 64 {
		out = append(out, '.')
	}

	return string(out[:64])
}

func newTestIngest(t *testing.T) (*Ingest, *memSink, *fixedClock, uuid.UUID) {
	t.Helper()

	sink := &memSink{}
	clock := &fixedClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	generated := uuid.New()

	return NewIngest(sink, prefixHasher{}, seqIDs{id: generated}, clock), sink, clock, generated
}

func TestPageViewNormalisesAndQueues(t *testing.T) {
	t.Parallel()

	ingest, sink, clock, generated := newTestIngest(t)
	session := uuid.New()

	id, err := ingest.PageView(t.Context(), Meta{IP: "203.0.113.9", UserAgent: browserUA, Country: "de"}, PageViewInput{
		SessionID: session, Path: "/posts/hello/?utm_source=x", Referrer: "https://news.example/item?id=1",
	})
	require.NoError(t, err)
	assert.Equal(t, generated, id, "the server picks an id when the client sends none")

	events := sink.all()
	require.Len(t, events, 1)
	e := events[0]
	assert.Equal(t, domain.KindPageView, e.Kind)
	assert.Equal(t, id, e.UUID)
	assert.Equal(t, clock.now, e.OccurredAt, "the server clock dates events, not the client")
	assert.Equal(t, session, e.Visitor.SessionID)
	assert.Nil(t, e.Visitor.SubjectUUID)
	assert.Equal(t, &domain.PageView{
		Path: "/posts/hello", Referrer: "https://news.example/item", Country: "DE", Device: domain.DeviceDesktop, Browser: "chrome",
	}, e.PageView)
}

func TestVisitorHashes(t *testing.T) {
	t.Parallel()

	ingest, sink, clock, _ := newTestIngest(t)
	subject := uuid.New()
	in := PageViewInput{SessionID: uuid.New(), Path: "/"}

	_, err := ingest.PageView(t.Context(), Meta{Subject: &subject, IP: "203.0.113.9", UserAgent: browserUA}, in)
	require.NoError(t, err)
	_, err = ingest.PageView(t.Context(), Meta{IP: "203.0.113.9", UserAgent: browserUA}, in)
	require.NoError(t, err)

	clock.now = clock.now.Add(24 * time.Hour)
	_, err = ingest.PageView(t.Context(), Meta{IP: "203.0.113.9", UserAgent: browserUA}, in)
	require.NoError(t, err)

	events := sink.all()
	require.Len(t, events, 3)
	assert.Equal(t, &subject, events[0].Visitor.SubjectUUID)
	assert.Contains(t, events[0].Visitor.Hash, "analytics:subject:")
	assert.Contains(t, events[1].Visitor.Hash, "analytics:visitor:", "keyed with the day's salt, not the date")
	assert.NotEqual(t, events[1].Visitor.Hash, events[2].Visitor.Hash, "anonymous visitors cannot be linked across days")

	for _, e := range events {
		assert.Len(t, e.Visitor.Hash, 64)
	}
}

func TestDroppedEventsAnswerLikeKeptOnes(t *testing.T) {
	t.Parallel()

	ingest, sink, _, _ := newTestIngest(t)
	clientID := uuid.New()
	in := PageViewInput{ID: clientID, SessionID: uuid.New(), Path: "/"}

	for name, meta := range map[string]Meta{
		"refused":  {Refused: true, UserAgent: browserUA},
		"prefetch": {Prefetch: true, UserAgent: browserUA},
		"bot":      {UserAgent: "Googlebot/2.1"},
		"no agent": {},
	} {
		id, err := ingest.PageView(t.Context(), meta, in)
		require.NoError(t, err, name)
		assert.Equal(t, clientID, id, name)
	}

	assert.Empty(t, sink.all())
}

func TestIngestValidation(t *testing.T) {
	t.Parallel()

	ingest, sink, _, _ := newTestIngest(t)
	meta := Meta{UserAgent: browserUA}
	session := uuid.New()
	ctx := t.Context()

	errs := map[string]error{}
	_, errs["no session"] = ingest.PageView(ctx, meta, PageViewInput{Path: "/"})
	_, errs["bad path"] = ingest.PageView(ctx, meta, PageViewInput{SessionID: session, Path: "x"})
	_, errs["no view id"] = ingest.TimeSpent(ctx, meta, TimeSpentInput{SessionID: session, Path: "/"})
	_, errs["bad transition"] = ingest.Navigation(ctx, meta, NavigationInput{SessionID: session, To: "/", Transition: "warp"})
	_, errs["bad from"] = ingest.Navigation(ctx, meta, NavigationInput{SessionID: session, From: "x", To: "/", Transition: "internal"})
	_, errs["empty query"] = ingest.Search(ctx, meta, SearchInput{SessionID: session, Query: " "})
	_, errs["negative results"] = ingest.Search(ctx, meta, SearchInput{SessionID: session, Query: "go", ResultCount: -1})
	_, errs["unknown filter"] = ingest.Search(ctx, meta, SearchInput{SessionID: session, Query: "go", Filters: map[string]string{"x": "y"}})
	_, errs["no search id"] = ingest.SearchClick(ctx, meta, SearchClickInput{
		SessionID: session, Position: 1, ResourceType: "post", ResourceID: uuid.New(),
	})
	_, errs["position zero"] = ingest.SearchClick(ctx, meta, SearchClickInput{
		SessionID: session, SearchID: uuid.New(), ResourceType: "post", ResourceID: uuid.New(),
	})
	_, errs["bad resource"] = ingest.SearchClick(ctx, meta, SearchClickInput{
		SessionID: session, SearchID: uuid.New(), Position: 1, ResourceType: "user", ResourceID: uuid.New(),
	})

	for name, err := range errs {
		require.ErrorIs(t, err, domain.ErrValidation, name)
	}

	assert.Empty(t, sink.all())
}

func TestOtherKindsAreNormalised(t *testing.T) {
	t.Parallel()

	ingest, sink, _, _ := newTestIngest(t)
	meta := Meta{UserAgent: browserUA}
	session, view, search, post := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ctx := t.Context()

	_, err := ingest.TimeSpent(ctx, meta, TimeSpentInput{ViewID: view, SessionID: session, Path: "/a/", FocusSeconds: 99_999})
	require.NoError(t, err)
	_, err = ingest.Navigation(ctx, meta, NavigationInput{SessionID: session, To: "/b", Transition: "direct"})
	require.NoError(t, err)
	id, err := ingest.Search(ctx, meta, SearchInput{ID: search, SessionID: session, Query: " Go  Lang ", ResultCount: 5_000_000})
	require.NoError(t, err)
	assert.Equal(t, search, id)
	_, err = ingest.SearchClick(ctx, meta, SearchClickInput{
		SessionID: session, SearchID: search, Position: 5000, ResourceType: "post", ResourceID: post,
	})
	require.NoError(t, err)

	events := sink.all()
	require.Len(t, events, 4)
	assert.Equal(t, view, events[0].UUID, "a heartbeat is keyed by its page view")
	assert.Equal(t, &domain.TimeSpent{Path: "/a", FocusSeconds: domain.MaxFocusSeconds}, events[0].TimeSpent)
	assert.Equal(t, &domain.Navigation{To: "/b", Transition: domain.TransitionDirect}, events[1].Navigation)
	assert.Equal(t, &domain.Search{Query: "go lang", ResultCount: domain.MaxResultCount, Filters: map[string]string{}}, events[2].Search)
	assert.Equal(t, &domain.SearchClick{
		SearchUUID: search, Position: domain.MaxPosition, ResourceType: domain.ResourcePost, ResourceUUID: post,
	}, events[3].SearchClick)
}

func TestProcessHasherIsKeyedPerProcess(t *testing.T) {
	t.Parallel()

	a, b := newProcessHasher(), newProcessHasher()
	first := a.MAC("x")
	assert.Len(t, first, 64)
	assert.Equal(t, first, a.MAC("x"))
	assert.NotEqual(t, first, b.MAC("x"))
}
