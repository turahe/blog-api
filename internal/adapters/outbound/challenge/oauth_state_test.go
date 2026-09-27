package challenge

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func newTestOAuthStates(t *testing.T) (*OAuthStates, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	return NewOAuthStates(client), server
}

func TestOAuthStatesAreSingleUse(t *testing.T) {
	t.Parallel()

	store, server := newTestOAuthStates(t)
	ctx := t.Context()
	value := authdomain.OAuthState{Provider: "google", RedirectURI: "https://app.test/cb", CodeVerifier: "v"}

	require.NoError(t, store.Save(ctx, "state-1", value, time.Minute))
	require.False(t, server.Exists(oauthStatePrefix+"state-1"), "the raw state is not the key")
	require.True(t, server.Exists(oauthKey("state-1")))

	got, err := store.Consume(ctx, "state-1")
	require.NoError(t, err)
	require.Equal(t, value, got)

	_, err = store.Consume(ctx, "state-1")
	require.ErrorIs(t, err, authdomain.ErrOAuthStateInvalid)

	_, err = store.Consume(ctx, "")
	require.ErrorIs(t, err, authdomain.ErrOAuthStateInvalid)
}

func TestOAuthStatesExpire(t *testing.T) {
	t.Parallel()

	store, server := newTestOAuthStates(t)
	ctx := t.Context()

	require.NoError(t, store.Save(ctx, "state-1", authdomain.OAuthState{Provider: "github"}, time.Minute))
	server.FastForward(time.Minute + time.Second)

	_, err := store.Consume(ctx, "state-1")
	require.ErrorIs(t, err, authdomain.ErrOAuthStateInvalid)
}

func TestOAuthStatesConsumeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		arrange func(server *miniredis.Miniredis)
		wantErr string
	}{
		{
			name:    "redis error",
			arrange: func(server *miniredis.Miniredis) { server.SetError("ERR injected") },
			wantErr: "injected",
		},
		{
			name: "corrupt record",
			arrange: func(server *miniredis.Miniredis) {
				_ = server.Set(oauthKey("state-1"), "{nope")
			},
			wantErr: "decode oauth state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, server := newTestOAuthStates(t)
			tt.arrange(server)

			_, err := store.Consume(t.Context(), "state-1")
			require.ErrorContains(t, err, tt.wantErr)
			require.NotErrorIs(t, err, authdomain.ErrOAuthStateInvalid)
		})
	}
}
