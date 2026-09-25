package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/adapters/outbound/outbox"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/bootstrap"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/metrics"
)

// auditPruneInterval is how often the worker deletes audit entries past retention.
const auditPruneInterval = time.Hour

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

			logger, flush, err := bootstrap.NewLogger(cfg, version)
			if err != nil {
				return err
			}
			defer flush()

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

			router.AddMiddleware(messaging.Tracing, messaging.Recoverer)

			db, err := database.Open(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, db.Close()) }()

			activity := auditservice.NewActivity(persistence.NewAuditRepository(db.GORM))
			go activity.PruneEvery(ctx, cfg.AuditRetention(), auditPruneInterval, logger)

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

			relay := outbox.New(persistence.NewOutboxRepository(db.GORM), bus.Publisher, bus.Topic, outbox.Config{
				BatchSize:    cfg.OutboxBatchSize,
				PollInterval: cfg.OutboxPollInterval,
				MaxAttempts:  cfg.OutboxMaxAttempts,
				Retention:    cfg.OutboxRetention,
			}, logger)

			if cfg.MetricsAddr != "" {
				workerMetrics := metrics.New(db.SQL, version)
				relay.WithObserver(workerMetrics)

				stopMetrics := serveMetrics(workerMetrics.NewServer(cfg.MetricsAddr), logger)
				defer stopMetrics()
			}

			relayDone := make(chan struct{})

			go func() {
				defer close(relayDone)

				relay.Run(ctx)
			}()

			logger.Info("worker started", "broker", bus.Broker, "topic", topic)

			err = router.Run(ctx)

			stop()
			<-relayDone

			if err != nil && ctx.Err() == nil {
				return fmt.Errorf("router: %w", err)
			}

			return nil
		},
	}
}

// serveMetrics serves /metrics in the background and returns a func that shuts it down.
func serveMetrics(server *nethttp.Server, logger *slog.Logger) func() {
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			logger.Error("metrics server failed", "error", err)
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(ctx)
	}
}
