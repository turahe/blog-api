package requests

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

var (
	idA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	idB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	idC = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	idD = uuid.MustParse("44444444-4444-4444-8444-444444444444")
)

func TestIngestPageViewValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"sessionId": idA.String(), "path": "/posts/a"}

	runBindCases[IngestPageView](t, []bindCase{
		{name: "valid", body: with(base, "id", idB.String(), "referrer", "https://example.com")},
		{name: "id must be a uuid", body: with(base, "id", "nope"), want: errs("id", msgUUID("id"))},
		{name: "sessionId required", body: with(base, "sessionId", absent), want: errs("sessionId", msgRequired("sessionId"))},
		{name: "sessionId must be a uuid", body: with(base, "sessionId", "nope"), want: errs("sessionId", msgUUID("sessionId"))},
		{name: "path required", body: with(base, "path", absent), want: errs("path", msgRequired("path"))},
		{name: "path too long", body: with(base, "path", long(2049)), want: errs("path", msgMaxChars("path", 2048))},
		{name: "referrer too long", body: with(base, "referrer", long(2049)), want: errs("referrer", msgMaxChars("referrer", 2048))},
	})
}

func TestIngestPageViewInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  IngestPageView
		want analyticsservice.PageViewInput
	}{
		{
			name: "all fields",
			req:  IngestPageView{ID: idA.String(), SessionID: idB.String(), Path: "/a", Referrer: "https://r.test"},
			want: analyticsservice.PageViewInput{ID: idA, SessionID: idB, Path: "/a", Referrer: "https://r.test"},
		},
		{
			name: "omitted id is nil",
			req:  IngestPageView{SessionID: idB.String(), Path: "/a"},
			want: analyticsservice.PageViewInput{ID: uuid.Nil, SessionID: idB, Path: "/a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.req.Input())
		})
	}
}

func TestIngestTimeSpentValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"viewId": idA.String(), "sessionId": idB.String(), "path": "/a", "focusSeconds": 3}

	runBindCases[IngestTimeSpent](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "zero focus is valid", body: with(base, "focusSeconds", 0)},
		{name: "viewId required", body: with(base, "viewId", absent), want: errs("viewId", msgRequired("viewId"))},
		{name: "viewId must be a uuid", body: with(base, "viewId", "x"), want: errs("viewId", msgUUID("viewId"))},
		{name: "sessionId required", body: with(base, "sessionId", absent), want: errs("sessionId", msgRequired("sessionId"))},
		{name: "path required", body: with(base, "path", absent), want: errs("path", msgRequired("path"))},
		{name: "path too long", body: with(base, "path", long(2049)), want: errs("path", msgMaxChars("path", 2048))},
		{name: "negative focus", body: with(base, "focusSeconds", -1), want: errs("focusSeconds", msgMin("focusSeconds", 0))},
	})
}

func TestIngestTimeSpentInput(t *testing.T) {
	t.Parallel()

	req := IngestTimeSpent{ViewID: idA.String(), SessionID: idB.String(), Path: "/a", FocusSeconds: 42}

	assert.Equal(t, analyticsservice.TimeSpentInput{ViewID: idA, SessionID: idB, Path: "/a", FocusSeconds: 42}, req.Input())
}

func TestIngestNavigationValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"sessionId": idA.String(), "to": "/b", "transition": "internal"}

	runBindCases[IngestNavigation](t, []bindCase{
		{name: "valid entry", body: with(base, "transition", "direct")},
		{name: "valid move", body: with(base, "id", idB.String(), "from", "/a", "transition", "back_forward")},
		{name: "id must be a uuid", body: with(base, "id", "x"), want: errs("id", msgUUID("id"))},
		{name: "sessionId required", body: with(base, "sessionId", absent), want: errs("sessionId", msgRequired("sessionId"))},
		{name: "from too long", body: with(base, "from", long(2049)), want: errs("from", msgMaxChars("from", 2048))},
		{name: "to required", body: with(base, "to", absent), want: errs("to", msgRequired("to"))},
		{name: "to too long", body: with(base, "to", long(2049)), want: errs("to", msgMaxChars("to", 2048))},
		{name: "transition required", body: with(base, "transition", absent), want: errs("transition", msgRequired("transition"))},
		{name: "transition unknown", body: with(base, "transition", "teleport"), want: errs("transition", msgOneOf("transition", "internal external back_forward direct"))},
	})
}

