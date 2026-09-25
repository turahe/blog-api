package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

func newRegistration(email string, now time.Time) authdomain.Registration {
	return authdomain.Registration{
		UUID: uuid.New(), Email: email, Username: "r" + uuid.NewString()[:8], FullName: "Reader",
		PasswordHash: "hash", TokenHash: "tok-" + uuid.NewString(), ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}
}

func TestRegistrationRepositoryCapsLiveSignUpsPerAddress(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewRegistrationRepository(tx)
	now := time.Now().UTC().Truncate(time.Microsecond)
	email := "reg-" + uuid.NewString() + "@example.com"

	expired := newRegistration(email, now.Add(-48*time.Hour))
	stored, err := repo.Create(t.Context(), expired, 2)
	require.NoError(t, err)
	require.True(t, stored)

	for range 2 {
		stored, err = repo.Create(t.Context(), newRegistration(email, now), 2)
		require.NoError(t, err)
		require.True(t, stored, "expired sign-ups do not count toward the cap")
	}

	stored, err = repo.Create(t.Context(), newRegistration("REG"+email[3:], now), 2)
	require.NoError(t, err)
	assert.False(t, stored, "the cap ignores letter case")
	assert.Equal(t, int64(3), countWhere(t, tx, "registrations", "lower(email) = lower(?)", email))

	pruned, err := repo.PruneExpired(t.Context(), now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pruned)
}

func TestRegistrationRepositoryConsumeDropsTheOtherSignUps(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewRegistrationRepository(tx)
	now := time.Now().UTC().Truncate(time.Microsecond)
	email := "reg-" + uuid.NewString() + "@example.com"
	other := "reg-" + uuid.NewString() + "@example.com"

	used, spare, unrelated := newRegistration(email, now), newRegistration(email, now), newRegistration(other, now)
	for _, r := range []authdomain.Registration{used, spare, unrelated} {
		stored, err := repo.Create(t.Context(), r, 3)
		require.NoError(t, err)
		require.True(t, stored)
	}

	found, err := repo.FindByTokenHash(t.Context(), used.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, used, found)

	consumed, err := repo.Consume(t.Context(), used.TokenHash)
	require.NoError(t, err)
	assert.True(t, consumed)
	assert.Zero(t, countWhere(t, tx, "registrations", "email = ?", email))
	assert.Equal(t, int64(1), countWhere(t, tx, "registrations", "email = ?", other), "other addresses keep theirs")

	consumed, err = repo.Consume(t.Context(), used.TokenHash)
	require.NoError(t, err)
	assert.False(t, consumed, "a token is consumed once")

	_, err = repo.FindByTokenHash(t.Context(), used.TokenHash)
	require.ErrorIs(t, err, authdomain.ErrRegistrationTokenInvalid)
}

func TestUserRepositoryCreateStoresEmailVerification(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	user, err := NewUserRepository(tx).Create(t.Context(), userdomain.User{
		UUID: uuid.New(), Email: "v-" + uuid.NewString() + "@example.com", Username: "v" + uuid.NewString()[:8],
		FullName: "Verified", PasswordHash: "hash", Status: userdomain.StatusActive,
		EmailVerifiedAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	stored, err := NewUserRepository(tx).FindByID(t.Context(), user.UUID)
	require.NoError(t, err)
	require.NotNil(t, stored.EmailVerifiedAt)
	assert.True(t, now.Equal(*stored.EmailVerifiedAt))
}
