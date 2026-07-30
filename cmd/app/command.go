package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"github.com/turahe/blog-api/internal/platform/seed"
)

var (
	version   = "0.1.0-dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "app",
		Short:         "Blog Platform API",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newServeCommand(),
		newMigrateCommand(),
		newDoctorCommand(),
		newVersionCommand(),
		newSeedCommand(),
		newWorkerCommand(),
		newPlaceholderCommand("scheduler", "Run recurring background jobs"),
	)
	return root
}

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			logger := newLogger(cfg.Environment)
			app, err := bootstrap.NewRuntime(ctx, cfg, logger, version)
			if err != nil {
				return fmt.Errorf("bootstrap application: %w", err)
			}
			defer app.Close()

			serverErr := make(chan error, 1)
			go func() {
				logger.Info("HTTP server listening", "address", cfg.Address, "version", version)
				serverErr <- app.Server.ListenAndServe()
			}()

			select {
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
				defer cancel()
				if err := app.Server.Shutdown(shutdownCtx); err != nil {
					return fmt.Errorf("shutdown HTTP server: %w", err)
				}
				return nil
			case err := <-serverErr:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return fmt.Errorf("serve HTTP: %w", err)
			}
		},
	}
}

func newMigrateCommand() *cobra.Command {
	migrate := &cobra.Command{Use: "migrate", Short: "Manage database migrations"}
	migrate.AddCommand(
		migrationAction("up", migrations.Up),
		migrationAction("down", migrations.Down),
		migrationAction("status", migrations.Status),
	)
	return migrate
}

func migrationAction(name string, action func(*sql.DB) error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: name + " database migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer db.Close()
			return action(db.SQL)
		},
	}
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check required runtime dependencies",
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
			if app.Config.MessagingEnabled() {
				bus, err := messaging.Open(cmd.Context(), app.Config)
				if err != nil {
					return fmt.Errorf("messaging: %w", err)
				}
				_ = bus.Close()
				fmt.Fprintf(cmd.OutOrStdout(), "messaging (%s): ok\n", bus.Broker)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "messaging: skipped (MESSAGE_BROKER unset)")
			}
			return nil
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "version=%s commit=%s built=%s go=%s\n", version, commit, buildTime, runtime.Version())
		},
	}
}

func newSeedCommand() *cobra.Command {
	var (
		email    string
		username string
		password string
		name     string
	)
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed roles, permissions, and an initial administrator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := seed.Run(cmd.Context(), db.GORM, seed.Options{
				AdminEmail: email, AdminUsername: username, AdminPassword: password, AdminName: name,
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "seed completed")
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "admin@example.com", "admin email")
	cmd.Flags().StringVar(&username, "username", "admin", "admin username")
	cmd.Flags().StringVar(&password, "password", "ChangeMeNow!123", "admin password")
	cmd.Flags().StringVar(&name, "name", "Administrator", "admin display name")
	return cmd
}

func newPlaceholderCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("%s is scaffolded but not implemented", use)
		},
	}
}

func newLogger(environment string) *slog.Logger {
	level := slog.LevelInfo
	if environment == "local" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
