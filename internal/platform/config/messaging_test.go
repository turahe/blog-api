package config

import (
	"testing"
)

func TestValidateMessagingEmptyOK(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	if err := cfg.ValidateMessaging(); err != nil {
		t.Fatalf("empty broker should be ok: %v", err)
	}
}

func TestValidateMessagingKafkaRequiresBrokers(t *testing.T) {
	t.Parallel()

	cfg := Config{MessageBroker: "kafka", KafkaConsumerGroup: "blog-api"}
	if err := cfg.ValidateMessaging(); err == nil {
		t.Fatal("expected error when KAFKA_BROKERS missing")
	}
}

func TestValidateMessagingKafkaOK(t *testing.T) {
	t.Parallel()

	cfg := Config{
		MessageBroker:      "kafka",
		KafkaBrokers:       []string{"127.0.0.1:9092"},
		KafkaConsumerGroup: "blog-api",
	}
	if err := cfg.ValidateMessaging(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMessagingRabbitRequiresURL(t *testing.T) {
	t.Parallel()

	cfg := Config{MessageBroker: "rabbitmq"}
	if err := cfg.ValidateMessaging(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateMessagingAMQPAliasOK(t *testing.T) {
	t.Parallel()

	cfg := Config{
		MessageBroker: "amqp",
		RabbitMQURL:   "amqp://guest:guest@localhost:5672/",
	}
	if err := cfg.ValidateMessaging(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMessagingGoogleRequiresProject(t *testing.T) {
	t.Parallel()

	cfg := Config{MessageBroker: "googlepubsub"}
	if err := cfg.ValidateMessaging(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateMessagingUnknownBroker(t *testing.T) {
	t.Parallel()

	cfg := Config{MessageBroker: "nats"}
	if err := cfg.ValidateMessaging(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadMessagingFromEnv(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("MESSAGE_BROKER", "kafka")
	t.Setenv("KAFKA_BROKERS", "127.0.0.1:9092,127.0.0.1:9093")
	t.Setenv("KAFKA_CONSUMER_GROUP", "blog-api")
	t.Setenv("MESSAGE_TOPIC_PREFIX", "blog.")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MessageBroker != "kafka" {
		t.Fatalf("broker=%q", cfg.MessageBroker)
	}

	if len(cfg.KafkaBrokers) != 2 {
		t.Fatalf("brokers=%v", cfg.KafkaBrokers)
	}

	if !cfg.MessagingEnabled() {
		t.Fatal("expected MessagingEnabled")
	}
}

func TestLoadMessagingAMQPAliasFromEnv(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("MESSAGE_BROKER", "amqp")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MessageBroker != "amqp" {
		t.Fatalf("broker=%q", cfg.MessageBroker)
	}

	if cfg.RabbitMQURL == "" {
		t.Fatal("expected RABBITMQ_URL")
	}
}
