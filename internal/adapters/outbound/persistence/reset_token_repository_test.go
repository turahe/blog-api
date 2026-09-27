package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func newResetToken(user uuid.UUID, mutate func(*authdomain.PasswordResetToken)) authdomain.PasswordResetToken {
	now := time.Now().UTC().Truncate(time.Microsecond)

	token := authdomain.PasswordResetToken{
		UUID: uuid.New(), UserUUID: user, JTI: uniqueSlug("jti"), TokenHash: uniqueSlug("hash"),
		Purpose: authdomain.PurposePasswordReset, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if mutate != nil {
		mutate(&token)
	}

	return token
}

func createResetToken(t *testing.T, repo *ResetTokenRepository, user uuid.UUID, mutate func(*authdomain.PasswordResetToken)) authdomain.PasswordResetToken {
	t.Helper()

	token := newResetToken(user, mutate)
	require.NoError(t, repo.Create(t.Context(), token))

	return token
}

func findResetToken(t *testing.T, repo *ResetTokenRepository, token authdomain.PasswordResetToken) authdomain.PasswordResetToken {
	t.Helper()

	got, err := repo.FindByHash(t.Context(), token.TokenHash)
	require.NoError(t, err)

	return got
}

func TestResetTokenRepositoryCreateAndFindByHash(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewResetTokenRepository(tx)
	ctx := t.Context()
	user := insertUser(t, tx)

	reset := createResetToken(t, repo, user, nil)
	change := createResetToken(t, repo, user, func(tok *authdomain.PasswordResetToken) {
		tok.Purpose, tok.NewEmail = authdomain.PurposeEmailChange, "new@example.test"
	})

	got := findResetToken(t, repo, reset)
	require.Equal(t, reset.UUID, got.UUID)
	require.Equal(t, user, got.UserUUID)
	require.Equal(t, reset.JTI, got.JTI)
	require.Equal(t, authdomain.PurposePasswordReset, got.Purpose)
	require.Empty(t, got.NewEmail)
	require.WithinDuration(t, reset.ExpiresAt, got.ExpiresAt, 0)
	require.Nil(t, got.UsedAt)

	got = findResetToken(t, repo, change)
	require.Equal(t, authdomain.PurposeEmailChange, got.Purpose)
	require.Equal(t, "new@example.test", got.NewEmail)

	_, err := repo.FindByHash(ctx, uniqueSlug("missing"))
	require.ErrorIs(t, err, authdomain.ErrInvalidToken)

	_, err = repo.FindByHash(canceledContext(t), reset.TokenHash)
	require.ErrorIs(t, err, context.Canceled)

	require.ErrorIs(t, repo.Create(ctx, newResetToken(uuid.New(), nil)), errUnknownReference)
}

func TestResetTokenRepositoryMarkUsed(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewResetTokenRepository(tx)
	token := createResetToken(t, repo, insertUser(t, tx), nil)
	used := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.MarkUsed(t.Context(), token.UUID, used))
	require.NoError(t, repo.MarkUsed(t.Context(), token.UUID, used.Add(time.Hour)))

	got := findResetToken(t, repo, token)
	require.NotNil(t, got.UsedAt)
	require.WithinDuration(t, used, *got.UsedAt, 0, "a used token keeps its first use time")
}

func TestResetTokenRepositoryRevokePending(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewResetTokenRepository(tx)
	user, other := insertUser(t, tx), insertUser(t, tx)
	at := time.Now().UTC().Truncate(time.Microsecond)

	pending := createResetToken(t, repo, user, nil)
	alreadyUsed := createResetToken(t, repo, user, nil)
	require.NoError(t, repo.MarkUsed(t.Context(), alreadyUsed.UUID, at.Add(-time.Hour)))
	emailChange := createResetToken(t, repo, user, func(tok *authdomain.PasswordResetToken) { tok.Purpose = authdomain.PurposeEmailChange })
	othersToken := createResetToken(t, repo, other, nil)

	require.NoError(t, repo.RevokePending(t.Context(), user, authdomain.PurposePasswordReset, at))

	got := findResetToken(t, repo, pending)
	require.NotNil(t, got.UsedAt)
	require.WithinDuration(t, at, *got.UsedAt, 0)

	got = findResetToken(t, repo, alreadyUsed)
	require.WithinDuration(t, at.Add(-time.Hour), *got.UsedAt, 0)
	require.Nil(t, findResetToken(t, repo, emailChange).UsedAt, "other purposes stay pending")
	require.Nil(t, findResetToken(t, repo, othersToken).UsedAt, "other users stay pending")
}

func TestResetTokenRepositoryPruneExpiredBefore(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewResetTokenRepository(tx)
	user := insertUser(t, tx)

	// A century back, so no other row can fall before the cutoff.
	expiry := time.Now().UTC().AddDate(-100, 0, 0).Truncate(time.Microsecond)
	expired := createResetToken(t, repo, user, func(tok *authdomain.PasswordResetToken) {
		tok.CreatedAt, tok.ExpiresAt = expiry.Add(-time.Hour), expiry
	})
	live := createResetToken(t, repo, user, nil)

	pruned, err := repo.PruneExpiredBefore(t.Context(), expiry.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(1), pruned)

	_, err = repo.FindByHash(t.Context(), expired.TokenHash)
	require.ErrorIs(t, err, authdomain.ErrInvalidToken)
	findResetToken(t, repo, live)

	_, err = repo.PruneExpiredBefore(canceledContext(t), expiry)
	require.ErrorIs(t, err, context.Canceled)
}
