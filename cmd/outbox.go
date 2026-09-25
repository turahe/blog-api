package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

func newOutboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outbox",
		Short: "Inspect and repair the domain event outbox",
	}
	cmd.AddCommand(newOutboxStatusCmd(), newOutboxRetryCmd())

	return cmd
}

func newOutboxStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show pending and failed outbox events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			repo, closeDB, err := openOutbox(cmd)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, closeDB()) }()

			backlog, err := repo.Backlog(cmd.Context())
			if err != nil {
				return err
			}

			lag := time.Duration(0)
			if backlog.OldestPendingAt != nil {
				lag = time.Since(*backlog.OldestPendingAt).Round(time.Second)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "pending %d, failed %d, oldest pending %s ago\n",
				backlog.Pending, backlog.Failed, lag)

			return err
		},
	}
}

func newOutboxRetryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retry",
		Short: "Make failed outbox events due for delivery again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			repo, closeDB, err := openOutbox(cmd)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, closeDB()) }()

			reset, err := repo.Retry(cmd.Context(), time.Now().UTC())
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "requeued %d failed outbox events\n", reset)

			return err
		},
	}
}

func openOutbox(cmd *cobra.Command) (*persistence.OutboxRepository, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}

	db, err := database.Open(cmd.Context(), cfg)
	if err != nil {
		return nil, nil, err
	}

	return persistence.NewOutboxRepository(db.GORM), db.Close, nil
}
