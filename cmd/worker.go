package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/platform/config"
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

			logger := newLogger(cfg.Environment)

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

			router.AddMiddleware(recoverHandler)

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

// recoverHandler turns a message handler panic into an error so the router
// nacks the message instead of crashing the worker process.
func recoverHandler(h message.HandlerFunc) message.HandlerFunc {
	return func(msg *message.Message) (msgs []*message.Message, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("message handler panic: %v\n%s", recovered, debug.Stack())
			}
		}()

		return h(msg)
	}
}
