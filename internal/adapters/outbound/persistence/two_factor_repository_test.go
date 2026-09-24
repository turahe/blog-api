package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func TestTwoFactorRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTwoFactorRepository(tx)
	ctx := t.Context()
	userID := insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Second)

	_, err := repo.Find(ctx, userID)
	require.ErrorIs(t, err, authdomain.ErrTwoFactorNotEnrolled)

	require.NoError(t, repo.SavePending(ctx, userID, "v1:first", now))
	require.NoError(t, repo.SavePending(ctx, userID, "v1:second", now), "setup may restart while unconfirmed")

	tf, err := repo.Find(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "v1:second", tf.SecretCiphertext)
	require.False(t, tf.Enabled())

	require.NoError(t, repo.Confirm(ctx, userID, 100, []string{"h1", "h2", "h3"}, now))
	require.ErrorIs(t, repo.Confirm(ctx, userID, 101, nil, now), authdomain.ErrTwoFactorInvalidCode, "confirm is one-shot")
	require.ErrorIs(t, repo.SavePending(ctx, userID, "v1:third", now), authdomain.ErrTwoFactorAlreadyEnabled)

	tf, err = repo.Find(ctx, userID)
	require.NoError(t, err)
	require.True(t, tf.Enabled())
	require.Equal(t, "v1:second", tf.SecretCiphertext)
	require.Equal(t, int64(100), tf.LastUsedStep)
	require.Equal(t, 3, tf.BackupCodesRemaining)

	ok, err := repo.UseStep(ctx, userID, 100)
	require.NoError(t, err)
	require.False(t, ok, "the confirm step is spent")

	ok, err = repo.UseStep(ctx, userID, 101)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = repo.UseStep(ctx, userID, 101)
	require.NoError(t, err)
	require.False(t, ok, "a step is accepted once")

	ok, err = repo.UseBackupCode(ctx, userID, "h2", now)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = repo.UseBackupCode(ctx, userID, "h2", now)
	require.NoError(t, err)
	require.False(t, ok, "a backup code is spent once")

	tf, err = repo.Find(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 2, tf.BackupCodesRemaining)

	require.NoError(t, repo.ReplaceBackupCodes(ctx, userID, []string{"n1", "n2"}, now))

	ok, err = repo.UseBackupCode(ctx, userID, "h1", now)
	require.NoError(t, err)
	require.False(t, ok, "replaced codes are gone")

	require.NoError(t, repo.Delete(ctx, userID))

	_, err = repo.Find(ctx, userID)
	require.ErrorIs(t, err, authdomain.ErrTwoFactorNotEnrolled)

	var codes int64
	require.NoError(t, tx.Model(&TwoFactorBackupCodeModel{}).Where("user_id = "+idOf("users"), userID).Count(&codes).Error)
	require.Zero(t, codes)
}

func TestTwoFactorRepositoryUnknownUser(t *testing.T) {
	t.Parallel()

	repo := NewTwoFactorRepository(integrationTx(t))

	require.Error(t, repo.SavePending(t.Context(), uuid.New(), "v1:x", time.Now()))

	ok, err := repo.UseStep(t.Context(), uuid.New(), 1)
	require.NoError(t, err)
	require.False(t, ok)
}
