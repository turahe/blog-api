package messaging

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

func listen(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			_ = conn.Close()
		}
	}()

	return ln.Addr().String()
}

func closedAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	return addr
}

func TestProbeKafkaSucceedsWhenAnyBrokerReachable(t *testing.T) {
	t.Parallel()

	err := Probe(t.Context(), config.Config{
		MessageBroker: "kafka",
		KafkaBrokers:  []string{closedAddr(t), listen(t)},
	})
	require.NoError(t, err)
}

func TestProbeKafkaFailsWhenNoBrokerReachable(t *testing.T) {
	t.Parallel()

	down := closedAddr(t)
	err := Probe(t.Context(), config.Config{MessageBroker: "kafka", KafkaBrokers: []string{down}})
	require.ErrorContains(t, err, down)
}

func TestProbeRabbitMQDialsURLHost(t *testing.T) {
	t.Parallel()

	addr := listen(t)
	require.NoError(t, Probe(t.Context(), config.Config{
		MessageBroker: "amqp",
		RabbitMQURL:   "amqp://guest:guest@" + addr + "/",
	}))

	require.Error(t, Probe(t.Context(), config.Config{
		MessageBroker: "rabbitmq",
		RabbitMQURL:   "amqp://guest:guest@" + closedAddr(t) + "/",
	}))
}

func TestProbeAddresses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"amqp default port", config.Config{MessageBroker: "rabbitmq", RabbitMQURL: "amqp://u:p@rabbit/"}, "rabbit:5672"},
		{"amqps default port", config.Config{MessageBroker: "rabbitmq", RabbitMQURL: "amqps://u:p@rabbit/vh"}, "rabbit:5671"},
		{"pubsub endpoint", config.Config{MessageBroker: "googlepubsub", GooglePubSubProjectID: "p"}, googlePubSubEndpoint},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var dialed string

			err := probe(t.Context(), tc.cfg, func(_ context.Context, _, address string) (net.Conn, error) {
				dialed = address
				return nil, errors.New("stop")
			})
			require.Error(t, err)
			require.Equal(t, tc.want, dialed)
		})
	}
}

func TestProbeRejectsBadConfig(t *testing.T) {
	t.Parallel()

	require.Error(t, Probe(t.Context(), config.Config{}))
	require.Error(t, Probe(t.Context(), config.Config{MessageBroker: "kafka"}))
	require.Error(t, Probe(t.Context(), config.Config{MessageBroker: "rabbitmq", RabbitMQURL: "::bad"}))
}
