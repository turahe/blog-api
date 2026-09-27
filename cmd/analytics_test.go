package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/ingestload"
)

func TestAnalyticsRollupRejectsBadRangeBeforeConnecting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing from", args: []string{}, wantErr: `required flag(s) "from" not set`},
		{name: "malformed from", args: []string{"--from", "2026-13-01"}, wantErr: "--from:"},
		{name: "to before from", args: []string{"--from", "2026-09-10", "--to", "2026-09-01"}, wantErr: "--to is before --from"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := runCLI(t, append([]string{"analytics", "rollup"}, tt.args...)...)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRollupRange(t *testing.T) {
	t.Parallel()

	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	tests := []struct {
		name      string
		from, to  string
		wantFirst time.Time
		wantLast  time.Time
		wantErr   string
	}{
		{name: "closed range", from: "2026-09-01", to: "2026-09-10", wantFirst: day(2026, 9, 1), wantLast: day(2026, 9, 10)},
		{name: "single day", from: "2026-09-01", to: "2026-09-01", wantFirst: day(2026, 9, 1), wantLast: day(2026, 9, 1)},
		{name: "empty to runs open ended", from: "2026-09-01", wantFirst: day(2026, 9, 1), wantLast: day(9999, 12, 31)},
		{name: "empty from", wantErr: "--from:"},
		{name: "invalid from month", from: "2026-13-01", wantErr: "--from:"},
		{name: "non-date to", from: "2026-09-01", to: "tomorrow", wantErr: "--to:"},
		{name: "to before from", from: "2026-09-10", to: "2026-09-01", wantErr: "--to is before --from"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			first, last, err := rollupRange(tt.from, tt.to)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantFirst, first)
			assert.Equal(t, tt.wantLast, last)
		})
	}
}

// inProcess serves requests without a socket so ingestload's pacing stays inside a synctest bubble.
type inProcess struct{ handler http.Handler }

func (p inProcess) RoundTrip(r *http.Request) (*http.Response, error) {
	defer func() { _ = r.Body.Close() }()

	rec := httptest.NewRecorder()
	p.handler.ServeHTTP(rec, r)

	return rec.Result(), nil
}

func TestRunLoadtest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		wantErr string
	}{
		{name: "all accepted", status: http.StatusAccepted},
		{name: "server errors fail the run", status: http.StatusInternalServerError, wantErr: "responses with status 500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) })
				cfg := ingestload.Config{
					BaseURL: "http://ingest.test", Rate: 100, Duration: time.Second,
					Client: &http.Client{Transport: inProcess{handler: handler}},
				}

				out := new(bytes.Buffer)
				cmd := &cobra.Command{}
				cmd.SetOut(out)
				cmd.SetContext(t.Context())

				err := runLoadtest(cmd, cfg, "", 0, 0.95)
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
				} else {
					require.NoError(t, err)
				}

				assert.Contains(t, out.String(), "planned 100, sent 100, skipped 0, errors 0")
				assert.NotContains(t, out.String(), "dropped by the API", "no metrics URL, no drop check")
			})
		})
	}
}

func TestPrintLoadtest(t *testing.T) {
	t.Parallel()

	out := new(bytes.Buffer)
	printLoadtest(out, ingestload.Config{Rate: 200, Duration: 30 * time.Second}, ingestload.Result{
		Planned: 10, Sent: 9, Skipped: 1, Errors: 2, Accepted: 6, Elapsed: 2 * time.Second,
		Statuses:   map[int]int64{http.StatusServiceUnavailable: 1, http.StatusAccepted: 6, http.StatusTooManyRequests: 2},
		Routes:     map[string]int64{"page-view": 4, "search": 2},
		PathPrefix: "/loadtest/abc",
		P50:        time.Millisecond, P95: 2 * time.Millisecond, P99: 3 * time.Millisecond, Max: 4 * time.Millisecond,
	})

	want := strings.Join([]string{
		"target 200 events/s for 30s; path prefix /loadtest/abc",
		"planned 10, sent 9, skipped 1, errors 2, accepted 6 (3.0 events/s)",
		"status 202: 6",
		"status 429: 2",
		"status 503: 1",
		"accepted page-view: 4",
		"accepted time-spent: 0",
		"accepted navigation: 0",
		"accepted search: 2",
		"accepted search-click: 0",
		"latency p50 1ms, p95 2ms, p99 3ms, max 4ms",
		"",
	}, "\n")
	assert.Equal(t, want, out.String())
}
