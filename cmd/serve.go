package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/platform/config"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			logger, flush, err := bootstrap.NewLogger(cfg, version)
			if err != nil {
				return err
			}
			defer flush()

			app, err := bootstrap.NewRuntime(ctx, cfg, logger, version)
			if err != nil {
				return fmt.Errorf("bootstrap application: %w", err)
			}
			defer func() { err = errors.Join(err, app.Close()) }()

			app.PolicySync.Start(ctx)

			serverErr := make(chan error, 2)

			go func() {
				logger.Info("HTTP server listening", "address", cfg.Address, "version", version)

				serverErr <- app.Server.ListenAndServe()
			}()

			if app.MetricsServer != nil {
				go func() {
					logger.Info("metrics server listening", "address", app.MetricsServer.Addr)

					if err := app.MetricsServer.ListenAndServe(); err != nil {
						serverErr <- fmt.Errorf("metrics: %w", err)
					}
				}()
			}

			select {
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
				defer cancel()

				var errs []error
				if err := app.Server.Shutdown(shutdownCtx); err != nil {
					errs = append(errs, fmt.Errorf("shutdown HTTP server: %w", err))
				}

				if app.MetricsServer != nil {
					if err := app.MetricsServer.Shutdown(shutdownCtx); err != nil {
						errs = append(errs, fmt.Errorf("shutdown metrics server: %w", err))
					}
				}

				return errors.Join(errs...)
			case err := <-serverErr:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}

				return fmt.Errorf("serve HTTP: %w", err)
			}
		},
	}
}
