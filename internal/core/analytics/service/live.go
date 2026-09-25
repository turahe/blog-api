package service

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

// liveMinutes is how many one-minute buckets span domain.LiveWindow.
const liveMinutes = int(domain.LiveWindow / time.Minute)

// liveRing is how many recent page views, and searches, the board keeps for new streams.
const liveRing = 5 * domain.LiveRecent

// livePrune is how often Add drops sessions that are no longer active.
const livePrune = 30 * time.Second

// Board is this replica's live view, fed with every replica's accepted events through the
// broker. It keeps the last domain.LiveWindow in one-minute buckets, the last active time of
// each session, and the latest page views and searches.
type Board struct {
	mu        sync.Mutex
	clock     Clock
	minutes   [liveMinutes]liveMinute
	sessions  map[uuid.UUID]time.Time
	capped    bool
	views     []sequenced
	searches  []sequenced
	seq       uint64
	lastPrune time.Time
}

type liveMinute struct {
	start    int64 // Unix minute
	views    int64
	searches int64
	paths    map[string]int64
}

type sequenced struct {
	seq   uint64
	event domain.LiveEvent
}

// NewBoard returns an empty Board.
func NewBoard(clock Clock) *Board {
	return &Board{clock: clock, sessions: map[uuid.UUID]time.Time{}}
}

// Add counts events. Events before the window's first minute are ignored; events from a
// replica whose clock runs ahead count as now.
func (b *Board) Add(events ...domain.LiveEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.clock.Now()
	oldest := now.Unix()/60 - int64(liveMinutes-1)

	for _, e := range events {
		if e.At.After(now) {
			e.At = now
		}

		if e.At.Unix()/60 < oldest {
			continue
		}

		b.touch(e.Session, e.At)

		switch e.Kind {
		case domain.KindPageView:
			b.minute(e.At).addView(e.Path)
			b.seq++
			b.views = keepLast(append(b.views, sequenced{b.seq, e}), liveRing)
		case domain.KindSearch:
			b.minute(e.At).searches++
			b.seq++
			b.searches = keepLast(append(b.searches, sequenced{b.seq, e}), liveRing)
		case domain.KindTimeSpent, domain.KindNavigation, domain.KindSearchClick:
		}
	}

	if now.Sub(b.lastPrune) >= livePrune {
		b.prune(now)
	}
}

// Snapshot returns the live view and the cursor to pass next time; the snapshot's recent
// events are those that arrived after cursor (0 for everything kept).
func (b *Board) Snapshot(cursor uint64) (domain.LiveSnapshot, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.clock.Now()
	b.prune(now)

	out := domain.LiveSnapshot{
		At: now, ActiveSessions: len(b.sessions), SessionsCapped: b.capped,
		Series:         make([]domain.LiveMinute, liveMinutes),
		RecentViews:    since(b.views, cursor),
		RecentSearches: since(b.searches, cursor),
	}

	current := now.Unix() / 60
	paths := map[string]int64{}

	for i := range liveMinutes {
		start := current - int64(liveMinutes-1-i)
		point := domain.LiveMinute{Start: time.Unix(start*60, 0).UTC()}

		if m := &b.minutes[bucket(start)]; m.start == start {
			point.Views, point.Searches = m.views, m.searches
			for path, n := range m.paths {
				paths[path] += n
			}
		}

		out.Series[i] = point
	}

	out.TopPages = topPaths(paths, domain.LiveTopPages)

	return out, b.seq
}

func (b *Board) touch(session uuid.UUID, at time.Time) {
	if last, ok := b.sessions[session]; ok {
		if at.After(last) {
			b.sessions[session] = at
		}

		return
	}

	if len(b.sessions) >= domain.LiveMaxSessions {
		b.capped = true
		return
	}

	b.sessions[session] = at
}

func (b *Board) prune(now time.Time) {
	b.lastPrune = now
	active := now.Add(-domain.LiveActiveWindow)

	for session, last := range b.sessions {
		if last.Before(active) {
			delete(b.sessions, session)
		}
	}

	b.capped = b.capped && len(b.sessions) >= domain.LiveMaxSessions
}

// minute returns the bucket for at, resetting it when it last held an older minute.
func (b *Board) minute(at time.Time) *liveMinute {
	start := at.Unix() / 60

	m := &b.minutes[bucket(start)]
	if m.start != start {
		*m = liveMinute{start: start, paths: map[string]int64{}}
	}

	return m
}

func (m *liveMinute) addView(path string) {
	m.views++

	if _, ok := m.paths[path]; !ok && len(m.paths) >= domain.LivePathsPerMinute {
		path = domain.Other
	}

	m.paths[path]++
}

func bucket(minute int64) int {
	return int(minute % int64(liveMinutes))
}

func keepLast(events []sequenced, n int) []sequenced {
	if len(events) <= n {
		return events
	}

	return slices.Clone(events[len(events)-n:])
}

// since returns the events after cursor, at most domain.LiveRecent of the latest, oldest first.
func since(events []sequenced, cursor uint64) []domain.LiveEvent {
	first := len(events)
	for first > 0 && events[first-1].seq > cursor {
		first--
	}

	first = max(first, len(events)-domain.LiveRecent)

	out := make([]domain.LiveEvent, 0, len(events)-first)
	for _, e := range events[first:] {
		out = append(out, e.event)
	}

	return out
}

func topPaths(counts map[string]int64, n int) []domain.PathCount {
	out := make([]domain.PathCount, 0, len(counts))
	for path, count := range counts {
		out = append(out, domain.PathCount{Path: path, Count: count})
	}

	slices.SortFunc(out, func(a, b domain.PathCount) int {
		if c := cmp.Compare(b.Count, a.Count); c != 0 {
			return c
		}

		return cmp.Compare(a.Path, b.Path)
	})

	return out[:min(n, len(out))]
}
