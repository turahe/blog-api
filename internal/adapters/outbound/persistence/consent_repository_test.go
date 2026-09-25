package persistence

import (
	"testing"

	"github.com/stretchr/testify/require"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	"github.com/turahe/blog-api/internal/platform/system"
)

func TestConsentRepositoryStoreWithdrawAndErase(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewConsentRepository(tx)
	svc := consentservice.New(repo, system.UUIDGenerator{}, system.Clock{})
	user := insertUser(t, tx)

	state, err := svc.Store(ctx, "", &user, map[consentdomain.Purpose]bool{
		consentdomain.PurposeAnalytics: true, consentdomain.PurposeAuthenticatedAnalytics: true,
	}, "2026-09")
	require.NoError(t, err)
	require.NotEmpty(t, state.Token)
	require.Len(t, state.Consents, 2)

	current, err := svc.Current(ctx, state.Token)
	require.NoError(t, err)
	require.Equal(t, &user, current.Subject.UserUUID)
	require.Len(t, current.Consents, 2)
	require.Equal(t, consentdomain.PurposeAnalytics, current.Consents[0].Purpose)

	consent, subject, err := repo.Get(ctx, current.Consents[0].UUID)
	require.NoError(t, err)
	require.Equal(t, consentdomain.StatusGranted, consent.Status)
	require.Equal(t, state.Subject.UUID, subject.UUID)

	withdrawn, err := svc.Withdraw(ctx, state.Token, nil, consent.UUID)
	require.NoError(t, err)
	require.Equal(t, consentdomain.StatusWithdrawn, withdrawn.Status)
	require.NotNil(t, withdrawn.WithdrawnAt)

	current, err = svc.Current(ctx, state.Token)
	require.NoError(t, err)
	require.Nil(t, current.Subject.UserUUID, "withdrawing analytics unlinks the user")

	_, err = svc.Store(ctx, state.Token, &user, map[consentdomain.Purpose]bool{
		consentdomain.PurposeAnalytics: true, consentdomain.PurposeAuthenticatedAnalytics: true,
	}, "2026-10")
	require.NoError(t, err)

	removed, err := svc.DeleteForUser(ctx, user)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	_, err = svc.Current(ctx, state.Token)
	require.ErrorIs(t, err, consentdomain.ErrNotFound)
}
