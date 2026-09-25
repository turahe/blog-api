package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestJobRunsOncePerIntervalAndRecordsErrors(t *testing.T) {
	t.Parallel()

	repo := NewJobRunRepository(integrationTx(t))
	name := "test-" + uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	failed := errors.New("prune failed")

	var calls int

	run := func(err error) func(context.Context) error {
		return func(context.Context) error {
			calls++

			return err
		}
	}

	ran, err := repo.TryRun(t.Context(), name, time.Hour, now, run(nil))
	require.NoError(t, err)
	require.True(t, ran)

	ran, err = repo.TryRun(t.Context(), name, time.Hour, now.Add(30*time.Minute), run(nil))
	require.NoError(t, err)
	require.False(t, ran, "not due until an hour after the last start")

	ran, err = repo.TryRun(t.Context(), name, 0, now.Add(30*time.Minute), run(failed))
	require.ErrorIs(t, err, failed)
	require.True(t, ran, "every <= 0 forces a run")

	ran, err = repo.TryRun(t.Context(), name, time.Hour, now.Add(90*time.Minute), run(nil))
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, 3, calls)

	runs, err := repo.List(t.Context())
	require.NoError(t, err)

	var got *JobRun

	for i := range runs {
		if runs[i].Name == name {
			got = &runs[i]
		}
	}

	require.NotNil(t, got)
	require.EqualValues(t, 3, got.Runs)
	require.True(t, got.LastStartedAt.Equal(now.Add(90*time.Minute)))
	require.NotNil(t, got.LastFinishedAt)
	require.Nil(t, got.LastError, "a successful run clears the last error")
}

func TestJobRunSkipsWhileAnotherConnectionHoldsTheLock(t *testing.T) {
	t.Parallel()

	integrationTx(t)

	name := "test-" + uuid.NewString()

	t.Cleanup(func() {
		integrationGorm.Exec(`DELETE FROM scheduled_job_runs WHERE name = ?`, name)
	})

	holder, other := NewJobRunRepository(integrationGorm), NewJobRunRepository(integrationGorm)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)

	go func() {
		_, err := holder.TryRun(t.Context(), name, time.Hour, time.Now(), func(context.Context) error {
			close(started)
			<-release

			return nil
		})
		done <- err
	}()

	<-started

	ran, err := other.TryRun(t.Context(), name, 0, time.Now(), func(context.Context) error {
		t.Error("ran while another replica held the job")

		return nil
	})
	require.NoError(t, err)
	require.False(t, ran)

	close(release)
	require.NoError(t, <-done)

	ran, err = other.TryRun(t.Context(), name, 0, time.Now(), func(context.Context) error { return nil })
	require.NoError(t, err)
	require.True(t, ran, "the lock is released after the run")
}

func TestPruneExpiredAuthTokens(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	user := insertUser(t, tx)
	sessions, resets := NewSessionRepository(tx), NewResetTokenRepository(tx)

	require.NoError(t, tx.Exec(`
		INSERT INTO refresh_sessions (user_id, family_id, token_hash, expires_at)
		SELECT id, gen_random_uuid(), 'old-'||gen_random_uuid(), now() - interval '40 days' FROM users WHERE uuid = ?
		UNION ALL
		SELECT id, gen_random_uuid(), 'live-'||gen_random_uuid(), now() + interval '1 day' FROM users WHERE uuid = ?`,
		user, user).Error)
	require.NoError(t, tx.Exec(`
		INSERT INTO password_reset_tokens (user_id, jti, token_hash, expires_at)
		SELECT id, gen_random_uuid()::text, 'old-'||gen_random_uuid(), now() - interval '10 days' FROM users WHERE uuid = ?
		UNION ALL
		SELECT id, gen_random_uuid()::text, 'live-'||gen_random_uuid(), now() + interval '1 hour' FROM users WHERE uuid = ?`,
		user, user).Error)

	removed, err := sessions.PruneExpiredBefore(t.Context(), time.Now().Add(-30*24*time.Hour))
	require.NoError(t, err)
	require.GreaterOrEqual(t, removed, int64(1))

	removed, err = resets.PruneExpiredBefore(t.Context(), time.Now().Add(-7*24*time.Hour))
	require.NoError(t, err)
	require.GreaterOrEqual(t, removed, int64(1))

	var left []string
	require.NoError(t, tx.Raw(`
		SELECT split_part(token_hash, '-', 1) FROM refresh_sessions WHERE user_id = (SELECT id FROM users WHERE uuid = ?)
		UNION ALL
		SELECT split_part(token_hash, '-', 1) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE uuid = ?)`,
		user, user).Scan(&left).Error)
	require.Equal(t, []string{"live", "live"}, left)
}
