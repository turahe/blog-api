package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

type closer struct {
	err    error
	closed *[]string
	name   string
}

func (c closer) Close() error {
	*c.closed = append(*c.closed, c.name)

	return c.err
}

type fakePublisher struct{ closer }

func (fakePublisher) Publish(string, ...*message.Message) error { return nil }

type fakeSubscriber struct{ closer }

func (fakeSubscriber) Subscribe(context.Context, string) (<-chan *message.Message, error) {
	return nil, nil
}

func TestNormalizeBroker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "kafka any case", raw: " Kafka ", want: BrokerKafka},
		{name: "rabbitmq", raw: "rabbitmq", want: BrokerRabbitMQ},
		{name: "amqp alias", raw: "AMQP", want: BrokerRabbitMQ},
		{name: "rabbit alias", raw: "rabbit", want: BrokerRabbitMQ},
		{name: "googlepubsub", raw: "googlepubsub", want: BrokerGooglePubSub},
		{name: "gcp-pubsub alias", raw: "gcp-pubsub", want: BrokerGooglePubSub},
		{name: "pubsub alias", raw: "pubsub", want: BrokerGooglePubSub},
		{name: "empty", raw: "  ", wantErr: "MESSAGE_BROKER is required"},
		{name: "unsupported", raw: "nats", wantErr: `unsupported MESSAGE_BROKER "nats"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeBroker(tt.raw)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
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

func TestOpenBroadcastRequiresInstance(t *testing.T) {
	t.Parallel()

	_, err := OpenBroadcast(context.Background(), config.Config{MessageBroker: BrokerKafka}, nil, " ")
	if err == nil || !strings.Contains(err.Error(), "instance") {
		t.Fatalf("expected an instance error, got %v", err)
	}
}

func TestTopicPrefix(t *testing.T) {
	t.Parallel()

	var nilBus *Bus

	tests := []struct {
		name string
		bus  *Bus
		in   string
		want string
	}{
		{name: "prefix added", bus: &Bus{topicPrefix: "blog."}, in: "post.published", want: "blog.post.published"},
		{name: "leading slash trimmed", bus: &Bus{topicPrefix: "blog."}, in: "/post.published", want: "blog.post.published"},
		{name: "already prefixed", bus: &Bus{topicPrefix: "blog."}, in: "blog.post.published", want: "blog.post.published"},
		{name: "no prefix", bus: &Bus{}, in: "/post.published", want: "post.published"},
		{name: "nil bus", bus: nilBus, in: "post.published", want: "post.published"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.bus.Topic(tt.in))
		})
	}
}

func TestBusClose(t *testing.T) {
	t.Parallel()

	errPub := errors.New("pub")
	errSub := errors.New("sub")
	errClient := errors.New("client")

	t.Run("nil bus", func(t *testing.T) {
		t.Parallel()

		var bus *Bus
		require.NoError(t, bus.Close())
	})

	t.Run("empty bus", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, (&Bus{}).Close())
	})

	t.Run("closes everything and joins errors", func(t *testing.T) {
		t.Parallel()

		var closed []string

		bus := &Bus{
			Publisher:  fakePublisher{closer{err: errPub, closed: &closed, name: "publisher"}},
			Subscriber: fakeSubscriber{closer{err: errSub, closed: &closed, name: "subscriber"}},
			cleanup: []func() error{
				func() error { closed = append(closed, "first"); return nil },
				func() error { closed = append(closed, "second"); return errClient },
			},
		}

		err := bus.Close()

		require.ErrorIs(t, err, errPub)
		require.ErrorIs(t, err, errSub)
		require.ErrorIs(t, err, errClient)
		assert.EqualError(t, err, "close publisher: pub\nclose subscriber: sub\nclient")
		assert.Equal(t, []string{"publisher", "subscriber", "second", "first"}, closed, "cleanup runs in reverse")
	})

	t.Run("clean close", func(t *testing.T) {
		t.Parallel()

		var closed []string

		bus := &Bus{
			Publisher:  fakePublisher{closer{closed: &closed, name: "publisher"}},
			Subscriber: fakeSubscriber{closer{closed: &closed, name: "subscriber"}},
		}

		require.NoError(t, bus.Close())
		assert.Equal(t, []string{"publisher", "subscriber"}, closed)
	})
}
