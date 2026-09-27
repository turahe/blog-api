package cmd

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/cache"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
)

func TestReportCache(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		CacheTTLPosts: time.Minute, CacheTTLCategories: 2 * time.Minute, CacheTTLTags: 3 * time.Minute,
		CacheTTLUsers: 4 * time.Minute, CacheTTLSettings: 5 * time.Minute,
	}

	runtimeWith := func(t *testing.T, server *miniredis.Miniredis) *bootstrap.Runtime {
		t.Helper()

		client := goredis.NewClient(&goredis.Options{Addr: server.Addr(), MaxRetries: -1})
		t.Cleanup(func() { _ = client.Close() })

		return &bootstrap.Runtime{Config: cfg, Cache: cache.NewRedis(client, bootstrap.CacheTTLs(cfg), slog.New(slog.DiscardHandler))}
	}

	tests := []struct {
		name    string
		runtime func(t *testing.T) *bootstrap.Runtime
		want    string
		wantErr string
	}{
		{
			name:    "bypassed when caching is disabled",
			runtime: func(*testing.T) *bootstrap.Runtime { return &bootstrap.Runtime{} },
			want:    "cache: bypassed (CACHE_ENABLED=false)\n",
		},
		{
			name:    "reports the ttl of every family",
			runtime: func(t *testing.T) *bootstrap.Runtime { return runtimeWith(t, miniredis.RunT(t)) },
			want:    "cache: ok (ttl posts=1m0s categories=2m0s tags=3m0s users=4m0s settings=5m0s)\n",
		},
		{
			name: "fails when the probe cannot reach redis",
			runtime: func(t *testing.T) *bootstrap.Runtime {
				server := miniredis.RunT(t)
				app := runtimeWith(t, server)
				server.Close()

				return app
			},
			wantErr: "cache: cache probe write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := new(bytes.Buffer)
			cmd := &cobra.Command{}
			cmd.SetOut(out)
			cmd.SetContext(t.Context())

			err := reportCache(cmd, tt.runtime(t))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, out.String())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, out.String())
		})
	}
}
