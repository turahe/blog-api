package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/adapters/inbound/scheduler"
	"github.com/turahe/blog-api/internal/adapters/outbound/cache"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/bootstrap"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	"github.com/turahe/blog-api/internal/core/event"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	redisplatform "github.com/turahe/blog-api/internal/platform/redis"
)

const (
	// schedulerTick is how often each job is checked for being due.
	schedulerTick = time.Minute
	// sessionRetention keeps expired refresh sessions for incident review before deletion.
	sessionRetention = 30 * 24 * time.Hour
	// resetTokenRetention keeps expired reset and verification tokens before deletion.
	resetTokenRetention = 7 * 24 * time.Hour
	// privacyBatch bounds how many export and erasure requests one run processes.
	privacyBatch = 10
	// impersonationBatch bounds how many expired impersonation sessions one run closes.
	impersonationBatch = 500
	// newsletterReleaseBatch bounds how many due scheduled issues one run queues.
	newsletterReleaseBatch = 50
	// newsletterTokenRetention keeps expired newsletter tokens so old links still say "expired".
	newsletterTokenRetention = 30 * 24 * time.Hour
	// mediaPurgeBatch bounds how many abandoned and how many deleted assets one run purges.
	mediaPurgeBatch = 200
	// analyticsRollupEvery is how stale today's analytics rollups may get.
	analyticsRollupEvery = 15 * time.Minute
	// analyticsExportBatch bounds how many analytics exports one run builds.
	analyticsExportBatch = 2
	// analyticsRetentionEvery is how often expired analytics data is pruned.
	analyticsRetentionEvery = time.Hour
)

func newSchedulerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Run recurring background jobs",
		Long: "Run recurring jobs (pruning and retention). Any number of replicas can run; " +
			"each job runs once per interval across all of them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return withScheduler(ctx, func(s *scheduler.Scheduler, _ *persistence.JobRunRepository, logger *slog.Logger) error {
				names := make([]string, 0, len(s.Jobs()))
				for _, job := range s.Jobs() {
					names = append(names, job.Name)
				}

				logger.Info("scheduler started", "jobs", names)
				s.Run(ctx)

				return nil
			})
		},
	}
	cmd.AddCommand(newSchedulerRunCmd(), newSchedulerStatusCmd())

	return cmd
}

func newSchedulerRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <job>",
		Short: "Run one job now, ignoring its interval",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withScheduler(cmd.Context(), func(s *scheduler.Scheduler, _ *persistence.JobRunRepository, _ *slog.Logger) error {
				ran, err := s.RunNow(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				if !ran {
					return fmt.Errorf("job %s is running in another process", args[0])
				}

				_, err = fmt.Fprintf(cmd.OutOrStdout(), "job %s finished\n", args[0])

				return err
			})
		},
	}
}

func newSchedulerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the last run of each job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withScheduler(cmd.Context(), func(s *scheduler.Scheduler, repo *persistence.JobRunRepository, _ *slog.Logger) error {
				runs, err := repo.List(cmd.Context())
				if err != nil {
					return err
				}

				return printJobRuns(cmd, s.Jobs(), runs)
			})
		},
	}
}

