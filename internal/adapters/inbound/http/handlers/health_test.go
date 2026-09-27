package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"testing"

	"github.com/stretchr/testify/require"
	healthdomain "github.com/turahe/blog-api/internal/core/health/domain"
)

type stubHealth struct{ ready healthdomain.Status }

func (stubHealth) Live() healthdomain.Status { return healthdomain.Status{Status: "ok", Version: "v1"} }

func (s stubHealth) Ready(context.Context) healthdomain.Status { return s.ready }

func TestLive(t *testing.T) {
	t.Parallel()

	w, body := runProfile(t, Live(stubHealth{}), profileRequest{method: nethttp.MethodGet, target: "/health/live"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, "ok", dataOf(body)["status"])
	require.Equal(t, "v1", dataOf(body)["version"])
}

func TestReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status healthdomain.Status
		code   int
	}{
		{name: "healthy", status: healthdomain.Status{Status: "ok"}, code: nethttp.StatusOK},
		{
			name: "optional dependency down stays ready",
			status: healthdomain.Status{Status: "ok", Dependencies: []healthdomain.Dependency{
				{Name: "redis", Healthy: false, Err: errors.New("dial refused")},
				{Name: "postgres", Healthy: true, Critical: true},
			}},
			code: nethttp.StatusOK,
		},
		{
			name: "critical dependency down",
			status: healthdomain.Status{Status: "degraded", Dependencies: []healthdomain.Dependency{
				{Name: "postgres", Critical: true, Err: errors.New("timeout")},
			}},
			code: nethttp.StatusServiceUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, ready(stubHealth{ready: tc.status}), profileRequest{method: nethttp.MethodGet, target: "/health/ready"})
			require.Equal(t, tc.code, w.Code)
			require.Equal(t, tc.status.Status, dataOf(body)["status"])

			deps, _ := dataOf(body)["dependencies"].([]any)
			require.Len(t, deps, len(tc.status.Dependencies))

			for _, dep := range deps {
				require.NotContains(t, as[map[string]any](t, dep), "Err", "errors are never serialized")
			}
		})
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	w, body := runProfile(t, version("1.2.3"), profileRequest{method: nethttp.MethodGet, target: "/health/version"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, "1.2.3", dataOf(body)["version"])
}
