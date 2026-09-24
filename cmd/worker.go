package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/logging"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

func newWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run asynchronous event consumers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if !cfg.MessagingEnabled() {
				return errors.New("MESSAGE_BROKER must be set to run the worker")
			}

			logger := logging.New(cfg.Environment)

			bus, err := messaging.Open(ctx, cfg, logger)
			if err != nil {
				return fmt.Errorf("open messaging: %w", err)
			}
			defer func() { err = errors.Join(err, bus.Close()) }()

			router, err := message.NewRouter(message.RouterConfig{}, bus.Logger)
			if err != nil {
				return fmt.Errorf("create message router: %w", err)
			}
			defer func() { err = errors.Join(err, router.Close()) }()

			router.AddMiddleware(messaging.Recoverer)

			topic := bus.Topic("worker.heartbeat")
			router.AddConsumerHandler(
				"worker-heartbeat",
				topic,
				bus.Subscriber,
				func(msg *message.Message) error {
					logger.Debug("heartbeat message", "uuid", msg.UUID)
					return nil
				},
			)

			logger.Info("worker started", "broker", bus.Broker, "topic", topic)

			if err := router.Run(ctx); err != nil && ctx.Err() == nil {
				return fmt.Errorf("router: %w", err)
			}

			return nil
		},
	}
}
