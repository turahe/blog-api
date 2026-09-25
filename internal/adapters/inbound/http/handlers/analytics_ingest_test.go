package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

type fakeIngest struct {
	mu     sync.Mutex
	meta   analyticsservice.Meta
	inputs []any
	err    error
}

func (f *fakeIngest) record(meta analyticsservice.Meta, in any) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.meta = meta
	f.inputs = append(f.inputs, in)

	return uuid.New(), f.err
}

func (f *fakeIngest) PageView(_ context.Context, m analyticsservice.Meta, in analyticsservice.PageViewInput) (uuid.UUID, error) {
	return f.record(m, in)
}

func (f *fakeIngest) TimeSpent(_ context.Context, m analyticsservice.Meta, in analyticsservice.TimeSpentInput) (uuid.UUID, error) {
	return f.record(m, in)
}

func (f *fakeIngest) Navigation(_ context.Context, m analyticsservice.Meta, in analyticsservice.NavigationInput) (uuid.UUID, error) {
	return f.record(m, in)
}

func (f *fakeIngest) Search(_ context.Context, m analyticsservice.Meta, in analyticsservice.SearchInput) (uuid.UUID, error) {
	return f.record(m, in)
}

func (f *fakeIngest) SearchClick(_ context.Context, m analyticsservice.Meta, in analyticsservice.SearchClickInput) (uuid.UUID, error) {
	return f.record(m, in)
}

func (f *fakeIngest) last() (analyticsservice.Meta, any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.inputs) == 0 {
		return f.meta, nil
	}

	return f.meta, f.inputs[len(f.inputs)-1]
}

type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	return false, time.Minute, nil
}

type ingestCall struct {
	path, body, remote string
	headers            map[string]string
}

// serveIngest routes the analytics controllers the way registerAnalytics does: the gate as
// group middleware, then the handler.
func serveIngest(t *testing.T, a routes.Analytics, call ingestCall) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	group := engine.Group("")
	group.Use(a.IngestLimits...)

	if a.IngestGate != nil {
		group.Use(a.IngestGate)
	}

	for path, h := range map[string]gin.HandlerFunc{
		"/page-view": a.PageView, "/time-spent": a.TimeSpent, "/navigation": a.Navigation,
		"/search": a.Search, "/search-click": a.SearchClick,
	} {
		group.POST(path, h)
	}

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, call.path, strings.NewReader(call.body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Chrome/140.0")

	if call.remote != "" {
		req.RemoteAddr = call.remote
	}

	for k, v := range call.headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	var body map[string]any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	}

	return w, body
}

func ingestControllers(deps Deps) routes.Analytics {
	var a routes.Analytics

	analyticsIngestControllers(deps, &a)

	return a
}

