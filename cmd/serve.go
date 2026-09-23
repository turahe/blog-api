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
