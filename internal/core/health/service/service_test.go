package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeChecker struct {
	name     string
	err      error
	optional bool
}

func (c fakeChecker) Name() string                { return c.name }
func (c fakeChecker) Check(context.Context) error { return c.err }
func (c fakeChecker) Optional() bool              { return c.optional }

func TestReadyIgnoresOptionalFailures(t *testing.T) {
	t.Parallel()

	status := New("v1",
		fakeChecker{name: "database"},
		fakeChecker{name: "messaging", err: errors.New("down"), optional: true},
	).Ready(t.Context())

	require.Equal(t, "ok", status.Status)
	require.Len(t, status.Dependencies, 2)
	require.True(t, status.Dependencies[0].Critical)
	require.False(t, status.Dependencies[1].Critical)
	require.False(t, status.Dependencies[1].Healthy)
	require.Equal(t, "unavailable", status.Dependencies[1].Message)
}

func TestReadyDegradesOnCriticalFailure(t *testing.T) {
	t.Parallel()

	status := New("v1", fakeChecker{name: "database", err: errors.New("down")}).Ready(t.Context())

	require.Equal(t, "degraded", status.Status)
	require.True(t, status.Dependencies[0].Critical)
}
