package messaging

import (
	"context"

	"github.com/ThreeDotsLabs/watermill-amqp/v3/pkg/amqp"
	"github.com/turahe/blog-api/internal/platform/config"
)

// openRabbitMQ keeps the durable fanout exchange for every caller; a broadcast instance
// binds its own exclusive queue that RabbitMQ deletes when the connection closes.
func openRabbitMQ(_ context.Context, bus *Bus, cfg config.Config, instance string) error {
	amqpConfig := amqp.NewDurablePubSubConfig(cfg.RabbitMQURL, amqp.GenerateQueueNameTopicName)
	if instance != "" {
		amqpConfig = amqp.NewDurablePubSubConfig(cfg.RabbitMQURL, amqp.GenerateQueueNameTopicNameWithSuffix(instance))
		amqpConfig.Queue.Durable = false
		amqpConfig.Queue.AutoDelete = true
		amqpConfig.Queue.Exclusive = true
	}

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
