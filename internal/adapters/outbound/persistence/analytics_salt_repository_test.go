package persistence

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyticsSaltsAreSharedThenDestroyed(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewAnalyticsSaltRepository(tx)
	first, second := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)

	salt, err := repo.DailySalt(ctx, "2001-01-02", first, "2001-01-01")
	require.NoError(t, err)
	assert.Equal(t, first, salt)

	salt, err = repo.DailySalt(ctx, "2001-01-02", second, "2001-01-01")
	require.NoError(t, err)
	assert.Equal(t, first, salt, "the salt stored first wins")

	_, err = repo.DailySalt(ctx, "2001-01-04", second, "2001-01-03")
	require.NoError(t, err)
	assert.Zero(t, countWhere(t, tx, "analytics_salts", "day = ?", "2001-01-02"), "a new day destroys salts before yesterday")

	_, err = repo.DailySalt(ctx, "2001-01-05", first, "2001-01-01")
	require.NoError(t, err)

	pruned, err := NewAnalyticsRetentionRepository(tx).PruneSalts(ctx, "2001-01-05")
	require.NoError(t, err)
	assert.Equal(t, int64(1), pruned)
	assert.Equal(t, int64(1), countWhere(t, tx, "analytics_salts", "day BETWEEN ? AND '2001-12-31'", "2001-01-01"))
}
