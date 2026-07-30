package messaging

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ThreeDotsLabs/watermill-googlecloud/v2/pkg/googlecloud"
	"github.com/turahe/blog-api/internal/platform/config"
	"google.golang.org/api/option"
)

func openGooglePubSub(_ context.Context, bus *Bus, cfg config.Config) error {
	opts, err := googleCredentialsOptions(cfg.GooglePubSubCredentialsSource)
	if err != nil {
		return err
	}

	publisher, err := googlecloud.NewPublisher(googlecloud.PublisherConfig{
		ProjectID:     cfg.GooglePubSubProjectID,
		ClientOptions: opts,
	}, bus.Logger)
	if err != nil {
		return fmt.Errorf("open google pubsub publisher: %w", err)
	}

	subscriber, err := googlecloud.NewSubscriber(googlecloud.SubscriberConfig{
		ProjectID:                  cfg.GooglePubSubProjectID,
		ClientOptions:              opts,
		GenerateSubscriptionName:   googlecloud.TopicSubscriptionName,
	}, bus.Logger)
	if err != nil {
		_ = publisher.Close()
		return fmt.Errorf("open google pubsub subscriber: %w", err)
	}

	bus.Publisher = publisher
	bus.Subscriber = subscriber
	return nil
}

func googleCredentialsOptions(source string) ([]option.ClientOption, error) {
	source = strings.TrimSpace(source)
	switch {
	case source == "", source == "workload-identity", source == "adc":
		return nil, nil
	case strings.HasPrefix(source, "path:"):
		path := strings.TrimSpace(strings.TrimPrefix(source, "path:"))
		if path == "" {
			return nil, fmt.Errorf("GOOGLE_PUBSUB_CREDENTIALS_SOURCE path is empty")
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("GOOGLE_PUBSUB_CREDENTIALS_SOURCE credentials file: %w", err)
		}
		return []option.ClientOption{option.WithCredentialsFile(path)}, nil
	default:
		return nil, fmt.Errorf("unsupported GOOGLE_PUBSUB_CREDENTIALS_SOURCE %q", source)
	}
}
