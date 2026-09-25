package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

var liveNow = time.Date(2026, 9, 25, 10, 30, 20, 0, time.UTC)

func liveView(session uuid.UUID, path string, at time.Time) domain.LiveEvent {
	return domain.LiveEvent{Kind: domain.KindPageView, Session: session, Path: path, At: at, Device: domain.DeviceMobile}
}

func TestBoardCountsTheLiveWindow(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: liveNow}
	board := NewBoard(clock)
	a, b, c, gone := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	board.Add(
		liveView(gone, "/a", liveNow.Add(-6*time.Minute)),
		liveView(a, "/a", liveNow.Add(-time.Minute)),
		liveView(a, "/b", liveNow),
		liveView(b, "/a", liveNow.Add(-10*time.Minute)),
		domain.LiveEvent{Kind: domain.KindSearch, Session: b, Query: "go", Results: 3, At: liveNow.Add(-2 * time.Minute)},
		domain.LiveEvent{Kind: domain.KindTimeSpent, Session: c, At: liveNow.Add(-4 * time.Minute)},
		liveView(uuid.New(), "/old", liveNow.Add(-31*time.Minute)),
		liveView(uuid.New(), "/future", liveNow.Add(time.Hour)),
	)

	snap, cursor := board.Snapshot(0)
	assert.Equal(t, 4, snap.ActiveSessions, "a, b (searched 2 minutes ago), c, and the future view's session")
	require.Len(t, snap.Series, 30)
	assert.Equal(t, liveNow.Truncate(time.Minute), snap.Series[29].Start)
	assert.Equal(t, int64(2), snap.Series[29].Views, "the future view counts as now")
	assert.Equal(t, int64(1), snap.Series[27].Searches)
	assert.Equal(t, []domain.PathCount{{Path: "/a", Count: 3}, {Path: "/b", Count: 1}, {Path: "/future", Count: 1}}, snap.TopPages)
	require.Len(t, snap.RecentViews, 5)
	assert.Equal(t, "/a", snap.RecentViews[0].Path, "oldest first")
	require.Len(t, snap.RecentSearches, 1)

	board.Add(liveView(a, "/c", liveNow))

	snap, _ = board.Snapshot(cursor)
	require.Len(t, snap.RecentViews, 1, "only events after the cursor")
	assert.Equal(t, "/c", snap.RecentViews[0].Path)
	assert.Empty(t, snap.RecentSearches)
}

func TestBoardForgetsMinutesThatLeftTheWindow(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: liveNow}
	board := NewBoard(clock)
	board.Add(liveView(uuid.New(), "/a", liveNow))

	clock.now = liveNow.Add(30 * time.Minute)
	board.Add(liveView(uuid.New(), "/b", clock.now))

	snap, _ := board.Snapshot(0)
	assert.Equal(t, []domain.PathCount{{Path: "/b", Count: 1}}, snap.TopPages, "the reused bucket was reset")
	assert.Equal(t, 1, snap.ActiveSessions)

	var views int64
	for _, m := range snap.Series {
		views += m.Views
	}

	assert.Equal(t, int64(1), views)
}

func TestBoardSendsTheLatestRecentEventsWhenBusy(t *testing.T) {
	t.Parallel()

	board := NewBoard(&fixedClock{now: liveNow})
	session := uuid.New()

	for i := range 3 * domain.LiveRecent {
		board.Add(liveView(session, "/p"+strconv.Itoa(i), liveNow))
	}

	snap, _ := board.Snapshot(0)
	require.Len(t, snap.RecentViews, domain.LiveRecent)
	assert.Equal(t, "/p"+strconv.Itoa(3*domain.LiveRecent-1), snap.RecentViews[domain.LiveRecent-1].Path)
}

func TestBoardFoldsPathsPastTheMinuteCap(t *testing.T) {
	t.Parallel()

	board := NewBoard(&fixedClock{now: liveNow})
	session := uuid.New()

	for i := range domain.LivePathsPerMinute + 5 {
		board.Add(liveView(session, "/p"+strconv.Itoa(i), liveNow))
	}

	snap, _ := board.Snapshot(0)
	assert.Equal(t, domain.PathCount{Path: domain.Other, Count: 5}, snap.TopPages[0])
}

func TestBoardCapsTrackedSessions(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: liveNow}
	board := NewBoard(clock)

	events := make([]domain.LiveEvent, 0, domain.LiveMaxSessions+1)
	for range domain.LiveMaxSessions + 1 {
		events = append(events, domain.LiveEvent{Kind: domain.KindNavigation, Session: uuid.New(), At: liveNow})
	}

	board.Add(events...)

	snap, _ := board.Snapshot(0)
	assert.Equal(t, domain.LiveMaxSessions, snap.ActiveSessions)
	assert.True(t, snap.SessionsCapped)

	clock.now = liveNow.Add(domain.LiveActiveWindow + time.Second)
	snap, _ = board.Snapshot(0)
	assert.Zero(t, snap.ActiveSessions)
	assert.False(t, snap.SessionsCapped)
}

type liveSink struct{ events []domain.LiveEvent }

func (s *liveSink) Publish(e domain.LiveEvent) { s.events = append(s.events, e) }

func TestIngestAnnouncesAcceptedEventsLive(t *testing.T) {
	t.Parallel()

	ingest, _, _, _ := newTestIngest(t)
	live := &liveSink{}
	ingest.WithLive(live)

	session := uuid.New()

	_, err := ingest.PageView(t.Context(), Meta{UserAgent: browserUA, Country: "de"}, PageViewInput{SessionID: session, Path: "/a"})
	require.NoError(t, err)
	_, err = ingest.Search(t.Context(), Meta{UserAgent: browserUA}, SearchInput{SessionID: session, Query: "  Go  ", ResultCount: 2})
	require.NoError(t, err)
	_, err = ingest.PageView(t.Context(), Meta{UserAgent: "Googlebot/2.1"}, PageViewInput{SessionID: session, Path: "/a"})
	require.NoError(t, err)

	require.Len(t, live.events, 2, "bots are not announced")
	assert.Equal(t, domain.LiveEvent{
		Kind: domain.KindPageView, At: live.events[0].At, Session: session, Path: "/a", Country: "DE", Device: domain.DeviceDesktop,
	}, live.events[0])
	assert.Equal(t, "go", live.events[1].Query)
	assert.Equal(t, 2, live.events[1].Results)
}
