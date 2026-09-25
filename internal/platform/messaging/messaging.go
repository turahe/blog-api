package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/turahe/blog-api/internal/platform/config"
)

// Canonical MESSAGE_BROKER values.
const (
	BrokerKafka        = "kafka"
	BrokerRabbitMQ     = "rabbitmq"
	BrokerGooglePubSub = "googlepubsub"
)

// Bus bundles a broker's Watermill publisher and subscriber.
type Bus struct {
	Broker      string
	Publisher   message.Publisher
	Subscriber  message.Subscriber
	Logger      watermill.LoggerAdapter
	topicPrefix string
	cleanup     []func() error
}

// NormalizeBroker maps MESSAGE_BROKER aliases to canonical broker names.
func NormalizeBroker(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case BrokerKafka:
		return BrokerKafka, nil
	case BrokerRabbitMQ, "amqp", "rabbit":
		return BrokerRabbitMQ, nil
	case BrokerGooglePubSub, "gcp-pubsub", "pubsub":
		return BrokerGooglePubSub, nil
	case "":
		return "", errors.New("MESSAGE_BROKER is required")
	default:
		return "", fmt.Errorf("unsupported MESSAGE_BROKER %q", raw)
	}
}

// Open connects the publisher and subscriber for the configured broker; Watermill
// logs through logger (slog.Default when nil). Subscribers share one consumer group,
// queue, or subscription, so each message is handled by one worker.
func Open(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Bus, error) {
	return open(ctx, cfg, logger, "")
}

// OpenBroadcast is Open with a subscriber of its own: every process that opens it receives
// every message published after it subscribes. instance names the process's temporary
// queue or subscription (letters, digits, and dashes); it is removed when the process
// stops (RabbitMQ) or after a day unused (Google Pub/Sub). Kafka reads without a
// consumer group, from the newest offset.
func OpenBroadcast(ctx context.Context, cfg config.Config, logger *slog.Logger, instance string) (*Bus, error) {
	if strings.TrimSpace(instance) == "" {
		return nil, errors.New("broadcast instance name is required")
	}

	return open(ctx, cfg, logger, instance)
}

func open(ctx context.Context, cfg config.Config, logger *slog.Logger, instance string) (*Bus, error) {
	broker, err := NormalizeBroker(cfg.MessageBroker)
	if err != nil {
		return nil, err
	}

	cfgCopy := cfg

	cfgCopy.MessageBroker = broker
	if err := cfgCopy.ValidateMessaging(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	bus := &Bus{
		Broker:      broker,
		Logger:      watermill.NewSlogLogger(logger.With("component", "watermill", "broker", broker)),
		topicPrefix: cfg.MessageTopicPrefix,
	}

	switch broker {
	case BrokerKafka:
		err = openKafka(ctx, bus, cfg, instance)
	case BrokerRabbitMQ:
		err = openRabbitMQ(ctx, bus, cfg, instance)
	case BrokerGooglePubSub:
		err = openGooglePubSub(ctx, bus, cfg, instance)
	}

	if err != nil {
		_ = bus.Close()
		return nil, err
	}

	return bus, nil
}

// Topic returns name with the configured topic prefix.
func (b *Bus) Topic(name string) string {
	name = strings.TrimPrefix(name, "/")
	if b == nil || b.topicPrefix == "" {
		return name
	}

	if strings.HasPrefix(name, b.topicPrefix) {
		return name
	}

	return b.topicPrefix + name
}

// Close shuts down the publisher, subscriber, and broker clients.
func (b *Bus) Close() error {
	if b == nil {
		return nil
	}

	var errs []error

	if b.Publisher != nil {
		if err := b.Publisher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close publisher: %w", err))
		}
	}

	if b.Subscriber != nil {
		if err := b.Subscriber.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close subscriber: %w", err))
		}
	}

	for _, v := range slices.Backward(b.cleanup) {
		if err := v(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
