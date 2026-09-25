package messaging

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/IBM/sarama"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/xdg-go/scram"
)

// applyKafkaSecurity enables TLS (KAFKA_TLS, KAFKA_TLS_CA_PATH) and SASL (KAFKA_SASL_*) on a
// sarama config. The system roots verify the broker unless a CA bundle is given.
func applyKafkaSecurity(sc *sarama.Config, cfg config.Config) error {
	if cfg.KafkaTLS {
		tlsConfig, err := kafkaTLSConfig(cfg.KafkaTLSCAPath)
		if err != nil {
			return err
		}

		sc.Net.TLS.Enable = true
		sc.Net.TLS.Config = tlsConfig
	}

	if cfg.KafkaSASLMechanism == "" {
		return nil
	}

	sc.Net.SASL.Enable = true
	sc.Net.SASL.Handshake = true
	sc.Net.SASL.User = cfg.KafkaSASLUsername
	sc.Net.SASL.Password = cfg.KafkaSASLPassword

	switch cfg.KafkaSASLMechanism {
	case config.KafkaSASLPlain:
		sc.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	case config.KafkaSASLSCRAMSHA256:
		sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &scramClient{hash: scram.SHA256} }
	case config.KafkaSASLSCRAMSHA512:
		sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &scramClient{hash: scram.SHA512} }
	default:
		return fmt.Errorf("unsupported KAFKA_SASL_MECHANISM %q", cfg.KafkaSASLMechanism)
	}

	return nil
}

func kafkaTLSConfig(caPath string) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caPath == "" {
		return tlsConfig, nil
	}

	pem, err := os.ReadFile(caPath) //nolint:gosec // G304: path comes from operator-controlled KAFKA_TLS_CA_PATH
	if err != nil {
		return nil, fmt.Errorf("read KAFKA_TLS_CA_PATH: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("KAFKA_TLS_CA_PATH has no PEM certificates")
	}

	tlsConfig.RootCAs = pool

	return tlsConfig, nil
}

// scramClient adapts xdg-go/scram to sarama.SCRAMClient.
type scramClient struct {
	hash         scram.HashGeneratorFcn
	conversation *scram.ClientConversation
}

func (c *scramClient) Begin(user, password, authzID string) error {
	client, err := c.hash.NewClient(user, password, authzID)
	if err != nil {
		return fmt.Errorf("scram client: %w", err)
	}

	c.conversation = client.NewConversation()

	return nil
}

func (c *scramClient) Step(challenge string) (string, error) {
	response, err := c.conversation.Step(challenge)
	if err != nil {
		return "", fmt.Errorf("scram step: %w", err)
	}

	return response, nil
}

func (c *scramClient) Done() bool { return c.conversation.Done() }