func TestIngestHandlersAcceptEveryKind(t *testing.T) {
	t.Parallel()

	session := uuid.NewString()
	id := uuid.New()
	cases := map[string]struct {
		body string
		want any
	}{
		"/page-view": {
			fmt.Sprintf(`{"id":%q,"sessionId":%q,"path":"/posts/hello?utm=1","referrer":"https://example.com/x"}`, id, session),
			analyticsservice.PageViewInput{ID: id, SessionID: uuid.MustParse(session), Path: "/posts/hello?utm=1", Referrer: "https://example.com/x"},
		},
		"/time-spent": {
			fmt.Sprintf(`{"viewId":%q,"sessionId":%q,"path":"/posts/hello","focusSeconds":42}`, id, session),
			analyticsservice.TimeSpentInput{ViewID: id, SessionID: uuid.MustParse(session), Path: "/posts/hello", FocusSeconds: 42},
		},
		"/navigation": {
			fmt.Sprintf(`{"sessionId":%q,"from":"/","to":"/posts/hello","transition":"internal"}`, session),
			analyticsservice.NavigationInput{SessionID: uuid.MustParse(session), From: "/", To: "/posts/hello", Transition: "internal"},
		},
		"/search": {
			fmt.Sprintf(`{"sessionId":%q,"query":"Go","resultCount":3,"filters":{"tag":"go"}}`, session),
			analyticsservice.SearchInput{SessionID: uuid.MustParse(session), Query: "Go", ResultCount: 3, Filters: map[string]string{"tag": "go"}},
		},
		"/search-click": {
			fmt.Sprintf(`{"sessionId":%q,"searchId":%q,"position":2,"resourceType":"post","resourceId":%q}`, session, id, id),
			analyticsservice.SearchClickInput{
				SessionID: uuid.MustParse(session), SearchID: id, Position: 2, ResourceType: "post", ResourceID: id,
			},
		},
	}

	for path, tc := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			ingest := &fakeIngest{}
			w, body := serveIngest(t, ingestControllers(Deps{AnalyticsIngest: ingest}), ingestCall{path: path, body: tc.body})
			require.Equal(t, nethttp.StatusAccepted, w.Code, w.Body.String())
			assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			assert.NotEmpty(t, dataOf(body)["id"])

			_, got := ingest.last()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIngestHandlersRejectInvalidEvents(t *testing.T) {
	t.Parallel()

	ingest := &fakeIngest{}
	a := ingestControllers(Deps{AnalyticsIngest: ingest})

	w, _ := serveIngest(t, a, ingestCall{path: "/page-view", body: `{"path":"/"}`})
	assert.Equal(t, nethttp.StatusBadRequest, w.Code, "sessionId is required")

	w, _ = serveIngest(t, a, ingestCall{path: "/navigation", body: fmt.Sprintf(`{"sessionId":%q,"to":"/","transition":"teleport"}`, uuid.New())})
	assert.Equal(t, nethttp.StatusBadRequest, w.Code)

	ingest.err = fmt.Errorf("%w: path must start with /", analyticsdomain.ErrValidation)
	w, body := serveIngest(t, a, ingestCall{path: "/page-view", body: fmt.Sprintf(`{"sessionId":%q,"path":"x"}`, uuid.New())})
	assert.Equal(t, nethttp.StatusBadRequest, w.Code)
	assert.Equal(t, responses.ErrorCodeValidation, errorCode(body))

	large := fmt.Sprintf(`{"sessionId":%q,"path":"/%s"}`, uuid.New(), strings.Repeat("a", ingestMaxBodyBytes))
	w, body = serveIngest(t, a, ingestCall{path: "/page-view", body: large})
	assert.Equal(t, nethttp.StatusRequestEntityTooLarge, w.Code)
	assert.Equal(t, "analytics.payload_too_large", errorCode(body))
}

func TestIngestMetaFromTheRequest(t *testing.T) {
	t.Parallel()

	body := fmt.Sprintf(`{"sessionId":%q,"path":"/"}`, uuid.New())
	deps := Deps{AnalyticsCountryHeader: "Cf-Ipcountry", TrustedProxies: []string{"10.0.0.0/8", "192.0.2.7"}}

	for name, tc := range map[string]struct {
		remote  string
		headers map[string]string
		country string
	}{
		"trusted proxy range": {"10.1.2.3:4000", map[string]string{"CF-IPCountry": "DE"}, "DE"},
		"trusted proxy ip":    {"192.0.2.7:4000", map[string]string{"CF-IPCountry": "FR"}, "FR"},
		"untrusted peer":      {"203.0.113.9:4000", map[string]string{"CF-IPCountry": "DE"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ingest := &fakeIngest{}
			d := deps
			d.AnalyticsIngest = ingest

			w, _ := serveIngest(t, ingestControllers(d), ingestCall{path: "/page-view", body: body, remote: tc.remote, headers: tc.headers})
			require.Equal(t, nethttp.StatusAccepted, w.Code)

			meta, _ := ingest.last()
			assert.Equal(t, tc.country, meta.Country)
			assert.NotEmpty(t, meta.IP)
			assert.Contains(t, meta.UserAgent, "Chrome")
		})
	}

	ingest := &fakeIngest{}
	subject := uuid.New()
	a := ingestControllers(Deps{AnalyticsIngest: ingest})
	a.IngestGate = func(c *gin.Context) { c.Set(contextAnalyticsSubject, subject) }

	_, _ = serveIngest(t, a, ingestCall{path: "/page-view", body: body, headers: map[string]string{"Sec-Purpose": "prefetch;prerender"}})
	meta, _ := ingest.last()
	assert.Equal(t, &subject, meta.Subject)
	assert.True(t, meta.Prefetch)
	assert.False(t, meta.Refused)

	a.IngestGate = func(c *gin.Context) { c.Set(contextAnalyticsRefused, true) }
	_, _ = serveIngest(t, a, ingestCall{path: "/page-view", body: body})
	meta, _ = ingest.last()
	assert.True(t, meta.Refused)
	assert.Nil(t, meta.Subject)
}

func TestIngestLimitsRunBeforeTheConsentGate(t *testing.T) {
	t.Parallel()

	gateCalls := 0
	a := routes.Analytics{IngestGate: func(*gin.Context) { gateCalls++ }}
	analyticsIngestControllers(Deps{AnalyticsIngest: &fakeIngest{}, RateLimiter: denyLimiter{}, AnalyticsIngestPerMinute: 1}, &a)

	w, _ := serveIngest(t, a, ingestCall{path: "/page-view", body: fmt.Sprintf(`{"sessionId":%q,"path":"/"}`, uuid.New())})
	assert.Equal(t, nethttp.StatusTooManyRequests, w.Code)
	assert.Zero(t, gateCalls, "a throttled request must not reach the consent lookup")
}

func TestIngestControllersStayStubsWithoutAService(t *testing.T) {
	t.Parallel()

	gate := func(*gin.Context) {}
	a := routes.Analytics{IngestGate: gate}
	analyticsIngestControllers(Deps{}, &a)

	assert.Nil(t, a.PageView)
	assert.Nil(t, a.IngestLimits)
	assert.NotNil(t, a.IngestGate)
}
