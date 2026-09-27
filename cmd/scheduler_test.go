package cmd

import (
	"bytes"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/scheduler"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/platform/config"
)

func TestSchedulerRunRequiresOneJob(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{}, {"a", "b"}} {
		t.Run(strconv.Itoa(len(args))+" args", func(t *testing.T) {
			t.Parallel()

			_, err := runCLI(t, append([]string{"scheduler", "run"}, args...)...)
			require.ErrorContains(t, err, "accepts 1 arg(s)")
		})
	}
}

func TestPrintJobRuns(t *testing.T) {
	t.Parallel()

	failure := "disk full"
	jakarta := time.FixedZone("WIB", 7*3600)
	jobs := []scheduler.Job{
		{Name: "audit-prune", Every: time.Hour},
		{Name: "privacy-requests", Every: time.Minute},
		{Name: "media-orphans", Every: time.Hour},
	}
	runs := []persistence.JobRun{
		{Name: "privacy-requests", LastStartedAt: time.Date(2026, 9, 27, 7, 30, 0, 0, jakarta), Runs: 12, LastError: &failure},
		{Name: "media-orphans", LastStartedAt: time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC), Runs: 3},
		{Name: "retired-job", LastStartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Runs: 99},
	}

	out := new(bytes.Buffer)
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	require.NoError(t, printJobRuns(cmd, jobs, runs))

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 4, "a header and one row per configured job; runs of removed jobs are hidden")
	assert.Equal(t, []string{"JOB", "EVERY", "LAST", "STARTED", "RUNS", "LAST", "ERROR"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"audit-prune", "1h0m0s", "never", "0"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"privacy-requests", "1m0s", "2026-09-27T00:30:00Z", "12", "disk", "full"}, strings.Fields(lines[2]))
	assert.Equal(t, []string{"media-orphans", "1h0m0s", "2026-09-27T01:00:00Z", "3"}, strings.Fields(lines[3]))
}

func TestSchedulerCache(t *testing.T) {
	t.Parallel()

	redisConfig := func(t *testing.T, server *miniredis.Miniredis) config.Config {
		t.Helper()

		port, err := strconv.Atoi(server.Port())
		require.NoError(t, err)

		return config.Config{CacheEnabled: true, RedisHost: server.Host(), RedisPort: port}
	}

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		readCache, closeCache := schedulerCache(t.Context(), config.Config{}, slog.New(slog.DiscardHandler))
		assert.Nil(t, readCache)
		closeCache()
	})

	t.Run("reachable redis", func(t *testing.T) {
		t.Parallel()

		readCache, closeCache := schedulerCache(t.Context(), redisConfig(t, miniredis.RunT(t)), slog.New(slog.DiscardHandler))
		t.Cleanup(closeCache)
		assert.NotNil(t, readCache)
	})

	t.Run("unreachable redis falls back to no cache", func(t *testing.T) {
		t.Parallel()

		server := miniredis.RunT(t)
		cfg := redisConfig(t, server)
		server.Close()

		logs := new(bytes.Buffer)
		readCache, closeCache := schedulerCache(t.Context(), cfg, slog.New(slog.NewTextHandler(logs, nil)))
		closeCache()

		assert.Nil(t, readCache)
		assert.Contains(t, logs.String(), "read cache unavailable")
	})
}

func TestLogPruned(t *testing.T) {
	t.Parallel()

	errPrune := errors.New("prune failed")

	tests := []struct {
		name    string
		removed int64
		err     error
		wantLog bool
	}{
		{name: "logs removed rows", removed: 5, wantLog: true},
		{name: "silent when nothing removed", removed: 0},
		{name: "passes errors through without logging", removed: 5, err: errPrune},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := new(bytes.Buffer)
			logger := slog.New(slog.NewTextHandler(logs, nil))

			err := logPruned(t.Context(), logger, "audit entries")(tt.removed, tt.err)
			require.ErrorIs(t, err, tt.err)

			if tt.wantLog {
				assert.Contains(t, logs.String(), `msg="pruned audit entries" count=5`)
			} else {
				assert.Empty(t, logs.String())
			}
		})
	}
}
