package messaging

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

const (
	brokerTestTimeout = time.Minute
	topicDelivered    = "it.delivered"
	topicPoisoned     = "it.poisoned"
)

// brokerTestConfig targets a real broker from TEST_KAFKA_BROKERS or TEST_RABBITMQ_URL, or
// skips. Topics, queues, and the consumer group are unique to the test and deleted afterwards.
func brokerTestConfig(t *testing.T, broker string) config.Config {
	t.Helper()

	id := strings.ToLower(watermill.NewShortUUID())
	cfg := config.Config{MessageBroker: broker, MessageTopicPrefix: "it-" + id + "."}
	topics := []string{topicDelivered, topicPoisoned, DeadLetterTopic}

	switch broker {
	case BrokerKafka:
		brokers := os.Getenv("TEST_KAFKA_BROKERS")
		if brokers == "" {
			t.Skip("TEST_KAFKA_BROKERS not set")
		}

		cfg.KafkaBrokers = strings.Split(brokers, ",")
		cfg.KafkaConsumerGroup = "blog-api-it-" + id

		t.Cleanup(func() { cleanupKafka(t, cfg, topics) })
	case BrokerRabbitMQ:
		cfg.RabbitMQURL = os.Getenv("TEST_RABBITMQ_URL")
		if cfg.RabbitMQURL == "" {
			t.Skip("TEST_RABBITMQ_URL not set")
		}

		t.Cleanup(func() { cleanupRabbitMQ(t, cfg, topics) })
	}

	return cfg
}

func cleanupKafka(t *testing.T, cfg config.Config, topics []string) {
	t.Helper()

	admin, err := sarama.NewClusterAdmin(cfg.KafkaBrokers, sarama.NewConfig())
	if err != nil {
		t.Logf("kafka cleanup: %v", err)
		return
	}

	defer func() { _ = admin.Close() }()

	for _, topic := range topics {
		if err := admin.DeleteTopic(cfg.MessageTopicPrefix + topic); err != nil {
			t.Logf("kafka cleanup %s: %v", topic, err)
		}
	}

	err = admin.DeleteConsumerGroup(cfg.KafkaConsumerGroup)
	if err != nil && !errors.Is(err, sarama.ErrGroupIDNotFound) {
		t.Logf("kafka cleanup group: %v", err)
	}
}

func cleanupRabbitMQ(t *testing.T, cfg config.Config, topics []string) {
	t.Helper()

	conn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		t.Logf("rabbitmq cleanup: %v", err)
		return
	}

	defer func() { _ = conn.Close() }()

	channel, err := conn.Channel()
	if err != nil {
		t.Logf("rabbitmq cleanup: %v", err)
		return
	}

	for _, topic := range topics {
		name := cfg.MessageTopicPrefix + topic
		if _, err := channel.QueueDelete(name, false, false, false); err != nil {
			t.Logf("rabbitmq cleanup queue %s: %v", name, err)
		}

		if err := channel.ExchangeDelete(name, false, false); err != nil {
			t.Logf("rabbitmq cleanup exchange %s: %v", name, err)
		}
	}
}

func TestBrokerDeliveryAndDeadLetter(t *testing.T) {
	t.Parallel()

	for _, broker := range []string{BrokerKafka, BrokerRabbitMQ} {
		t.Run(broker, func(t *testing.T) {
			t.Parallel()

			testBrokerDeliveryAndDeadLetter(t, brokerTestConfig(t, broker))
		})
	}
}

// testBrokerDeliveryAndDeadLetter proves, against a real broker, that the message id, payload,
// and correlation header survive a round trip, and that a permanent failure is dead-lettered
// with Watermill's poison headers.
func testBrokerDeliveryAndDeadLetter(t *testing.T, cfg config.Config) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), brokerTestTimeout)
	defer cancel()

	bus, err := Open(ctx, cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(func() { _ = bus.Close() })

	dead, err := bus.Subscriber.Subscribe(ctx, bus.Topic(DeadLetterTopic))
	require.NoError(t, err)

	router, err := NewRouter(bus, ConsumerConfig{MaxRetries: 1, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond})
	require.NoError(t, err)

	delivered := make(chan *message.Message, 1)

	router.AddConsumerHandler("it-delivered", bus.Topic(topicDelivered), bus.Subscriber, func(msg *message.Message) error {
		delivered <- msg
		return nil
	})
	router.AddConsumerHandler("it-poisoned", bus.Topic(topicPoisoned), bus.Subscriber, func(*message.Message) error {
		return ErrPermanent
	})

	routerDone := make(chan struct{})

	go func() {
		defer close(routerDone)

		_ = router.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-routerDone
	})

	select {
	case <-router.Running():
	case <-ctx.Done():
		t.Fatal("router did not start")
	}

	sent := message.NewMessage(watermill.NewUUID(), []byte(`{"hello":"broker"}`))
	sent.Metadata.Set(CorrelationIDKey, "req-123")
	require.NoError(t, bus.Publisher.Publish(bus.Topic(topicDelivered), sent))

	poisoned := message.NewMessage(watermill.NewUUID(), []byte(`{}`))
	require.NoError(t, bus.Publisher.Publish(bus.Topic(topicPoisoned), poisoned))

	select {
	case got := <-delivered:
		require.Equal(t, sent.UUID, got.UUID)
		require.JSONEq(t, string(sent.Payload), string(got.Payload))
		require.Equal(t, "req-123", got.Metadata.Get(CorrelationIDKey))
	case <-ctx.Done():
		t.Fatal("message was not delivered")
	}

	select {
	case got := <-dead:
		got.Ack()
		require.Equal(t, poisoned.UUID, got.UUID)
		require.Equal(t, bus.Topic(topicPoisoned), got.Metadata.Get("topic_poisoned"))
		require.Contains(t, got.Metadata.Get("reason_poisoned"), ErrPermanent.Error())
	case <-ctx.Done():
		t.Fatal("message was not dead-lettered")
	}
}
