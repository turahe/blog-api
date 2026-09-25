package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Manage the audit log",
	}
	cmd.AddCommand(newAuditPruneCmd())

	return cmd
}

func newAuditPruneCmd() *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete audit entries older than the retention period",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			cfg, err := config.LoadBackground()
			if err != nil {
				return err
			}

			retention := cfg.AuditRetention()
			if days > 0 {
				retention = time.Duration(days) * 24 * time.Hour
			}

			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, db.Close()) }()

			deleted, err := auditservice.NewActivity(persistence.NewAuditRepository(db.GORM)).Prune(cmd.Context(), retention)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "deleted %d audit entries older than %s\n", deleted, retention)

			return err
		},
	}
	cmd.Flags().IntVar(&days, "older-than-days", 0, "retention in days (default AUDIT_RETENTION_DAYS)")

	return cmd
}
