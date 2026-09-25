package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

func settingsWrite(key, previous, value string, expected int64, by *uuid.UUID, at time.Time) settingsdomain.Write {
	w := settingsdomain.Write{
		Key: key, Value: json.RawMessage(value), ExpectedVersion: expected,
		ChangedBy: by, RequestID: "req-" + key, HistoryID: uuid.New(), At: at,
	}
	if previous != "" {
		w.Previous = json.RawMessage(previous)
	}

	return w
}

func TestSettingsRepositoryVersionedSaveAndHistory(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewSettingsRepository(tx)
	ctx := t.Context()
	user := insertUser(t, tx)
	key := "test." + uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.Save(ctx, settingsWrite(key, "", `"one"`, 0, &user, at)))
	require.ErrorIs(t, repo.Save(ctx, settingsWrite(key, "", `"dup"`, 0, &user, at)), settingsdomain.ErrVersionConflict,
		"a second first write loses")
	require.NoError(t, repo.Save(ctx, settingsWrite(key, `"one"`, `["a","b"]`, 1, nil, at.Add(time.Second))))
	require.ErrorIs(t, repo.Save(ctx, settingsWrite(key, `"one"`, `"stale"`, 1, nil, at)), settingsdomain.ErrVersionConflict)

	rows, err := repo.List(ctx)
	require.NoError(t, err)

	var got *settingsdomain.Stored

	for i := range rows {
		if rows[i].Key == key {
			got = &rows[i]
		}
	}

	require.NotNil(t, got)
	assert.EqualValues(t, 2, got.Version)
	assert.JSONEq(t, `["a","b"]`, string(got.Value))
	assert.Nil(t, got.UpdatedBy, "the second write had no actor")

	locked, err := repo.Lock(ctx, []string{key, "test.missing"})
	require.NoError(t, err)
	require.Len(t, locked, 1)
	assert.Equal(t, key, locked[0].Key)

	page, err := repo.History(ctx, settingsdomain.HistoryFilter{Key: key, Page: 1, PerPage: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 2, page.Total, "failed saves leave no history")
	require.Len(t, page.Items, 2)

	newest, oldest := page.Items[0], page.Items[1]
	assert.EqualValues(t, 2, newest.Version)
	assert.JSONEq(t, `"one"`, string(newest.Previous))
	assert.JSONEq(t, `["a","b"]`, string(newest.New))
	assert.EqualValues(t, 1, oldest.Version)
	assert.Nil(t, oldest.Previous)
	assert.Equal(t, &user, oldest.ChangedBy)
	assert.Equal(t, "req-"+key, oldest.RequestID)

	page, err = repo.History(ctx, settingsdomain.HistoryFilter{Key: key, Page: 2, PerPage: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.EqualValues(t, 1, page.Items[0].Version)
}

func TestSettingsRepositoryRollsBackWithTransaction(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewSettingsRepository(tx)
	key := "test." + uuid.NewString()
	failed := errors.New("outbox failed")

	err := NewTransactor(tx).InTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Save(ctx, settingsWrite(key, "", `true`, 0, nil, time.Now().UTC())))
		return failed
	})
	require.ErrorIs(t, err, failed)

	locked, err := repo.Lock(t.Context(), []string{key})
	require.NoError(t, err)
	assert.Empty(t, locked)

	page, err := repo.History(t.Context(), settingsdomain.HistoryFilter{Key: key, Page: 1, PerPage: 10})
	require.NoError(t, err)
	assert.Zero(t, page.Total)
}
