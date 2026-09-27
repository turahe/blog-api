package messaging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

func TestOpenRabbitMQUnreachable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance string
	}{
		{name: "shared queue"},
		{name: "broadcast queue", instance: "api-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{MessageBroker: "rabbitmq", RabbitMQURL: "amqp://guest:guest@" + closedAddr(t) + "/"}

			var (
				bus *Bus
				err error
			)

			if tt.instance == "" {
				bus, err = Open(context.Background(), cfg, nil)
			} else {
				bus, err = OpenBroadcast(context.Background(), cfg, nil, tt.instance)
			}

			require.ErrorContains(t, err, "connection refused")
			assert.Nil(t, bus)
		})
	}
}
