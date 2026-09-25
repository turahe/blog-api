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
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/bootstrap"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

const (
	// schedulerTick is how often each job is checked for being due.
	schedulerTick = time.Minute
	// sessionRetention keeps expired refresh sessions for incident review before deletion.
	sessionRetention = 30 * 24 * time.Hour
	// resetTokenRetention keeps expired reset and verification tokens before deletion.
	resetTokenRetention = 7 * 24 * time.Hour
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
	cfg, err := config.Load()
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

	repo := persistence.NewJobRunRepository(db.GORM)

	return fn(scheduler.New(repo, schedulerTick, logger, scheduledJobs(cfg, db, logger)...), repo, logger)
}

// scheduledJobs lists the recurring jobs. Names are stable: they key locks and run history.
func scheduledJobs(cfg config.Config, db *database.Database, logger *slog.Logger) []scheduler.Job {
	activity := auditservice.NewActivity(persistence.NewAuditRepository(db.GORM))
	processed := persistence.NewProcessedMessageRepository(db.GORM)
	sessions := persistence.NewSessionRepository(db.GORM)
	resets := persistence.NewResetTokenRepository(db.GORM)

	return []scheduler.Job{
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

			return errors.Join(sessionsErr, resetsErr)
		}},
	}
}

func logPruned(ctx context.Context, logger *slog.Logger, what string) func(int64, error) error {
	return func(removed int64, err error) error {
		if err == nil && removed > 0 {
			logger.InfoContext(ctx, "pruned "+what, "count", removed)
		}

		return err
	}
}
