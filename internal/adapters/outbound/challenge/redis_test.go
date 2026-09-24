package challenge

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	return New(client), server
}

func TestStoreRoundTripAndConsume(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	ctx := t.Context()
	login := authdomain.PendingLogin{UserUUID: uuid.New(), Remember: true, UserAgent: "ua", IPAddress: "10.0.0.1"}

	require.NoError(t, store.Save(ctx, "h1", login, time.Minute))

	got, err := store.Get(ctx, "h1")
	require.NoError(t, err)
	require.Equal(t, login, got)

	for want := 1; want <= 3; want++ {
		n, err := store.Attempt(ctx, "h1")
		require.NoError(t, err)
		require.Equal(t, want, n)
	}

	consumed, err := store.Consume(ctx, "h1")
	require.NoError(t, err)
	require.True(t, consumed)

	consumed, err = store.Consume(ctx, "h1")
	require.NoError(t, err)
	require.False(t, consumed)

	_, err = store.Get(ctx, "h1")
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid)
}

func TestStoreExpiresAndAttemptDoesNotResurrect(t *testing.T) {
	t.Parallel()

	store, server := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Save(ctx, "h2", authdomain.PendingLogin{UserUUID: uuid.New()}, time.Minute))
	server.FastForward(2 * time.Minute)

	_, err := store.Get(ctx, "h2")
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid)

	_, err = store.Attempt(ctx, "h2")
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid)
	require.False(t, server.Exists(keyPrefix+"h2"), "an attempt must not recreate an expired challenge")
}
