package messaging

import (
	"context"

	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	"github.com/turahe/blog-api/internal/platform/config"
)

func openKafka(_ context.Context, bus *Bus, cfg config.Config) error {
	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:   cfg.KafkaBrokers,
			Marshaler: kafka.DefaultMarshaler{},
		},
		bus.Logger,
	)
	if err != nil {
		return err
	}

	subscriber, err := kafka.NewSubscriber(
		kafka.SubscriberConfig{
			Brokers:       cfg.KafkaBrokers,
			Unmarshaler:   kafka.DefaultMarshaler{},
			ConsumerGroup: cfg.KafkaConsumerGroup,
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
