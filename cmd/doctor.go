// Package cmd wires the Cobra CLI (serve, worker, migrate, seed, doctor, version).
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check required runtime dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			logger, flush, err := bootstrap.NewLogger(cfg, version)
			if err != nil {
				return err
			}
			defer flush()

			app, err := bootstrap.NewRuntime(cmd.Context(), cfg, logger, version)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, app.Close()) }()

			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "database (%s): ok\nredis: ok\n", app.Database.Driver); err != nil {
				return err
			}

			if err := reportCache(cmd, app); err != nil {
				return err
			}

			if !app.Config.MessagingEnabled() {
				_, err := fmt.Fprintln(out, "messaging: skipped (MESSAGE_BROKER unset)")

				return err
			}

			bus, err := messaging.Open(cmd.Context(), app.Config, logger)
			if err != nil {
				return fmt.Errorf("messaging: %w", err)
			}

			if err := bus.Close(); err != nil {
				return fmt.Errorf("messaging close: %w", err)
			}

			_, err = fmt.Fprintf(out, "messaging (%s): ok\n", bus.Broker)

			return err
		},
	}
}

// reportCache probes the public read cache, or reports that CACHE_ENABLED=false bypasses it.
func reportCache(cmd *cobra.Command, app *bootstrap.Runtime) error {
	out := cmd.OutOrStdout()
	if app.Cache == nil {
		_, err := fmt.Fprintln(out, "cache: bypassed (CACHE_ENABLED=false)")
		return err
	}

	if err := app.Cache.Probe(cmd.Context()); err != nil {
		return fmt.Errorf("cache: %w", err)
	}

	ttls := bootstrap.CacheTTLs(app.Config)
	parts := make([]string, 0, len(readcache.Families))

	for _, family := range readcache.Families {
		parts = append(parts, fmt.Sprintf("%s=%s", family, ttls[family]))
	}

	_, err := fmt.Fprintf(out, "cache: ok (ttl %s)\n", strings.Join(parts, " "))

	return err
}
