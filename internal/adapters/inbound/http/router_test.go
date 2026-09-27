package http

import (
	"context"
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	healthdomain "github.com/turahe/blog-api/internal/core/health/domain"
	impservice "github.com/turahe/blog-api/internal/core/impersonation/service"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
	notificationservice "github.com/turahe/blog-api/internal/core/notification/service"
	privacyservice "github.com/turahe/blog-api/internal/core/privacy/service"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

type liveHealth struct{}

func (liveHealth) Live() healthdomain.Status { return healthdomain.Status{Status: "ok"} }

func (liveHealth) Ready(context.Context) healthdomain.Status {
	return healthdomain.Status{Status: "ok"}
}

type recordingMetrics struct {
	mu     sync.Mutex
	routes []string
}

func (*recordingMetrics) StartRequest() func() { return func() {} }

func (m *recordingMetrics) ObserveHTTP(_, route string, _ int, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.routes = append(m.routes, route)
}

type discardAudit struct{}

func (discardAudit) Record(context.Context, auditdomain.Entry) {}

// everyService sets each service to a zero value: enough to wire routes, as long as the
// request is rejected before the service is used.
func everyService() Dependencies {
	return Dependencies{
		Logger:               slog.New(slog.DiscardHandler),
		Health:               liveHealth{},
		Auth:                 fakeAuthService{},
		AdminUsers:           &authservice.AuthService{},
		TwoFactor:            &authservice.AuthService{},
		AdminLogin:           &authservice.AuthService{},
		OAuth:                &authservice.AuthService{},
		RoleAdmin:            &rbacservice.RoleService{},
		Profiles:             &userservice.ProfileService{},
		Settings:             &settingsservice.Service{},
		Consent:              &consentservice.Service{},
		AnalyticsIngest:      &analyticsservice.Ingest{},
		AnalyticsReports:     &analyticsservice.Reports{},
		AnalyticsExports:     &analyticsservice.Exports{},
		AnalyticsLive:        &analyticsservice.Board{},
		AnalyticsLiveStreams: &realtime.Hub{},
		PrivacyRequests:      &privacyservice.Service{},
		Impersonation:        &impservice.Service{},
		Newsletter:           &nlservice.Service{},
		Activity:             &auditservice.Activity{},
		Notifications:        &notificationservice.Inbox{},
		NotificationStream:   &realtime.Hub{},
		SwaggerEnabled:       true,
	}
}

func serve(t *testing.T, deps Dependencies, method, path, body string) (int, map[string]any) {
	t.Helper()

	router, err := NewRouter(deps)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var envelope map[string]any
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())
	}

	return w.Code, envelope
}

func errorCode(envelope map[string]any) string {
	errBody, _ := envelope["error"].(map[string]any)
	code, _ := errBody["code"].(string)

	return code
}

func TestNewRouterWiresPresentServices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
		absent int
	}{
		{name: "health alias", method: nethttp.MethodGet, path: "/api/v1/health", status: nethttp.StatusOK, absent: nethttp.StatusNotFound},
		{name: "role admin", method: nethttp.MethodGet, path: "/api/v1/admin/users/nope/roles", status: nethttp.StatusBadRequest, code: "validation_error", absent: nethttp.StatusNotImplemented},
		{name: "admin activity", method: nethttp.MethodGet, path: "/api/v1/admin/users/nope/activity", status: nethttp.StatusBadRequest, code: "validation_error", absent: nethttp.StatusNotImplemented},
		{name: "impersonation", method: nethttp.MethodPost, path: "/api/v1/admin/impersonation/start", body: `{`, status: nethttp.StatusBadRequest, code: "validation_error", absent: nethttp.StatusNotImplemented},
		{name: "newsletter", method: nethttp.MethodPost, path: "/api/v1/me/newsletter/subscribe", body: `{`, status: nethttp.StatusBadRequest, code: "validation_error", absent: nethttp.StatusNotImplemented},
		{name: "profiles", method: nethttp.MethodPatch, path: "/api/v1/me/profile", body: `{`, status: nethttp.StatusBadRequest, code: "validation_error", absent: nethttp.StatusNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, envelope := serve(t, everyService(), tc.method, tc.path, tc.body)
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.code, errorCode(envelope))

			status, envelope = serve(t, Dependencies{Logger: slog.New(slog.DiscardHandler), Auth: fakeAuthService{}}, tc.method, tc.path, tc.body)
			require.Equal(t, tc.absent, status, "absent service")

			if tc.absent == nethttp.StatusNotImplemented {
				require.Equal(t, "operation.not_implemented", errorCode(envelope))
			}
		})
	}
}

