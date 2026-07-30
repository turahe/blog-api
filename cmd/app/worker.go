package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

func newWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run asynchronous event consumers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if !cfg.MessagingEnabled() {
				return fmt.Errorf("MESSAGE_BROKER must be set to run the worker")
			}

			logger := newLogger(cfg.Environment)
			bus, err := messaging.Open(ctx, cfg)
			if err != nil {
				return fmt.Errorf("open messaging: %w", err)
			}
			defer bus.Close()

			router, err := message.NewRouter(message.RouterConfig{}, bus.Logger)
			if err != nil {
				return err
			}
			defer func() { _ = router.Close() }()

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
