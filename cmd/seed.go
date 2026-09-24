package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/seed"
)

func newSeedCmd() *cobra.Command {
	var (
		email    string
		username string
		password string
		name     string
	)

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed roles, permissions, and an initial administrator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, db.Close()) }()

			if err := seed.Run(cmd.Context(), db.GORM, seed.Options{
				AdminEmail:    email,
				AdminUsername: username,
				AdminPassword: password,
				AdminName:     name,
			}); err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), "seed completed")

			return err
		},
	}
	cmd.Flags().StringVar(&email, "email", "admin@example.com", "admin email")
	cmd.Flags().StringVar(&username, "username", "admin", "admin username")
	cmd.Flags().StringVar(&password, "password", "ChangeMeNow!123", "admin password")
	cmd.Flags().StringVar(&name, "name", "Administrator", "admin display name")

	return cmd
}
