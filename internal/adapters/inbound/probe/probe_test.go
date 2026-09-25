package probe

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
)

type fakeChecker struct {
	name string
	err  error
}

func (c fakeChecker) Name() string                { return c.name }
func (c fakeChecker) Check(context.Context) error { return c.err }

func serve(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

	return recorder
}

func TestReadyReportsCriticalFailureWithoutLeakingErrors(t *testing.T) {
	t.Parallel()

	svc := healthservice.New("v1",
		fakeChecker{name: "database"},
		fakeChecker{name: "messaging", err: errors.New("dial tcp 10.0.0.5:9092: connection refused")},
	)

	recorder := serve(t, Ready(svc, slog.New(slog.DiscardHandler)))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Contains(t, recorder.Body.String(), `"status":"degraded"`)
	require.NotContains(t, recorder.Body.String(), "10.0.0.5")
}

func TestReadyAndLiveHealthy(t *testing.T) {
	t.Parallel()

	svc := healthservice.New("v1", fakeChecker{name: "database"})

	require.Equal(t, http.StatusOK, serve(t, Ready(svc, slog.New(slog.DiscardHandler))).Code)
	require.Equal(t, http.StatusOK, serve(t, Live(svc)).Code)
}
