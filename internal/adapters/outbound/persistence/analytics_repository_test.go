package persistence

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

func analyticsVisitor(subject *uuid.UUID) analyticsdomain.Visitor {
	return analyticsdomain.Visitor{SubjectUUID: subject, Hash: strings.Repeat("a", 64), SessionID: uuid.New()}
}

func countWhere(t *testing.T, tx *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()

	var n int64
	require.NoError(t, tx.Table(table).Where(where, args...).Count(&n).Error)

	return n
}

func TestAnalyticsRepositoryInsertsEveryKindOnce(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAnalyticsRepository(tx)
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	visitor := analyticsVisitor(nil)
	search := uuid.New()

	events := []analyticsdomain.Event{
		{Kind: analyticsdomain.KindPageView, UUID: uuid.New(), Visitor: visitor, OccurredAt: at, PageView: &analyticsdomain.PageView{
			Path: "/posts/a", Country: "DE", Device: analyticsdomain.DeviceMobile, Browser: "safari",
		}},
		{Kind: analyticsdomain.KindNavigation, UUID: uuid.New(), Visitor: visitor, OccurredAt: at, Navigation: &analyticsdomain.Navigation{
			To: "/posts/a", Transition: analyticsdomain.TransitionDirect,
		}},
		{Kind: analyticsdomain.KindSearch, UUID: search, Visitor: visitor, OccurredAt: at, Search: &analyticsdomain.Search{
			Query: "go", ResultCount: 3, Filters: map[string]string{"tag": "go"},
		}},
		{Kind: analyticsdomain.KindSearchClick, UUID: uuid.New(), Visitor: visitor, OccurredAt: at, SearchClick: &analyticsdomain.SearchClick{
			SearchUUID: search, Position: 1, ResourceType: analyticsdomain.ResourcePost, ResourceUUID: uuid.New(),
		}},
	}

	require.NoError(t, repo.InsertBatch(t.Context(), events))
	require.NoError(t, repo.InsertBatch(t.Context(), events), "a retried batch is ignored, not an error")

	for _, table := range []string{"analytics_page_views", "analytics_navigation", "analytics_searches", "analytics_search_clicks"} {
		assert.Equal(t, int64(1), countWhere(t, tx, table, "visitor_hash = ? AND session_id = ?", visitor.Hash, visitor.SessionID), table)
	}

	var row struct {
		Referrer    *string
		CountryCode string
		DeviceType  string
	}
	require.NoError(t, tx.Raw(`SELECT referrer, country_code, device_type FROM analytics_page_views WHERE uuid = ?`, events[0].UUID).
		Scan(&row).Error)
	assert.Nil(t, row.Referrer)
	assert.Equal(t, "DE", row.CountryCode)
	assert.Equal(t, "mobile", row.DeviceType)

	var filters string
	require.NoError(t, tx.Raw(`SELECT filters->>'tag' FROM analytics_searches WHERE uuid = ?`, search).Scan(&filters).Error)
	assert.Equal(t, "go", filters)
}

func TestAnalyticsRepositoryKeepsTheLongestHeartbeat(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAnalyticsRepository(tx)
	view := uuid.New()
	visitor := analyticsVisitor(nil)
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	beat := func(focus int, at time.Time) analyticsdomain.Event {
		return analyticsdomain.Event{
			Kind: analyticsdomain.KindTimeSpent, UUID: view, Visitor: visitor, OccurredAt: at,
			TimeSpent: &analyticsdomain.TimeSpent{Path: "/posts/a", FocusSeconds: focus},
		}
	}

	require.NoError(t, repo.InsertBatch(t.Context(), []analyticsdomain.Event{
		beat(15, start), beat(30, start.Add(15*time.Second)),
	}), "heartbeats for one view in one batch fold into one row")
	require.NoError(t, repo.InsertBatch(t.Context(), []analyticsdomain.Event{beat(20, start.Add(time.Minute))}),
		"a late, smaller heartbeat does not lower the focus time")

	var row struct {
		FocusSeconds int
		StartedAt    time.Time
		LastSeenAt   time.Time
	}
	require.NoError(t, tx.Raw(`SELECT focus_seconds, started_at, last_seen_at FROM analytics_time_spent WHERE uuid = ?`, view).
		Scan(&row).Error)
	assert.Equal(t, 30, row.FocusSeconds)
	assert.True(t, row.StartedAt.Equal(start))
	assert.True(t, row.LastSeenAt.Equal(start.Add(time.Minute)))
}

func TestAccountErasureDeletesTheSubjectsAnalyticsEvents(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	consent := consentservice.New(NewConsentRepository(tx), system.UUIDGenerator{}, system.Clock{})
	user := insertUser(t, tx)

	linked, err := consent.Store(ctx, "", &user, map[consentdomain.Purpose]bool{
		consentdomain.PurposeAnalytics: true, consentdomain.PurposeAuthenticatedAnalytics: true,
	}, "2026-09")
	require.NoError(t, err)

	other, err := consent.Store(ctx, "", nil, map[consentdomain.Purpose]bool{consentdomain.PurposeAnalytics: true}, "2026-09")
	require.NoError(t, err)

	repo := NewAnalyticsRepository(tx)
	pageView := func(subject uuid.UUID) analyticsdomain.Event {
		return analyticsdomain.Event{
			Kind: analyticsdomain.KindPageView, UUID: uuid.New(), Visitor: analyticsVisitor(&subject), OccurredAt: time.Now(),
			PageView: &analyticsdomain.PageView{Path: "/", Device: analyticsdomain.DeviceDesktop, Browser: "chrome"},
		}
	}
	require.NoError(t, repo.InsertBatch(ctx, []analyticsdomain.Event{pageView(linked.Subject.UUID), pageView(other.Subject.UUID)}))

	removed, err := consent.DeleteForUser(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	assert.Zero(t, countWhere(t, tx, "analytics_page_views", "subject_uuid = ?", linked.Subject.UUID))
	assert.Equal(t, int64(1), countWhere(t, tx, "analytics_page_views", "subject_uuid = ?", other.Subject.UUID))
}