func printJobRuns(cmd *cobra.Command, jobs []scheduler.Job, runs []persistence.JobRun) error {
	byName := make(map[string]persistence.JobRun, len(runs))
	for _, run := range runs {
		byName[run.Name] = run
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "JOB\tEVERY\tLAST STARTED\tRUNS\tLAST ERROR")

	for _, job := range jobs {
		started, count, lastErr := "never", int64(0), ""
		if run, ok := byName[job.Name]; ok {
			started, count = run.LastStartedAt.UTC().Format(time.RFC3339), run.Runs
			if run.LastError != nil {
				lastErr = *run.LastError
			}
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", job.Name, job.Every, started, count, lastErr)
	}

	return w.Flush()
}

func withScheduler(
	ctx context.Context, fn func(*scheduler.Scheduler, *persistence.JobRunRepository, *slog.Logger) error,
) (err error) {
	cfg, err := config.LoadBackground()
	if err != nil {
		return err
	}

	logger, flush, err := bootstrap.NewLogger(cfg, version)
	if err != nil {
		return err
	}
	defer flush()

	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	readCache, closeCache := schedulerCache(ctx, cfg, logger)
	defer closeCache()

	repo := persistence.NewJobRunRepository(db.GORM)
	jobs := scheduledJobs(ctx, cfg, db, readCache, logger)

	return fn(scheduler.New(repo, schedulerTick, logger, jobs...), repo, logger)
}

// schedulerCache connects to the public read cache so erasures can invalidate it. It returns a
// nil cache when caching is disabled or Redis is unreachable; cached reads then expire by TTL.
func schedulerCache(ctx context.Context, cfg config.Config, logger *slog.Logger) (readcache.Cache, func()) {
	if !cfg.CacheEnabled {
		return nil, func() {}
	}

	client, err := redisplatform.Open(ctx, cfg.RedisURL())
	if err != nil {
		logger.Warn("read cache unavailable; erasures will not invalidate cached reads", "error", err)
		return nil, func() {}
	}

	return cache.NewRedis(client, bootstrap.CacheTTLs(cfg), logger), func() { _ = client.Close() }
}

// scheduledJobs lists the recurring jobs. Names are stable: they key locks and run history.
func scheduledJobs(
	ctx context.Context, cfg config.Config, db *database.Database, readCache readcache.Cache, logger *slog.Logger,
) []scheduler.Job {
	activity := auditservice.NewActivity(persistence.NewAuditRepository(db.GORM))
	processed := persistence.NewProcessedMessageRepository(db.GORM)
	sessions := persistence.NewSessionRepository(db.GORM)
	resets := persistence.NewResetTokenRepository(db.GORM)
	registrations := persistence.NewRegistrationRepository(db.GORM)
	newsletter := bootstrap.NewNewsletterService(cfg, db, bootstrap.NewEvents(cfg, db), nil, logger)
	privacy := bootstrap.NewPrivacyService(ctx, cfg, db, nil,
		event.Unit{Tx: persistence.NewTransactor(db.GORM)}, readCache, logger).WithModuleErasers(newsletter)
	impersonation := bootstrap.NewImpersonationService(cfg, db, bootstrap.NewEvents(cfg, db), nil, nil)

	jobs := []scheduler.Job{
		{Name: "audit-prune", Every: time.Hour, Run: func(ctx context.Context) error {
			return logPruned(ctx, logger, "audit entries")(activity.Prune(ctx, cfg.AuditRetention()))
		}},
		{Name: "processed-messages-prune", Every: time.Hour, Run: func(ctx context.Context) error {
			return logPruned(ctx, logger, "processed messages")(
				processed.PruneBefore(ctx, time.Now().Add(-cfg.ConsumerDedupeRetention)))
		}},
		{Name: "auth-tokens-prune", Every: time.Hour, Run: func(ctx context.Context) error {
			sessionsErr := logPruned(ctx, logger, "refresh sessions")(
				sessions.PruneExpiredBefore(ctx, time.Now().Add(-sessionRetention)))
			resetsErr := logPruned(ctx, logger, "reset tokens")(
				resets.PruneExpiredBefore(ctx, time.Now().Add(-resetTokenRetention)))
			registrationsErr := logPruned(ctx, logger, "expired registrations")(
				registrations.PruneExpired(ctx, time.Now()))

			return errors.Join(sessionsErr, resetsErr, registrationsErr)
		}},
		{Name: "privacy-requests", Every: time.Minute, Run: func(ctx context.Context) error {
			done, err := privacy.ProcessPending(ctx, privacyBatch)
			if done > 0 {
				logger.InfoContext(ctx, "processed privacy requests", "count", done)
			}

			purged, purgeErr := privacy.PurgeExpired(ctx)

			return errors.Join(err, logPruned(ctx, logger, "export archives")(int64(purged), purgeErr))
		}},
		{Name: "impersonation-expire", Every: time.Minute, Run: func(ctx context.Context) error {
			closed, err := impersonation.ExpireStale(ctx, impersonationBatch)

			return logPruned(ctx, logger, "expired impersonation sessions")(int64(closed), err)
		}},
		{Name: "newsletter-release", Every: time.Minute, Run: func(ctx context.Context) error {
			released, err := newsletter.ReleaseDue(ctx, newsletterReleaseBatch)
			if released > 0 {
				logger.InfoContext(ctx, "queued scheduled newsletter issues", "count", released)
			}

			return err
		}},
		{Name: "newsletter-tokens-prune", Every: time.Hour, Run: func(ctx context.Context) error {
			return logPruned(ctx, logger, "newsletter tokens")(newsletter.PruneTokens(ctx, newsletterTokenRetention))
		}},
	}

	jobs = append(jobs, analyticsJobs(ctx, cfg, db, logger)...)

	return append(jobs, mediaJobs(ctx, cfg, db, logger)...)
}

// analyticsJobs returns the rollup builder, the export builder, and analytics retention.
func analyticsJobs(ctx context.Context, cfg config.Config, db *database.Database, logger *slog.Logger) []scheduler.Job {
	aggregator := bootstrap.NewAnalyticsAggregator(db)
	exports := bootstrap.NewAnalyticsExports(ctx, cfg, db, nil, logger)
	retention := bootstrap.NewAnalyticsRetention(db)

	return []scheduler.Job{
		{Name: "analytics-rollup", Every: analyticsRollupEvery, Run: func(ctx context.Context) error {
			result, err := aggregator.Run(ctx)
			if result.Rebuilt {
				logger.InfoContext(ctx, "rebuilt analytics rollups", "periods", result.Periods, "cohorts", result.Cohorts)
			}

			return err
		}},
		{Name: "analytics-exports", Every: time.Minute, Run: func(ctx context.Context) error {
			done, err := exports.ProcessPending(ctx, analyticsExportBatch)
			if done > 0 {
				logger.InfoContext(ctx, "built analytics exports", "count", done)
			}

			purged, purgeErr := exports.PurgeExpired(ctx)

			return errors.Join(err, logPruned(ctx, logger, "analytics export archives")(int64(purged), purgeErr))
		}},
		{Name: "analytics-retention", Every: analyticsRetentionEvery, Run: func(ctx context.Context) error {
			result, err := retention.Run(ctx)
			if result.RawDeleted > 0 || result.RollupsDeleted > 0 {
				logger.InfoContext(ctx, "pruned analytics data", "raw_events", result.RawDeleted,
					"raw_before", result.RawBefore, "daily_rollups", result.RollupsDeleted, "rollups_before", result.RollupsBefore)
			}

			return err
		}},
	}
}

// mediaJobs returns the media orphan cleanup when media storage is configured.
func mediaJobs(ctx context.Context, cfg config.Config, db *database.Database, logger *slog.Logger) []scheduler.Job {
	janitor, err := bootstrap.NewMediaJanitor(ctx, cfg, db)
	if err != nil {
		logger.Warn("media orphan cleanup disabled", "error", err)
		return nil
	}

	if janitor == nil {
		return nil
	}

	policy := mediaservice.PurgePolicy{TrashRetention: cfg.MediaPurgeAfter, Limit: mediaPurgeBatch}

	return []scheduler.Job{{Name: "media-orphans", Every: time.Hour, Run: func(ctx context.Context) error {
		result, err := janitor.PurgeOrphans(ctx, policy)
		if result.Abandoned+result.Trashed > 0 {
			logger.InfoContext(ctx, "purged orphan media",
				"abandoned", result.Abandoned, "trashed", result.Trashed, "bytes", result.Bytes)
		}

		return err
	}}}
}

func logPruned(ctx context.Context, logger *slog.Logger, what string) func(int64, error) error {
	return func(removed int64, err error) error {
		if err == nil && removed > 0 {
			logger.InfoContext(ctx, "pruned "+what, "count", removed)
		}

		return err
	}
}
