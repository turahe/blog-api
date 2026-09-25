package bootstrap_test

import (
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/140.0 Safari/537.36"

type analyticsStack struct {
	*authStack

	writer *analyticsservice.Writer
}

func newAnalyticsStack(t *testing.T) *analyticsStack {
	t.Helper()

	var writer *analyticsservice.Writer

	s := newAuthStackWith(t, func(tx *gorm.DB, deps *httpadapter.Dependencies) {
		deps.Settings = settingsservice.New(persistence.NewSettingsRepository(tx), settingsdomain.DefaultCatalogue(), uuidGen{}, system.Clock{})
		deps.Consent = consentservice.New(persistence.NewConsentRepository(tx), uuidGen{}, system.Clock{})
		// One flush at Close: the transaction must not be used by the writer while requests run.
		writer = analyticsservice.NewWriter(persistence.NewAnalyticsRepository(tx), slog.New(slog.DiscardHandler),
			analyticsservice.WriterOptions{BatchSize: 1000, FlushInterval: time.Hour})
		deps.AnalyticsIngest = analyticsservice.NewIngest(writer, nil, uuidGen{}, system.Clock{})
	})

	t.Cleanup(func() { _ = writer.Close(t.Context()) })

	return &analyticsStack{authStack: s, writer: writer}
}

func (s *analyticsStack) send(t *testing.T, path, ua string, body any, headers map[string]string) reply {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ua)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	var envelope struct {
		Data  map[string]any `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())

	return reply{status: w.Code, data: envelope.Data, code: envelope.Error.Code}
}

func (s *analyticsStack) consentToken(t *testing.T, granted bool) string {
	t.Helper()

	r := s.send(t, "/api/v1/analytics/consent", browserUA, map[string]any{
		"purposes": map[string]bool{"analytics": granted}, "policy_version": "2026-09",
	}, nil)
	require.Equal(t, nethttp.StatusCreated, r.status, r.code)

	token, _ := r.data["token"].(string)
	require.NotEmpty(t, token)

	return token
}

func (s *analyticsStack) rows(t *testing.T, table, session string) int64 {
	t.Helper()

	var n int64
	require.NoError(t, s.tx.Table(table).Where("session_id = ?", session).Count(&n).Error)

	return n
}

func TestAnalyticsIngestHonoursConsent(t *testing.T) {
	t.Parallel()

	s := newAnalyticsStack(t)
	session := uuid.NewString()
	view := map[string]any{"session_id": session, "path": "/posts/hello?token=secret"}
	page := "/api/v1/analytics/ingest/page-view"

	r := s.send(t, page, browserUA, view, nil)
	assert.Equal(t, nethttp.StatusForbidden, r.status, "consent is required by default")
	assert.Equal(t, "analytics.consent_required", r.code)

	granted := s.consentToken(t, true)
	r = s.send(t, page, browserUA, view, map[string]string{"X-Consent-Token": granted})
	require.Equal(t, nethttp.StatusAccepted, r.status, r.code)
	viewID, _ := r.data["id"].(string)
	require.NotEmpty(t, viewID)

	r = s.send(t, "/api/v1/analytics/ingest/time-spent", browserUA,
		map[string]any{"view_id": viewID, "session_id": session, "path": "/posts/hello", "focus_seconds": 20},
		map[string]string{"X-Consent-Token": granted})
	require.Equal(t, nethttp.StatusAccepted, r.status, r.code)

	r = s.send(t, "/api/v1/analytics/ingest/search", browserUA,
		map[string]any{"session_id": session, "query": "Hello", "result_count": 2}, map[string]string{"X-Consent-Token": granted})
	require.Equal(t, nethttp.StatusAccepted, r.status, r.code)
	searchID, _ := r.data["id"].(string)

	r = s.send(t, "/api/v1/analytics/ingest/search-click", browserUA, map[string]any{
		"session_id": session, "search_id": searchID, "position": 1, "resource_type": "post", "resource_id": uuid.NewString(),
	}, map[string]string{"X-Consent-Token": granted})
	require.Equal(t, nethttp.StatusAccepted, r.status, r.code)

	require.NoError(t, s.tx.Exec(`INSERT INTO settings (key, value) VALUES ('analytics.consent_required', 'false')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`).Error)

	anonymous := uuid.NewString()
	refusedSession := uuid.NewString()
	botSession := uuid.NewString()
	refused := s.consentToken(t, false)

	r = s.send(t, page, browserUA, map[string]any{"session_id": anonymous, "path": "/"}, nil)
	require.Equal(t, nethttp.StatusAccepted, r.status, r.code)
	r = s.send(t, page, browserUA, map[string]any{"session_id": refusedSession, "path": "/"}, map[string]string{"X-Consent-Token": refused})
	require.Equal(t, nethttp.StatusAccepted, r.status, "a refusal is answered like any other event")
	r = s.send(t, page, "Googlebot/2.1", map[string]any{"session_id": botSession, "path": "/"}, nil)
	require.Equal(t, nethttp.StatusAccepted, r.status)

	require.NoError(t, s.writer.Close(t.Context()))

	var stored struct {
		Path        string
		SubjectUUID *uuid.UUID
	}
	require.NoError(t, s.tx.Raw(`SELECT path, subject_uuid FROM analytics_page_views WHERE uuid = ?`, viewID).Scan(&stored).Error)
	assert.Equal(t, "/posts/hello", stored.Path, "the query string never reaches storage")
	assert.NotNil(t, stored.SubjectUUID, "a granted subject's events can be erased with it")

	assert.Equal(t, int64(1), s.rows(t, "analytics_time_spent", session))
	assert.Equal(t, int64(1), s.rows(t, "analytics_searches", session))
	assert.Equal(t, int64(1), s.rows(t, "analytics_search_clicks", session))

	var anonymousSubject *uuid.UUID
	require.NoError(t, s.tx.Raw(`SELECT subject_uuid FROM analytics_page_views WHERE session_id = ?`, anonymous).
		Row().Scan(&anonymousSubject))
	assert.Nil(t, anonymousSubject, "without consent the event is stored unlinked")

	assert.Zero(t, s.rows(t, "analytics_page_views", refusedSession), "a refusing subject's events are dropped")
	assert.Zero(t, s.rows(t, "analytics_page_views", botSession), "bots are dropped")
}

func TestAnalyticsIngestIsDisabledBySetting(t *testing.T) {
	t.Parallel()

	s := newAnalyticsStack(t)
	require.NoError(t, s.tx.Exec(`INSERT INTO settings (key, value) VALUES ('analytics.enabled', 'false')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`).Error)

	r := s.send(t, "/api/v1/analytics/ingest/page-view", browserUA,
		map[string]any{"session_id": uuid.NewString(), "path": "/"}, nil)
	assert.Equal(t, nethttp.StatusNotFound, r.status)
	assert.Equal(t, "analytics.disabled", r.code)

	r = s.send(t, "/api/v1/analytics/ingest/navigation", browserUA,
		map[string]any{"session_id": uuid.NewString(), "to": "/", "transition": "direct"}, nil)
	assert.Equal(t, nethttp.StatusNotFound, r.status)
}
