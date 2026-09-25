// Package messaging opens Watermill publishers/subscribers for Kafka, RabbitMQ, and Google Pub/Sub.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/ThreeDotsLabs/watermill-googlecloud/v2/pkg/googlecloud"
	"github.com/turahe/blog-api/internal/platform/config"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/durationpb"
)

// broadcastSubscriptionTTL is Pub/Sub's minimum expiration: an instance's subscription is
// deleted after a day without subscribers.
const broadcastSubscriptionTTL = 24 * time.Hour

func openGooglePubSub(_ context.Context, bus *Bus, cfg config.Config, instance string) error {
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

	subscriberConfig := googlecloud.SubscriberConfig{
		ProjectID:                cfg.GooglePubSubProjectID,
		ClientOptions:            opts,
		GenerateSubscriptionName: googlecloud.TopicSubscriptionName,
	}
	if instance != "" {
		subscriberConfig.GenerateSubscriptionName = googlecloud.TopicSubscriptionNameWithSuffix("_" + instance)
		subscriberConfig.GenerateSubscription = func(googlecloud.GenerateSubscriptionParams) *pubsubpb.Subscription {
			return &pubsubpb.Subscription{
				ExpirationPolicy: &pubsubpb.ExpirationPolicy{Ttl: durationpb.New(broadcastSubscriptionTTL)},
			}
		}
	}

	subscriber, err := googlecloud.NewSubscriber(subscriberConfig, bus.Logger)
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
			return nil, errors.New("GOOGLE_PUBSUB_CREDENTIALS_SOURCE path is empty")
		}

		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("GOOGLE_PUBSUB_CREDENTIALS_SOURCE credentials file: %w", err)
		}

		return []option.ClientOption{option.WithAuthCredentialsFile(option.ServiceAccount, path)}, nil
	default:
		return nil, fmt.Errorf("unsupported GOOGLE_PUBSUB_CREDENTIALS_SOURCE %q", source)
	}
}
