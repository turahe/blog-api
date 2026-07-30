package messaging

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/turahe/blog-api/internal/platform/config"
)

const (
	BrokerKafka        = "kafka"
	BrokerRabbitMQ     = "rabbitmq"
	BrokerGooglePubSub = "googlepubsub"
)

type Bus struct {
	Broker      string
	Publisher   message.Publisher
	Subscriber  message.Subscriber
	Logger      watermill.LoggerAdapter
	topicPrefix string
	cleanup     []func() error
}

func NormalizeBroker(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case BrokerKafka:
		return BrokerKafka, nil
	case BrokerRabbitMQ, "amqp", "rabbit":
		return BrokerRabbitMQ, nil
	case BrokerGooglePubSub, "gcp-pubsub", "pubsub":
		return BrokerGooglePubSub, nil
	case "":
		return "", fmt.Errorf("MESSAGE_BROKER is required")
	default:
		return "", fmt.Errorf("unsupported MESSAGE_BROKER %q", raw)
	}
}

func Open(ctx context.Context, cfg config.Config) (*Bus, error) {
	broker, err := NormalizeBroker(cfg.MessageBroker)
	if err != nil {
		return nil, err
	}
	cfgCopy := cfg
	cfgCopy.MessageBroker = broker
	if err := cfgCopy.ValidateMessaging(); err != nil {
		return nil, err
	}

	bus := &Bus{
		Broker:      broker,
		Logger:      watermill.NewStdLogger(false, false),
		topicPrefix: cfg.MessageTopicPrefix,
	}

	switch broker {
	case BrokerKafka:
		err = openKafka(ctx, bus, cfg)
	case BrokerRabbitMQ:
		err = openRabbitMQ(ctx, bus, cfg)
	case BrokerGooglePubSub:
		err = openGooglePubSub(ctx, bus, cfg)
	}
	if err != nil {
		_ = bus.Close()
		return nil, err
	}
	return bus, nil
}

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

func (b *Bus) Close() error {
	if b == nil {
		return nil
	}

	var first error
	if b.Publisher != nil {
		if err := b.Publisher.Close(); err != nil && first == nil {
			first = err
		}
	}
	if b.Subscriber != nil {
		if err := b.Subscriber.Close(); err != nil && first == nil {
			first = err
		}
	}
	for i := len(b.cleanup) - 1; i >= 0; i-- {
		if err := b.cleanup[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}
