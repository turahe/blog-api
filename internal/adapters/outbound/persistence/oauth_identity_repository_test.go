package persistence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func TestOAuthIdentityRepositoryLinkFindTouch(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewOAuthIdentityRepository(tx)
	ctx := t.Context()
	userID := insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Second)
	identity := authdomain.OAuthIdentity{Provider: "google", Subject: uniqueSlug("sub"), Email: "ada@example.test"}

	_, err := repo.FindUser(ctx, identity.Provider, identity.Subject)
	require.ErrorIs(t, err, authdomain.ErrOAuthNoAccount)

	require.NoError(t, repo.Link(ctx, userID, identity, now))

	got, err := repo.FindUser(ctx, identity.Provider, identity.Subject)
	require.NoError(t, err)
	require.Equal(t, userID, got)

	later := now.Add(time.Hour)
	require.NoError(t, repo.Touch(ctx, identity.Provider, identity.Subject, later))

	var model OAuthIdentityModel
	require.NoError(t, tx.Where("provider = ? AND subject = ?", identity.Provider, identity.Subject).Take(&model).Error)
	require.NotNil(t, model.LastUsedAt)
	require.True(t, later.Equal(model.LastUsedAt.UTC()))

	_, err = repo.FindUser(ctx, "github", identity.Subject)
	require.ErrorIs(t, err, authdomain.ErrOAuthNoAccount, "subjects are per provider")

	require.NoError(t, tx.Exec("UPDATE users SET deleted_at = now() WHERE uuid = ?", userID).Error)

	_, err = repo.FindUser(ctx, identity.Provider, identity.Subject)
	require.ErrorIs(t, err, authdomain.ErrOAuthNoAccount, "deleted users cannot sign in")
}

func TestOAuthIdentityRepositoryRejectsDuplicateLinks(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewOAuthIdentityRepository(tx)
	ctx := t.Context()
	first, second := insertUser(t, tx), insertUser(t, tx)
	now := time.Now().UTC()
	identity := authdomain.OAuthIdentity{Provider: "github", Subject: uniqueSlug("sub")}

	require.NoError(t, repo.Link(ctx, first, identity, now))
	require.ErrorIs(t, repo.Link(ctx, second, identity, now), authdomain.ErrOAuthLinkConflict,
		"one provider identity belongs to one user")

	other := authdomain.OAuthIdentity{Provider: "github", Subject: uniqueSlug("sub")}
	require.ErrorIs(t, repo.Link(ctx, first, other, now), authdomain.ErrOAuthLinkConflict,
		"a user links one identity per provider")
}
