package middleware

import (
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMaxInFlightShedsBeyondLimit(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	entered, release := make(chan struct{}), make(chan struct{})

	router := gin.New()
	router.Use(MaxInFlight(1))
	router.GET("/slow", func(c *gin.Context) {
		close(entered)
		<-release
		c.Status(nethttp.StatusOK)
	})
	router.GET("/health/ready", func(c *gin.Context) { c.Status(nethttp.StatusOK) })
	router.GET("/me/notifications/stream", func(c *gin.Context) { c.Status(nethttp.StatusOK) })

	serve := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, path, nil))

		return recorder
	}

	var (
		wg   sync.WaitGroup
		slow *httptest.ResponseRecorder
	)

	wg.Go(func() { slow = serve("/slow") })

	<-entered

	shed := serve("/slow")
	require.Equal(t, nethttp.StatusServiceUnavailable, shed.Code)
	require.Equal(t, "1", shed.Header().Get("Retry-After"))
	require.Contains(t, shed.Body.String(), "server.overloaded")

	require.Equal(t, nethttp.StatusOK, serve("/health/ready").Code, "probes are never shed")
	require.Equal(t, nethttp.StatusOK, serve("/me/notifications/stream").Code, "streams are not counted")

	close(release)
	wg.Wait()
	require.Equal(t, nethttp.StatusOK, slow.Code)

	require.Equal(t, nethttp.StatusOK, serve("/health/ready").Code)
}

func TestMaxInFlightDisabled(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MaxInFlight(0))
	router.GET("/x", func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil))
	require.Equal(t, nethttp.StatusNoContent, recorder.Code)
}
