package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/ingestload"
)

func newAnalyticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Manage analytics rollups and load-test ingest",
	}
	cmd.AddCommand(newAnalyticsRollupCmd(), newAnalyticsLoadtestCmd())

	return cmd
}

func newAnalyticsRollupCmd() *cobra.Command {
	var from, to string

	cmd := &cobra.Command{
		Use:   "rollup",
		Short: "Recompute analytics rollups for a range of days",
		Long: "Recompute the daily, weekly, and monthly rollups overlapping --from through --to (dates in the " +
			"site.timezone setting) and the retention cohorts that change within them. The analytics-rollup job " +
			"in app scheduler already keeps recent periods current; use this after restoring raw events or " +
			"fixing a failed run. Days before the oldest raw event are skipped, but a week or month that " +
			"started before it is recomputed from the events that remain.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			first, last, err := rollupRange(from, to)
			if err != nil {
				return err
			}

			cfg, err := config.LoadBackground()
			if err != nil {
				return err
			}

			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, db.Close()) }()

			result, err := bootstrap.NewAnalyticsAggregator(db).Backfill(cmd.Context(), first, last)
			if err != nil {
				return err
			}

			what := "recomputed"
			if result.Rebuilt {
				what = "rebuilt every period the raw events cover (first run or new time zone):"
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %d periods and %d cohorts\n", what, result.Periods, result.Cohorts)

			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "first day, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&to, "to", "", "last day, YYYY-MM-DD (default: today)")
	_ = cmd.MarkFlagRequired("from")

	return cmd
}

// rollupRange parses the --from and --to dates; an empty to means "through today".
func rollupRange(from, to string) (time.Time, time.Time, error) {
	first, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("--from: %w", err)
	}

	last := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

	if to != "" {
		if last, err = time.Parse(time.DateOnly, to); err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--to: %w", err)
		}
	}

	if last.Before(first) {
		return time.Time{}, time.Time{}, errors.New("--to is before --from")
	}

	return first, last, nil
}

// metricsSettle is how long the load test waits for the API's writer to flush before it
// reads the drop counter again.
const metricsSettle = 3 * time.Second

func newAnalyticsLoadtestCmd() *cobra.Command {
	var (
		cfg        ingestload.Config
		metricsURL string
		maxP99     time.Duration
		minRatio   float64
	)

	cmd := &cobra.Command{
		Use:   "loadtest",
		Short: "Send analytics ingest traffic at a fixed rate and check the API keeps up",
		Long: "Send --rate events per second for --duration to the five public ingest routes of the API at --url " +
			"(40% page views, 25% heartbeats, 20% navigation, 10% searches, 5% result clicks) and report " +
			"accepted events, statuses, and latency percentiles. It fails when any response is not 202, the " +
			"accepted rate falls under --min-ratio of --rate, p99 exceeds --max-p99, or, with --metrics-url, " +
			"the API dropped events. Events are stored for real: run it against a disposable stack, with " +
			"ANALYTICS_INGEST_PER_MINUTE=0 or --spread behind a trusted proxy, and delete the rows under the " +
			"reported path prefix afterwards.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLoadtest(cmd, cfg, metricsURL, maxP99, minRatio)
		},
	}
	cmd.Flags().StringVar(&cfg.BaseURL, "url", "http://127.0.0.1:8080", "API origin")
	cmd.Flags().IntVar(&cfg.Rate, "rate", 500, "events per second")
	cmd.Flags().DurationVar(&cfg.Duration, "duration", time.Minute, "how long to send")
	cmd.Flags().IntVar(&cfg.Workers, "workers", 0, "requests in flight (default rate/10, at least 8)")
	cmd.Flags().IntVar(&cfg.Sessions, "sessions", 200, "simulated visitor sessions")
	cmd.Flags().IntVar(&cfg.Spread, "spread", 0, "X-Forwarded-For addresses to rotate through (0 sends none)")
	cmd.Flags().StringVar(&metricsURL, "metrics-url", "", "Prometheus endpoint of the API (METRICS_ADDR) to check for dropped events")
	cmd.Flags().DurationVar(&maxP99, "max-p99", 250*time.Millisecond, "fail above this p99 latency (0 disables)")
	cmd.Flags().Float64Var(&minRatio, "min-ratio", 0.95, "fail when fewer than this share of --rate events/s are accepted")

	return cmd
}

func runLoadtest(cmd *cobra.Command, cfg ingestload.Config, metricsURL string, maxP99 time.Duration, minRatio float64) error {
	ctx := cmd.Context()
	client := &http.Client{Timeout: 10 * time.Second}

	var droppedBefore float64

	if metricsURL != "" {
		var err error
		if droppedBefore, err = ingestload.DroppedEvents(ctx, client, metricsURL); err != nil {
			return err
		}
	}

	result, err := ingestload.Run(ctx, cfg)
	if err != nil {
		return err
	}

	printLoadtest(cmd.OutOrStdout(), cfg, result)

	checkErr := result.Check(cfg.Rate, minRatio, maxP99)

	if metricsURL != "" {
		time.Sleep(metricsSettle)

		after, err := ingestload.DroppedEvents(ctx, client, metricsURL)
		if err != nil {
			return errors.Join(checkErr, err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dropped by the API: %.0f\n", after-droppedBefore)

		if after > droppedBefore {
			checkErr = errors.Join(checkErr, fmt.Errorf("the API dropped %.0f events", after-droppedBefore))
		}
	}

	return checkErr
}

func printLoadtest(w io.Writer, cfg ingestload.Config, r ingestload.Result) {
	statuses := make([]int, 0, len(r.Statuses))
	for status := range r.Statuses {
		statuses = append(statuses, status)
	}

	sort.Ints(statuses)

	_, _ = fmt.Fprintf(w, "target %d events/s for %s; path prefix %s\n", cfg.Rate, cfg.Duration, r.PathPrefix)
	_, _ = fmt.Fprintf(w, "planned %d, sent %d, skipped %d, errors %d, accepted %d (%.1f events/s)\n",
		r.Planned, r.Sent, r.Skipped, r.Errors, r.Accepted, r.Rate())

	for _, status := range statuses {
		_, _ = fmt.Fprintf(w, "status %d: %d\n", status, r.Statuses[status])
	}

	for _, route := range []string{"page-view", "time-spent", "navigation", "search", "search-click"} {
		_, _ = fmt.Fprintf(w, "accepted %s: %d\n", route, r.Routes[route])
	}

	_, _ = fmt.Fprintf(w, "latency p50 %s, p95 %s, p99 %s, max %s\n", r.P50, r.P95, r.P99, r.Max)
}
