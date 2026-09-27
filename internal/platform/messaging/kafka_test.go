package messaging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

// Kafka protocol API keys the mock broker advertises.
const (
	apiKeyMetadata    = 3
	apiKeyAPIVersions = 18
)

// mockKafka starts an in-process sarama mock broker on loopback that answers metadata requests.
func mockKafka(t *testing.T) string {
	t.Helper()

	broker := sarama.NewMockBroker(t, 1)
	t.Cleanup(broker.Close)

	broker.SetHandlerByMap(map[string]sarama.MockResponse{
		"MetadataRequest": sarama.NewMockMetadataResponse(t).
			SetBroker(broker.Addr(), broker.BrokerID()).
			SetController(broker.BrokerID()),
		"ApiVersionsRequest": sarama.NewMockApiVersionsResponse(t).SetApiKeys([]sarama.ApiVersionsResponseKey{
			{ApiKey: apiKeyMetadata, MinVersion: 0, MaxVersion: 12},
			{ApiKey: apiKeyAPIVersions, MinVersion: 0, MaxVersion: 3},
		}),
	})

	return broker.Addr()
}

func TestOpenKafka(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance string
	}{
		{name: "consumer group"},
		{name: "broadcast without group", instance: "api-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{
				MessageBroker:      "kafka",
				KafkaBrokers:       []string{mockKafka(t)},
				KafkaConsumerGroup: "blog-api",
				MessageTopicPrefix: "test.",
			}

			var (
				bus *Bus
				err error
			)

			if tt.instance == "" {
				bus, err = Open(context.Background(), cfg, nil)
			} else {
				bus, err = OpenBroadcast(context.Background(), cfg, nil, tt.instance)
			}

			require.NoError(t, err)
			t.Cleanup(func() { _ = bus.Close() })

			assert.Equal(t, BrokerKafka, bus.Broker)
			assert.IsType(t, &kafka.Publisher{}, bus.Publisher)
			assert.IsType(t, &kafka.Subscriber{}, bus.Subscriber)
			assert.Equal(t, "test.post", bus.Topic("post"))
		})
	}
}

func TestOpenKafkaErrors(t *testing.T) {
	t.Parallel()

	badCA := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(badCA, []byte("not a certificate"), 0o600))

	tests := []struct {
		name    string
		cfg     func(t *testing.T) config.Config
		wantErr string
	}{
		{
			name: "invalid TLS CA",
			cfg: func(t *testing.T) config.Config {
				return config.Config{
					MessageBroker: "kafka", KafkaBrokers: []string{mockKafka(t)}, KafkaConsumerGroup: "g",
					KafkaTLS: true, KafkaTLSCAPath: badCA,
				}
			},
			wantErr: "KAFKA_TLS_CA_PATH has no PEM certificates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bus, err := Open(context.Background(), tt.cfg(t), nil)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, bus)
		})
	}
}

func TestOpenKafkaUnreachableBroker(t *testing.T) {
	t.Parallel()

	down := closedAddr(t)

	// The publisher retries metadata with a fixed 2s backoff; the bubble's fake clock skips it.
	synctest.Test(t, func(t *testing.T) {
		bus, err := Open(context.Background(), config.Config{
			MessageBroker: "kafka", KafkaBrokers: []string{down}, KafkaConsumerGroup: "g",
		}, nil)

		require.ErrorContains(t, err, "run out of available brokers")
		assert.Nil(t, bus)
	})
}
