package middleware

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type observation struct {
	method, route string
	status        int
}

type fakeRecorder struct {
	inflight int
	seen     []observation
}

func (f *fakeRecorder) StartRequest() func() {
	f.inflight++
	return func() { f.inflight-- }
}

func (f *fakeRecorder) ObserveHTTP(method, route string, status int, _ time.Duration) {
	f.seen = append(f.seen, observation{method, route, status})
}

func TestMetricsLabelsByRouteTemplate(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	recorder := &fakeRecorder{}
	router := gin.New()
	router.Use(Metrics(recorder))
	router.GET("/posts/:slug", func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

	for _, path := range []string{"/posts/hello", "/nope/secret-token"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, path, nil))
	}

	require.Equal(t, []observation{
		{nethttp.MethodGet, "/posts/:slug", nethttp.StatusNoContent},
		{nethttp.MethodGet, unmatchedRoute, nethttp.StatusNotFound},
	}, recorder.seen)
	require.Zero(t, recorder.inflight)
}