func TestAuthChains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		deps  Dependencies
		links int
	}{
		{name: "no auth service", deps: Dependencies{}, links: 0},
		{name: "without impersonation", deps: Dependencies{Auth: fakeAuthService{}}, links: 1},
		{name: "with impersonation", deps: Dependencies{Auth: fakeAuthService{}, Impersonation: &impservice.Service{}}, links: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			optional, required := authChains(tc.deps)
			require.Len(t, optional, tc.links)
			require.Len(t, required, tc.links)
		})
	}
}

func TestOptionalServices(t *testing.T) {
	t.Parallel()

	full := everyService()
	full.NewsletterProvider = responses.NewsletterProvider{Name: "smtp", SendingEnabled: true}
	full.AnalyticsIngestPerMinute = 30
	full.AnalyticsCountryHeader = "CF-IPCountry"
	full.TrustedProxies = []string{"10.0.0.0/8"}

	liveOnly := Dependencies{AnalyticsLive: &analyticsservice.Board{}}

	tests := []struct {
		name  string
		deps  Dependencies
		check func(t *testing.T, got handlers.Deps)
	}{
		{
			name: "copies present services",
			deps: full,
			check: func(t *testing.T, got handlers.Deps) {
				t.Helper()
				require.NotNil(t, got.Impersonation)
				require.NotNil(t, got.Newsletter)
				require.Equal(t, "smtp", got.NewsletterProvider.Name)
				require.False(t, got.NewsletterProvider.SendingEnabled, "sending follows the service, not the config")
				require.NotNil(t, got.Profiles)
				require.NotNil(t, got.Privacy)
				require.NotNil(t, got.Settings)
				require.NotNil(t, got.SettingsValues)
				require.NotNil(t, got.PrivacyRequests)
				require.NotNil(t, got.Consent)
				require.NotNil(t, got.AnalyticsIngest)
				require.Equal(t, 30, got.AnalyticsIngestPerMinute)
				require.Equal(t, "CF-IPCountry", got.AnalyticsCountryHeader)
				require.Equal(t, []string{"10.0.0.0/8"}, got.TrustedProxies)
				require.NotNil(t, got.AnalyticsReports)
				require.NotNil(t, got.AnalyticsExports)
				require.NotNil(t, got.AnalyticsLive)
				require.NotNil(t, got.AnalyticsLiveStreams)
			},
		},
		{
			name: "absent services stay nil interfaces",
			deps: Dependencies{},
			check: func(t *testing.T, got handlers.Deps) {
				t.Helper()
				require.Equal(t, handlers.Deps{}, got)
			},
		},
		{
			name: "live board needs its stream hub",
			deps: liveOnly,
			check: func(t *testing.T, got handlers.Deps) {
				t.Helper()
				require.Nil(t, got.AnalyticsLive)
				require.Nil(t, got.AnalyticsLiveStreams)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got handlers.Deps
			optionalServices(&got, tc.deps)
			tc.check(t, got)
		})
	}
}

func TestGlobalMiddleware(t *testing.T) {
	t.Parallel()

	const base = 6

	tests := []struct {
		name  string
		deps  Dependencies
		links int
	}{
		{name: "defaults", deps: Dependencies{}, links: base},
		{name: "metrics", deps: Dependencies{Metrics: &recordingMetrics{}}, links: base + 1},
		{name: "audit", deps: Dependencies{Audit: discardAudit{}}, links: base + 1},
		{name: "cache bypass", deps: Dependencies{CacheBypassHeader: true}, links: base + 1},
		{name: "everything", deps: Dependencies{Metrics: &recordingMetrics{}, Audit: discardAudit{}, CacheBypassHeader: true}, links: base + 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, globalMiddleware(tc.deps), tc.links)
		})
	}
}

func TestNewRouterRecordsMetricsByRoute(t *testing.T) {
	t.Parallel()

	metrics := &recordingMetrics{}
	deps := Dependencies{Logger: slog.New(slog.DiscardHandler), Health: liveHealth{}, Metrics: metrics, Audit: discardAudit{}, CacheBypassHeader: true}

	status, _ := serve(t, deps, nethttp.MethodGet, "/api/v1/health", "")
	require.Equal(t, nethttp.StatusOK, status)

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	require.Equal(t, []string{"/api/v1/health"}, metrics.routes)
}
