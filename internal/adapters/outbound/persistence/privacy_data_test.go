package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPrivacyDataUnknownUser(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	data := NewPrivacyData(tx)
	at := time.Now().UTC()

	_, err := data.ExportUser(t.Context(), uuid.New(), at)
	require.ErrorIs(t, err, errUserGone)
	require.ErrorIs(t, data.EraseUser(t.Context(), uuid.New(), at), errUserGone)
}

func TestPrivacyDataDatabaseErrors(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	data := NewPrivacyData(tx)
	user := insertUser(t, tx)
	at := time.Now().UTC()

	_, err := data.ExportUser(canceledContext(t), user, at)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, data.EraseUser(canceledContext(t), user, at), context.Canceled)
}

func TestPrivacyDataEraseUserIsRepeatable(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	data := NewPrivacyData(tx)
	user := insertUser(t, tx)
	at := time.Now().UTC()

	require.NoError(t, data.EraseUser(t.Context(), user, at))
	require.NoError(t, data.EraseUser(t.Context(), user, at), "running it again is harmless")

	got, err := NewUserRepository(tx).FindByID(t.Context(), user)
	require.NoError(t, err)
	require.Equal(t, ErasedFullName, got.FullName)
	require.Equal(t, "erased+"+user.String()+"@invalid", got.Email)
}
