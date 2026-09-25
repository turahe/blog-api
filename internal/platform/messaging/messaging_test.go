package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/turahe/blog-api/internal/platform/config"
)

func TestNormalizeBroker(t *testing.T) {
	t.Parallel()

	got, err := NormalizeBroker("Kafka")
	if err != nil || got != BrokerKafka {
		t.Fatalf("got %q err=%v", got, err)
	}

	if _, err := NormalizeBroker("nats"); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenRequiresMessaging(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), config.Config{}, nil)
	if err == nil {
		t.Fatal("expected error when MESSAGE_BROKER empty")
	}
}

func TestOpenAMQPAliasAccepted(t *testing.T) {
	t.Parallel()

	bus, err := Open(context.Background(), config.Config{
		MessageBroker: "amqp",
		RabbitMQURL:   "amqp://guest:guest@localhost:5672/",
	}, nil)
	if err == nil {
		if bus == nil {
			t.Fatal("expected bus when open succeeds")
		}

		_ = bus.Close()

		return
	}

	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "connection") && !strings.Contains(lower, "dial") && !strings.Contains(lower, "rabbitmq") && !strings.Contains(lower, "amqp") {
		t.Fatalf("expected connection-related error, got %v", err)
	}
}

func TestOpenRejectsIncompleteKafkaWithoutDial(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), config.Config{
		MessageBroker:      "kafka",
		KafkaConsumerGroup: "blog-api",
	}, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestTopicPrefix(t *testing.T) {
	t.Parallel()

	b := &Bus{topicPrefix: "blog."}
	if got := b.Topic("post.published"); got != "blog.post.published" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenBroadcastRequiresInstance(t *testing.T) {
	t.Parallel()

	_, err := OpenBroadcast(context.Background(), config.Config{MessageBroker: BrokerKafka}, nil, " ")
	if err == nil || !strings.Contains(err.Error(), "instance") {
		t.Fatalf("expected an instance error, got %v", err)
	}
}
