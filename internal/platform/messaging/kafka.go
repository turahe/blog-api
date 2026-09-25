package messaging

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	"github.com/turahe/blog-api/internal/platform/config"
)

// openKafka joins KAFKA_CONSUMER_GROUP, or no group at all for a broadcast instance.
func openKafka(_ context.Context, bus *Bus, cfg config.Config, instance string) error {
	group := cfg.KafkaConsumerGroup
	if instance != "" {
		group = ""
	}

	publisherConfig := kafka.DefaultSaramaSyncPublisherConfig()
	if err := applyKafkaSecurity(publisherConfig, cfg); err != nil {
		return err
	}

	saramaConfig := kafka.DefaultSaramaSubscriberConfig()
	if err := applyKafkaSecurity(saramaConfig, cfg); err != nil {
		return err
	}

	if group != "" {
		// A new group starts from the oldest offset; the relay publishes before the worker's
		// consumers first join, and those events must not be skipped.
		saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
	}

	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:               cfg.KafkaBrokers,
			Marshaler:             kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: publisherConfig,
		},
		bus.Logger,
	)
	if err != nil {
		return err
	}

	subscriber, err := kafka.NewSubscriber(
		kafka.SubscriberConfig{
			Brokers:               cfg.KafkaBrokers,
			Unmarshaler:           kafka.DefaultMarshaler{},
			ConsumerGroup:         group,
			OverwriteSaramaConfig: saramaConfig,
		},
		bus.Logger,
	)
	if err != nil {
		_ = publisher.Close()
		return err
	}

	bus.Publisher = publisher
	bus.Subscriber = subscriber

	return nil
}
