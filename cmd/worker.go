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
	"github.com/turahe/blog-api/internal/adapters/outbound/mailqueue"
	"github.com/turahe/blog-api/internal/adapters/outbound/outbox"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/bootstrap"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/metrics"
)

// auditPruneInterval is how often the worker deletes audit entries and dedupe rows past retention.
const auditPruneInterval = time.Hour

// emailConsumer is the handler name, and dedupe key, of the email dispatch consumer.
const emailConsumer = "email-dispatch"

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

			router, err := messaging.NewRouter(bus, messaging.ConsumerConfig{
				MaxRetries:      cfg.ConsumerMaxRetries,
				InitialInterval: cfg.ConsumerRetryInterval,
				MaxInterval:     cfg.ConsumerRetryMaxInterval,
			})
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, router.Close()) }()

			db, err := database.Open(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, db.Close()) }()

			activity := auditservice.NewActivity(persistence.NewAuditRepository(db.GORM))
			go activity.PruneEvery(ctx, cfg.AuditRetention(), auditPruneInterval, logger)

			dedupe := persistence.NewProcessedMessageRepository(db.GORM)
			go pruneProcessedEvery(ctx, dedupe, cfg.ConsumerDedupeRetention, logger)

			consumers, err := addConsumers(router, bus, cfg, dedupe, logger)
			if err != nil {
				return err
			}

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

			logger.Info("worker started", "broker", bus.Broker, "consumers", consumers)

			if len(consumers) > 0 {
				err = router.Run(ctx)
			} else {
				<-ctx.Done()
			}

			stop()
			<-relayDone

			if err != nil && ctx.Err() == nil {
				return fmt.Errorf("router: %w", err)
			}

			return nil
		},
	}
}

// addConsumers registers the worker's message handlers and returns their names.
func addConsumers(
	router *message.Router, bus *messaging.Bus, cfg config.Config, dedupe messaging.Deduper, logger *slog.Logger,
) ([]string, error) {
	var names []string

	box, err := bootstrap.NewSecretBox(cfg)
	if err != nil {
		return nil, err
	}

	mailer, err := bootstrap.NewMailer(cfg)
	if err != nil {
		return nil, err
	}

	if box != nil && mailer != nil {
		router.AddConsumerHandler(emailConsumer, bus.Topic(event.NotificationEmailRequested), bus.Subscriber,
			messaging.Idempotent(dedupe, emailConsumer, mailqueue.Handler(box, mailer)))

		names = append(names, emailConsumer)
	} else {
		logger.Warn("email dispatch disabled: needs APP_ENCRYPTION_KEY and SMTP_HOST; queued emails wait in the broker")
	}

	return names, nil
}

// pruneProcessedEvery deletes consumer dedupe rows older than retention until ctx ends.
func pruneProcessedEvery(
	ctx context.Context, repo *persistence.ProcessedMessageRepository, retention time.Duration, logger *slog.Logger,
) {
	ticker := time.NewTicker(auditPruneInterval)
	defer ticker.Stop()

	for {
		if removed, err := repo.PruneBefore(ctx, time.Now().Add(-retention)); err != nil {
			logger.ErrorContext(ctx, "prune processed messages failed", "error", err)
		} else if removed > 0 {
			logger.InfoContext(ctx, "pruned processed messages", "count", removed)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
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
