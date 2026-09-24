package middleware

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type countingLimiter struct {
	hits map[string]int
	err  error
}

func (l *countingLimiter) Allow(_ context.Context, key string, limit int, _ time.Duration) (bool, time.Duration, error) {
	if l.err != nil {
		return false, 0, l.err
	}

	l.hits[key]++

	return l.hits[key] <= limit, 30 * time.Second, nil
}

func rateLimitedRouter(limiter Limiter, user *uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/x", func(c *gin.Context) {
		if user != nil {
			c.Set(ContextUserIDKey, *user)
		}

		c.Next()
	}, RateLimit(limiter, nil, "comments.create", 2, time.Minute), func(c *gin.Context) {
		c.Status(nethttp.StatusNoContent)
	})

	return router
}

func TestRateLimitRejectsOverLimitWithRetryAfter(t *testing.T) {
	t.Parallel()

	limiter := &countingLimiter{hits: map[string]int{}}
	router := rateLimitedRouter(limiter, nil)

	codes := make([]int, 0, 3)

	var last *httptest.ResponseRecorder
	for range 3 {
		last = httptest.NewRecorder()
		router.ServeHTTP(last, httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/x", nil))
		codes = append(codes, last.Code)
	}

	require.Equal(t, []int{204, 204, 429}, codes)
	require.Equal(t, "30", last.Header().Get("Retry-After"))
	require.Contains(t, limiter.hits, "comments.create:ip:192.0.2.1")
}

func TestRateLimitKeysByUserWhenAuthenticated(t *testing.T) {
	t.Parallel()

	limiter := &countingLimiter{hits: map[string]int{}}
	user := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	router := rateLimitedRouter(limiter, &user)

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/x", nil))

	require.Equal(t, 1, limiter.hits["comments.create:user:"+user.String()])
}

func TestRateLimitFailsOpen(t *testing.T) {
	t.Parallel()

	router := rateLimitedRouter(&countingLimiter{err: errors.New("redis down")}, nil)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/x", nil))

	require.Equal(t, nethttp.StatusNoContent, recorder.Code)
}
