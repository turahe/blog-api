package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check required runtime dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			app, err := bootstrap.NewRuntime(cmd.Context(), cfg, newLogger(cfg.Environment), version)
			if err != nil {
				return err
			}
			defer app.Close()
			fmt.Fprintf(cmd.OutOrStdout(), "database (%s): ok\n", app.Database.Driver)
			fmt.Fprintln(cmd.OutOrStdout(), "redis: ok")
			if !app.Config.MessagingEnabled() {
				fmt.Fprintln(cmd.OutOrStdout(), "messaging: skipped (MESSAGE_BROKER unset)")
				return nil
			}
			bus, err := messaging.Open(cmd.Context(), app.Config)
			if err != nil {
				return fmt.Errorf("messaging: %w", err)
			}
			_ = bus.Close()
			fmt.Fprintf(cmd.OutOrStdout(), "messaging (%s): ok\n", bus.Broker)
			return nil
		},
	}
}
