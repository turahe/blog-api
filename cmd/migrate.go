package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/migrations"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database migrations",
	}
	cmd.AddCommand(
		newMigrateActionCmd("up", migrations.Up),
		newMigrateActionCmd("down", migrations.Down),
		newMigrateActionCmd("status", migrations.Status),
	)

	return cmd
}

func newMigrateActionCmd(name string, action func(*sql.DB) error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: name + " database migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			cfg, err := config.LoadBackground()
			if err != nil {
				return err
			}

			db, err := bootstrap.OpenDatabase(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, db.Close()) }()

			sqlDB, err := db.DB().DB()
			if err != nil {
				return fmt.Errorf("get sql.DB: %w", err)
			}

			return action(sqlDB)
		},
	}
}
