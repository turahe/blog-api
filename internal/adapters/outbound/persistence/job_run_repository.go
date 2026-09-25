package persistence

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"gorm.io/gorm"
)

// jobLockNamespace keeps scheduler advisory lock keys apart from any other advisory locks.
const jobLockNamespace = "blog-api:job:"

// JobRun is the last recorded run of a scheduled job.
type JobRun struct {
	Name           string
	LastStartedAt  time.Time
	LastFinishedAt *time.Time
	LastError      *string
	Runs           int64
}

// JobRunRepository runs scheduled jobs at most once per interval across replicas.
type JobRunRepository struct {
	db *gorm.DB
}

// NewJobRunRepository returns a JobRunRepository over db.
func NewJobRunRepository(db *gorm.DB) *JobRunRepository {
	return &JobRunRepository{db: db}
}

// TryRun runs fn when no other process holds the job's advisory lock and the last run started
// at least every ago (every <= 0 forces a run). It reports whether fn ran; fn's error is
// recorded in scheduled_job_runs and returned.
func (r *JobRunRepository) TryRun(
	ctx context.Context, name string, every time.Duration, now time.Time, fn func(ctx context.Context) error,
) (ran bool, err error) {
	err = r.pinned(ctx, func(c *gorm.DB) error {
		var locked bool
		if err := c.Raw(`SELECT pg_try_advisory_lock(?)`, jobLockKey(name)).Scan(&locked).Error; err != nil {
			return fmt.Errorf("lock job %s: %w", name, err)
		}

		if !locked {
			return nil
		}

		defer c.WithContext(context.WithoutCancel(ctx)).Exec(`SELECT pg_advisory_unlock(?)`, jobLockKey(name))

		due, err := r.due(c, name, every, now)
		if err != nil || !due {
			return err
		}

		if err := c.Exec(`
			INSERT INTO scheduled_job_runs (name, last_started_at) VALUES (?, ?)
			ON CONFLICT (name) DO UPDATE SET last_started_at = EXCLUDED.last_started_at`, name, now).Error; err != nil {
			return fmt.Errorf("start job %s: %w", name, err)
		}

		ran = true
		runErr := fn(ctx)

		var failure *string

		if runErr != nil {
			msg := lastError(runErr)
			failure = &msg
		}

		if err := c.WithContext(context.WithoutCancel(ctx)).Exec(`
			UPDATE scheduled_job_runs SET last_finished_at = now(), last_error = ?, runs = runs + 1
			WHERE name = ?`, failure, name).Error; err != nil {
			return errors.Join(runErr, fmt.Errorf("finish job %s: %w", name, err))
		}

		return runErr
	})

	return ran, err
}

// List returns every recorded job run, by name.
func (r *JobRunRepository) List(ctx context.Context) ([]JobRun, error) {
	var runs []JobRun
	if err := r.db.WithContext(ctx).Raw(`
		SELECT name, last_started_at, last_finished_at, last_error, runs
		FROM scheduled_job_runs ORDER BY name`).Scan(&runs).Error; err != nil {
		return nil, fmt.Errorf("list job runs: %w", err)
	}

	return runs, nil
}

func (r *JobRunRepository) due(c *gorm.DB, name string, every time.Duration, now time.Time) (bool, error) {
	if every <= 0 {
		return true, nil
	}

	var last []time.Time
	if err := c.Raw(`SELECT last_started_at FROM scheduled_job_runs WHERE name = ?`, name).Scan(&last).Error; err != nil {
		return false, fmt.Errorf("read job %s: %w", name, err)
	}

	return len(last) == 0 || !now.Before(last[0].Add(every)), nil
}

// pinned runs fn on one connection, because advisory locks belong to a session. Inside a
// transaction the transaction's connection is already pinned.
func (r *JobRunRepository) pinned(ctx context.Context, fn func(c *gorm.DB) error) error {
	db := r.db.WithContext(ctx)
	if _, inTx := db.Statement.ConnPool.(gorm.TxCommitter); inTx {
		return fn(db)
	}

	return db.Connection(fn)
}

func jobLockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(jobLockNamespace + name))

	return int64(h.Sum64()) //nolint:gosec // wraps on purpose: advisory locks take a signed bigint
}
