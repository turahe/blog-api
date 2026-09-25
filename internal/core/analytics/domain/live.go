package domain

import (
	"time"

	"github.com/google/uuid"
)

// PermRealtimeRead opens the live admin stream.
const PermRealtimeRead = "analytics.realtime.read"

// Live view windows and limits.
const (
	// LiveActiveWindow is how recently a session must have sent any event to count as active.
	LiveActiveWindow = 5 * time.Minute
	// LiveWindow is the span of the per-minute series and the top pages.
	LiveWindow = 30 * time.Minute
	// LiveRefresh is how often the stream sends a summary and the events since the last one.
	LiveRefresh = 5 * time.Second
	// LiveTopPages is how many pages the summary lists.
	LiveTopPages = 10
	// LiveRecent is the most page views, and the most searches, one refresh sends; busier
	// intervals send the latest.
	LiveRecent = 20
	// LivePathsPerMinute caps the distinct paths counted per minute; the rest count as Other.
	LivePathsPerMinute = 1000
	// LiveMaxSessions caps the sessions tracked as active.
	LiveMaxSessions = 100_000
)

// LiveEvent is an accepted event as the live view sees it: no visitor or subject identity,
// only the session to count active sessions. Path, Country, and Device are set for page views;
// Query and Results for searches. Other kinds only mark their session active.
type LiveEvent struct {
	Kind    Kind
	At      time.Time
	Session uuid.UUID
	Path    string
	Country string
	Device  Device
	Query   string
	Results int
}

// LiveEventOf reduces an accepted event to its live form.
func LiveEventOf(e Event) LiveEvent {
	out := LiveEvent{Kind: e.Kind, At: e.OccurredAt, Session: e.Visitor.SessionID}

	switch {
	case e.PageView != nil:
		out.Path, out.Country, out.Device = e.PageView.Path, e.PageView.Country, e.PageView.Device
	case e.Search != nil:
		out.Query, out.Results = e.Search.Query, e.Search.ResultCount
	}

	return out
}

// LiveMinute is one minute of the live series.
type LiveMinute struct {
	Start    time.Time
	Views    int64
	Searches int64
}

// LiveSnapshot is the live view at At. Recent holds the page views and searches that arrived
// after the caller's cursor, oldest first.
type LiveSnapshot struct {
	At             time.Time
	ActiveSessions int
	SessionsCapped bool
	Series         []LiveMinute
	TopPages       []PathCount
	RecentViews    []LiveEvent
	RecentSearches []LiveEvent
}
