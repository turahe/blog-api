package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestLiveEventOf(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	session, subject := uuid.New(), uuid.New()
	visitor := Visitor{SubjectUUID: &subject, Hash: "hash", SessionID: session}

	tests := []struct {
		name  string
		event Event
		want  LiveEvent
	}{
		{
			name: "page view",
			event: Event{
				Kind: KindPageView, Visitor: visitor, OccurredAt: at,
				PageView: &PageView{Path: "/a", Referrer: "https://ref.example", Country: "ID", Device: DeviceMobile, Browser: "Firefox"},
			},
			want: LiveEvent{Kind: KindPageView, At: at, Session: session, Path: "/a", Country: "ID", Device: DeviceMobile},
		},
		{
			name: "search",
			event: Event{
				Kind: KindSearch, Visitor: visitor, OccurredAt: at,
				Search: &Search{Query: "go", ResultCount: 4, Filters: map[string]string{"tag": "x"}},
			},
			want: LiveEvent{Kind: KindSearch, At: at, Session: session, Query: "go", Results: 4},
		},
		{
			name:  "other kinds only mark the session",
			event: Event{Kind: KindTimeSpent, Visitor: visitor, OccurredAt: at, TimeSpent: &TimeSpent{Path: "/a", FocusSeconds: 9}},
			want:  LiveEvent{Kind: KindTimeSpent, At: at, Session: session},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, LiveEventOf(tt.event))
		})
	}
}
