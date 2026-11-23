package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/migrations"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database schema (GORM AutoMigrate)",
	}
	cmd.AddCommand(
		newMigrateActionCmd("up", migrations.Up),
		newMigrateActionCmd("down", migrations.Down),
		newMigrateActionCmd("status", migrations.Status),
	)

	return cmd
}

func newMigrateActionCmd(name string, action func(*gorm.DB) error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: name + " database schema",
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

			gormDB := db.DB()
			if gormDB == nil {
				return fmt.Errorf("gorm db not initialized")
			}

			return action(gormDB)
		},
	}
}
