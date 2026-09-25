package bootstrap

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/analyticslive"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/system"
)

type discardSink struct{}

func (discardSink) Enqueue(analyticsdomain.Event) bool { return true }

// TestAnalyticsLiveReachesEveryReplica runs two replicas on RabbitMQ: a page view accepted by
// one shows on both boards.
func TestAnalyticsLiveReachesEveryReplica(t *testing.T) {
	t.Parallel()

	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("TEST_RABBITMQ_URL not set")
	}

	prefix := "it-" + strings.ToLower(watermill.NewShortUUID()) + "."
	cfg := config.Config{MessageBroker: "rabbitmq", RabbitMQURL: url, MessageTopicPrefix: prefix, SSEMaxConcurrentPerUser: 1}
	logger := slog.New(slog.DiscardHandler)

	t.Cleanup(func() {
		conn, err := amqp.Dial(url)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		if channel, err := conn.Channel(); err == nil {
			_ = channel.ExchangeDelete(prefix+analyticslive.Topic, false, false)
		}
	})

	boards := make([]*analyticsservice.Board, 2)
	ingests := make([]*analyticsservice.Ingest, 2)

	for i := range boards {
		bus := newBroadcast(t.Context(), cfg, logger)
		require.NotNil(t, bus)
		t.Cleanup(func() { _ = bus.Close() })

		ingests[i] = analyticsservice.NewIngest(discardSink{}, nil, system.UUIDGenerator{}, system.Clock{})
		boards[i], _ = newAnalyticsLive(t.Context(), cfg, bus, ingests[i], logger)
		require.NotNil(t, boards[i])
	}

	_, err := ingests[0].PageView(t.Context(), analyticsservice.Meta{UserAgent: "Mozilla/5.0 Firefox/140.0"},
		analyticsservice.PageViewInput{SessionID: uuid.New(), Path: "/live"})
	require.NoError(t, err)

	for i, board := range boards {
		require.Eventually(t, func() bool {
			snap, _ := board.Snapshot(0)
			return len(snap.TopPages) == 1 && snap.TopPages[0].Path == "/live" && snap.ActiveSessions == 1
		}, 15*time.Second, 50*time.Millisecond, "replica %d", i)
	}
}
