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
	"github.com/turahe/blog-api/internal/adapters/inbound/probe"
	"github.com/turahe/blog-api/internal/adapters/outbound/mailqueue"
	"github.com/turahe/blog-api/internal/adapters/outbound/outbox"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/bootstrap"
	"github.com/turahe/blog-api/internal/core/event"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/metrics"
)

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

			cfg, err := config.LoadBackground()
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

			dedupe := persistence.NewProcessedMessageRepository(db.GORM)

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
				defer serveWorkerProbes(cfg, db, relay, logger)()
			}

			routerDone, err := startRouter(ctx, router, len(consumers) > 0)
			if err != nil {
				return err
			}

			relayDone := make(chan struct{})

			go func() {
				defer close(relayDone)

				relay.Run(ctx)
			}()

			logger.Info("worker started", "broker", bus.Broker, "consumers", consumers)

			runErr := <-routerDone

			stop()
			<-relayDone

			if runErr != nil && ctx.Err() == nil {
				return fmt.Errorf("router: %w", runErr)
			}

			return nil
		},
	}
}

// startRouter runs router in the background and returns once its subscriptions exist, so the
// relay never publishes before the worker's RabbitMQ queues or Kafka group membership do (a
// fanout exchange drops messages that no queue is bound for yet). The channel yields the
// router's result, or nil at shutdown when there are no consumers.
func startRouter(ctx context.Context, router *message.Router, hasConsumers bool) (<-chan error, error) {
	done := make(chan error, 1)

	if !hasConsumers {
		go func() {
			<-ctx.Done()

			done <- nil
		}()

		return done, nil
	}

	go func() { done <- router.Run(ctx) }()

	select {
	case <-router.Running():
		return done, nil
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("router: %w", err)
		}

		done <- nil

		return done, nil
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
		addConsumer(router, bus, cfg, emailConsumer, bus.Topic(event.NotificationEmailRequested),
			messaging.Idempotent(dedupe, emailConsumer, mailqueue.Handler(box, mailer, logger)), logger)

		names = append(names, emailConsumer)
	} else {
		logger.Warn("email dispatch disabled: needs APP_ENCRYPTION_KEY and SMTP_HOST; queued emails wait in the broker")
	}

	return names, nil
}

// addConsumer registers CONSUMER_CONCURRENCY copies of handler on topic behind one shared
// circuit breaker. Copies compete for queue messages (AMQP) or split partitions (Kafka); the
// dedupe key stays name, so copies never handle the same message twice.
func addConsumer(
	router *message.Router, bus *messaging.Bus, cfg config.Config, name, topic string,
	handler message.NoPublishHandlerFunc, logger *slog.Logger,
) {
	breaker := messaging.CircuitBreaker(name, messaging.BreakerConfig{
		Failures: cfg.ConsumerBreakerFailures,
		OpenFor:  cfg.ConsumerBreakerTimeout,
	}, logger)

	for i := range cfg.ConsumerConcurrency {
		handlerName := name
		if cfg.ConsumerConcurrency > 1 {
			handlerName = fmt.Sprintf("%s-%d", name, i+1)
		}

		router.AddConsumerHandler(handlerName, topic, bus.Subscriber, handler).AddMiddleware(breaker)
	}
}

// serveWorkerProbes serves /metrics, /healthz and /readyz on METRICS_ADDR and returns a func
// that shuts the server down. Readiness fails while the database or the broker is down.
func serveWorkerProbes(cfg config.Config, db *database.Database, relay *outbox.Relay, logger *slog.Logger) func() {
	workerMetrics := metrics.New(db.SQL, version)
	relay.WithObserver(workerMetrics)

	health := healthservice.New(version, bootstrap.WorkerHealthCheckers(cfg, db)...)

	return serveMetrics(workerMetrics.NewServer(cfg.MetricsAddr,
		metrics.Route{Pattern: "GET /healthz", Handler: probe.Live(health)},
		metrics.Route{Pattern: "GET /readyz", Handler: probe.Ready(health, logger)},
	), logger)
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
