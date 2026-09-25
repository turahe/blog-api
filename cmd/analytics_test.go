package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollupRange(t *testing.T) {
	t.Parallel()

	first, last, err := rollupRange("2026-09-01", "2026-09-10")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), first)
	assert.Equal(t, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), last)

	_, last, err = rollupRange("2026-09-01", "")
	require.NoError(t, err)
	assert.True(t, last.After(time.Now()), "an empty --to runs through today")

	for _, bad := range [][2]string{{"", ""}, {"2026-13-01", ""}, {"2026-09-01", "tomorrow"}, {"2026-09-10", "2026-09-01"}} {
		_, _, err := rollupRange(bad[0], bad[1])
		require.Error(t, err, bad)
	}
}
