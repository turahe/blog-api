package messaging

import (
	"context"

	"github.com/ThreeDotsLabs/watermill-amqp/v3/pkg/amqp"
	"github.com/turahe/blog-api/internal/platform/config"
)

func openRabbitMQ(_ context.Context, bus *Bus, cfg config.Config) error {
	amqpConfig := amqp.NewDurablePubSubConfig(cfg.RabbitMQURL, amqp.GenerateQueueNameTopicName)

	publisher, err := amqp.NewPublisher(amqpConfig, bus.Logger)
	if err != nil {
		return err
	}

	subscriber, err := amqp.NewSubscriber(amqpConfig, bus.Logger)
	if err != nil {
		_ = publisher.Close()
		return err
	}

	bus.Publisher = publisher
	bus.Subscriber = subscriber
	return nil
}
