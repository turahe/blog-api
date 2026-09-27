package challenge

import (
	"context"
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

func TestStoreGetRejectsCorruptRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{name: "invalid json", payload: "{nope", wantErr: "decode challenge:"},
		{name: "invalid user uuid", payload: `{"user_uuid":"not-a-uuid"}`, wantErr: "decode challenge user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, server := newTestStore(t)
			server.HSet(keyPrefix+"h", fieldLogin, tt.payload)

			_, err := store.Get(t.Context(), "h")
			require.ErrorContains(t, err, tt.wantErr)
			require.NotErrorIs(t, err, authdomain.ErrChallengeInvalid)
		})
	}
}

func TestStoreSurfacesRedisErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(ctx context.Context, store *Store) error
	}{
		{name: "get", call: func(ctx context.Context, store *Store) error {
			_, err := store.Get(ctx, "h")
			return err
		}},
		{name: "attempt", call: func(ctx context.Context, store *Store) error {
			_, err := store.Attempt(ctx, "h")
			return err
		}},
		{name: "consume", call: func(ctx context.Context, store *Store) error {
			consumed, err := store.Consume(ctx, "h")
			require.False(t, consumed)

			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, server := newTestStore(t)
			server.SetError("ERR injected")

			err := tt.call(t.Context(), store)
			require.ErrorContains(t, err, "injected")
			require.NotErrorIs(t, err, authdomain.ErrChallengeInvalid)
		})
	}
}
