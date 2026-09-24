package messaging

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/turahe/blog-api/internal/platform/config"
)

const googlePubSubEndpoint = "pubsub.googleapis.com:443"

type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Probe checks that the configured broker accepts TCP connections. It is cheap
// enough for readiness checks: no publisher or subscriber is opened.
func Probe(ctx context.Context, cfg config.Config) error {
	var dialer net.Dialer

	return probe(ctx, cfg, dialer.DialContext)
}

func probe(ctx context.Context, cfg config.Config, dial dialFunc) error {
	broker, err := NormalizeBroker(cfg.MessageBroker)
	if err != nil {
		return err
	}

	addrs, err := probeAddresses(broker, cfg)
	if err != nil {
		return err
	}

	var errs []error

	for _, addr := range addrs {
		conn, err := dial(ctx, "tcp", addr)
		if err == nil {
			return conn.Close()
		}

		errs = append(errs, fmt.Errorf("%s %s: %w", broker, addr, err))
	}

	return errors.Join(errs...)
}

func probeAddresses(broker string, cfg config.Config) ([]string, error) {
	switch broker {
	case BrokerKafka:
		addrs := make([]string, 0, len(cfg.KafkaBrokers))
		for _, b := range cfg.KafkaBrokers {
			if b = strings.TrimSpace(b); b != "" {
				addrs = append(addrs, b)
			}
		}

		if len(addrs) == 0 {
			return nil, errors.New("KAFKA_BROKERS is required")
		}

		return addrs, nil
	case BrokerRabbitMQ:
		addr, err := amqpAddress(cfg.RabbitMQURL)
		if err != nil {
			return nil, err
		}

		return []string{addr}, nil
	default:
		if emulator := strings.TrimSpace(os.Getenv("PUBSUB_EMULATOR_HOST")); emulator != "" {
			return []string{emulator}, nil
		}

		return []string{googlePubSubEndpoint}, nil
	}
}

func amqpAddress(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", errors.New("RABBITMQ_URL must be an amqp:// or amqps:// URL with a host")
	}

	port := u.Port()
	if port == "" {
		port = "5672"
		if u.Scheme == "amqps" {
			port = "5671"
		}
	}

	return net.JoinHostPort(u.Hostname(), port), nil
}
