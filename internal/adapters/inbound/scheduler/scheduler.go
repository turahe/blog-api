// Package scheduler runs recurring jobs for app scheduler. Any number of replicas can run:
// the Store makes each job run once per interval across all of them.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Job is a named task run every Every.
type Job struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
}

// Store runs fn unless another replica holds the job or its last run started less than
// every ago; every <= 0 forces a run. It reports whether fn ran.
type Store interface {
	TryRun(ctx context.Context, name string, every time.Duration, now time.Time, fn func(ctx context.Context) error) (bool, error)
}

// ErrUnknownJob is returned by RunNow for a name that is not registered.
var ErrUnknownJob = errors.New("unknown job")

// Scheduler checks each job every tick, or every Every when that is shorter.
type Scheduler struct {
	store  Store
	jobs   []Job
	tick   time.Duration
	logger *slog.Logger
	now    func() time.Time
}

// New returns a Scheduler for jobs.
func New(store Store, tick time.Duration, logger *slog.Logger, jobs ...Job) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Scheduler{store: store, jobs: jobs, tick: tick, logger: logger, now: time.Now}
}

// Jobs returns the registered jobs.
func (s *Scheduler) Jobs() []Job {
	return s.jobs
}

// Run checks every job until ctx ends, then waits for running jobs to return.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup

	for _, job := range s.jobs {
		wg.Go(func() { s.loop(ctx, job) })
	}

	wg.Wait()
}

// RunNow runs the named job once, ignoring its interval but not a run in progress elsewhere.
func (s *Scheduler) RunNow(ctx context.Context, name string) (bool, error) {
	for _, job := range s.jobs {
		if job.Name == name {
			return s.run(ctx, job, 0)
		}
	}

	return false, fmt.Errorf("%w: %s", ErrUnknownJob, name)
}

func (s *Scheduler) loop(ctx context.Context, job Job) {
	interval := s.tick
	if job.Every > 0 && job.Every < interval {
		interval = job.Every
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		_, _ = s.run(ctx, job, job.Every)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) run(ctx context.Context, job Job, every time.Duration) (bool, error) {
	started := s.now()

	ran, err := s.store.TryRun(ctx, job.Name, every, started, job.Run)

	switch {
	case err != nil && ctx.Err() == nil:
		s.logger.ErrorContext(ctx, "scheduled job failed", "job", job.Name, "ran", ran, "error", err)
	case ran:
		s.logger.InfoContext(ctx, "scheduled job finished", "job", job.Name, "duration", time.Since(started))
	}

	return ran, err
}