func TestIngestNavigationInput(t *testing.T) {
	t.Parallel()

	req := IngestNavigation{ID: idA.String(), SessionID: idB.String(), From: "/a", To: "/b", Transition: "external"}

	assert.Equal(t, analyticsservice.NavigationInput{
		ID: idA, SessionID: idB, From: "/a", To: "/b", Transition: "external",
	}, req.Input())
}

func TestIngestSearchValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"sessionId": idA.String(), "query": "go"}

	runBindCases[IngestSearch](t, []bindCase{
		{name: "valid", body: with(base, "id", idB.String(), "resultCount", 3, "filters", map[string]string{"tag": "go"})},
		{name: "id must be a uuid", body: with(base, "id", "x"), want: errs("id", msgUUID("id"))},
		{name: "sessionId required", body: with(base, "sessionId", absent), want: errs("sessionId", msgRequired("sessionId"))},
		{name: "query required", body: with(base, "query", absent), want: errs("query", msgRequired("query"))},
		{name: "query too long", body: with(base, "query", long(1001)), want: errs("query", msgMaxChars("query", 1000))},
		{name: "negative result count", body: with(base, "resultCount", -1), want: errs("resultCount", msgMin("resultCount", 0))},
		{
			name: "too many filters",
			body: with(base, "filters", map[string]string{"category": "a", "tag": "b", "from": "c", "to": "d", "x": "e"}),
			want: errs("filters", msgMaxItems("filters", 4)),
		},
	})
}

func TestIngestSearchInput(t *testing.T) {
	t.Parallel()

	req := IngestSearch{ID: idA.String(), SessionID: idB.String(), Query: "go", ResultCount: 7, Filters: map[string]string{"tag": "go"}}

	assert.Equal(t, analyticsservice.SearchInput{
		ID: idA, SessionID: idB, Query: "go", ResultCount: 7, Filters: map[string]string{"tag": "go"},
	}, req.Input())
}

func TestIngestSearchClickValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sessionId": idA.String(), "searchId": idB.String(), "position": 1, "resourceType": "post", "resourceId": idC.String(),
	}

	runBindCases[IngestSearchClick](t, []bindCase{
		{name: "valid", body: with(base, "id", idD.String())},
		{name: "id must be a uuid", body: with(base, "id", "x"), want: errs("id", msgUUID("id"))},
		{name: "sessionId required", body: with(base, "sessionId", absent), want: errs("sessionId", msgRequired("sessionId"))},
		{name: "searchId required", body: with(base, "searchId", absent), want: errs("searchId", msgRequired("searchId"))},
		{name: "searchId must be a uuid", body: with(base, "searchId", "x"), want: errs("searchId", msgUUID("searchId"))},
		{name: "position required", body: with(base, "position", absent), want: errs("position", msgRequired("position"))},
		{name: "position from one", body: with(base, "position", -2), want: errs("position", msgMin("position", 1))},
		{name: "resourceType required", body: with(base, "resourceType", absent), want: errs("resourceType", msgRequired("resourceType"))},
		{name: "resourceType unknown", body: with(base, "resourceType", "user"), want: errs("resourceType", msgOneOf("resourceType", "post page category tag"))},
		{name: "resourceId required", body: with(base, "resourceId", absent), want: errs("resourceId", msgRequired("resourceId"))},
		{name: "resourceId must be a uuid", body: with(base, "resourceId", "x"), want: errs("resourceId", msgUUID("resourceId"))},
	})
}

func TestIngestSearchClickInput(t *testing.T) {
	t.Parallel()

	req := IngestSearchClick{
		ID: idA.String(), SessionID: idB.String(), SearchID: idC.String(), Position: 2, ResourceType: "tag", ResourceID: idD.String(),
	}

	assert.Equal(t, analyticsservice.SearchClickInput{
		ID: idA, SessionID: idB, SearchID: idC, Position: 2, ResourceType: "tag", ResourceID: idD,
	}, req.Input())
}

func TestAnalyticsReportValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		query    string
		wantRule string
	}{
		{name: "empty query is valid", query: ""},
		{name: "all fields valid", query: "from=2026-08-01&to=2026-08-31&grain=week&compare=none&limit=100&sort=rising"},
		{name: "bad from", query: "from=08/01/2026", wantRule: "must be a valid date and time"},
		{name: "bad to", query: "to=2026-13-01", wantRule: "must be a valid date and time"},
		{name: "bad grain", query: "grain=year", wantRule: "must be one of: day week month"},
		{name: "bad compare", query: "compare=yoy", wantRule: "must be one of: previous none"},
		{name: "limit too small", query: "limit=-1", wantRule: "must be at least 1"},
		{name: "limit too large", query: "limit=101", wantRule: "may not be greater than 100"},
		{name: "bad sort", query: "sort=random", wantRule: "must be one of: views time rising"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?"+tt.query, nil)

			var dst AnalyticsReport

			err := binding.Query.Bind(req, &dst)
			if tt.wantRule == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)

			details := FormatValidationError(err)
			require.Len(t, details, 1)

			for _, messages := range details {
				require.Len(t, messages, 1)
				assert.Contains(t, messages[0], tt.wantRule)
			}
		})
	}
}

func TestAnalyticsReportQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  AnalyticsReport
		want analyticsdomain.ReportQuery
	}{
		{
			name: "defaults compare to previous",
			req:  AnalyticsReport{},
			want: analyticsdomain.ReportQuery{Compare: true},
		},
		{
			name: "all fields",
			req:  AnalyticsReport{From: "2026-08-01", To: "2026-08-31", Grain: "week", Compare: "none", Limit: 10, Sort: "views"},
			want: analyticsdomain.ReportQuery{
				From:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
				To:    time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
				Grain: analyticsdomain.GrainWeek, Compare: false, Limit: 10, Sort: "views",
			},
		},
		{
			name: "explicit previous compares",
			req:  AnalyticsReport{Compare: "previous", Grain: "month"},
			want: analyticsdomain.ReportQuery{Grain: analyticsdomain.GrainMonth, Compare: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.req.Query())
		})
	}
}

func TestAnalyticsExportValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"from": "2026-08-01", "to": "2026-08-31", "currentPassword": "secret"}

	runBindCases[AnalyticsExport](t, []bindCase{
		{name: "valid", body: with(base, "grain", "day", "twoFactorCode", "123456")},
		{name: "from required", body: with(base, "from", absent), want: errs("from", msgRequired("from"))},
		{name: "from format", body: with(base, "from", "2026/08/01"), want: errs("from", msgDate("from"))},
		{name: "to required", body: with(base, "to", absent), want: errs("to", msgRequired("to"))},
		{name: "to format", body: with(base, "to", "tomorrow"), want: errs("to", msgDate("to"))},
		{name: "grain unknown", body: with(base, "grain", "hour"), want: errs("grain", msgOneOf("grain", "day week month"))},
		{name: "password required", body: with(base, "currentPassword", absent), want: errs("currentPassword", msgRequired("currentPassword"))},
		{name: "password too long", body: with(base, "currentPassword", long(129)), want: errs("currentPassword", msgMaxChars("currentPassword", 128))},
		{name: "code too long", body: with(base, "twoFactorCode", long(33)), want: errs("twoFactorCode", msgMaxChars("twoFactorCode", 32))},
	})
}

func TestAnalyticsExportInput(t *testing.T) {
	t.Parallel()

	req := AnalyticsExport{From: "2026-08-01", To: "2026-08-31", Grain: "day", CurrentPassword: "secret", TwoFactorCode: "123456"}

	assert.Equal(t, analyticsservice.ExportRequest{
		Query: analyticsdomain.ReportQuery{
			From:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			To:    time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			Grain: analyticsdomain.GrainDay,
		},
		Password: "secret",
		Code:     "123456",
	}, req.Input())
}

func TestOptionalDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{name: "date", in: "2026-02-28", want: time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)},
		{name: "empty", in: "", want: time.Time{}},
		{name: "invalid", in: "2026-02-30", want: time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, optionalDate(tt.in))
		})
	}
}

func TestOptionalUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want uuid.UUID
	}{
		{name: "uuid", in: idA.String(), want: idA},
		{name: "empty", in: "", want: uuid.Nil},
		{name: "invalid", in: "nope", want: uuid.Nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, optionalUUID(tt.in))
		})
	}
}
