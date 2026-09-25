package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

func newAnalyticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Manage analytics rollups",
	}
	cmd.AddCommand(newAnalyticsRollupCmd())

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
