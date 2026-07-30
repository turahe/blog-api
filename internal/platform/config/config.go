package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress     = "0.0.0.0:8080"
	defaultDatabaseURL = "postgres://blog:blog@127.0.0.1:5432/blog?sslmode=disable"
	defaultRedisURL    = "redis://127.0.0.1:6379/0"
	defaultDBDriver    = "postgres"
)

type Config struct {
	Environment               string
	Address                   string
	DBDriver                  string
	DatabaseURL               string
	DBUser                    string
	DBPassword                string
	DBName                    string
	DBInstanceConnectionName  string
	DBIAMAuthEnabled          bool
	DBPrivateIPEnabled        bool
	DBGoogleCredentialsSource string
	RedisURL                  string
	TrustedProxies            []string
	ShutdownTimeout           time.Duration
	ReadTimeout               time.Duration
	ReadHeaderTimeout         time.Duration
	IdleTimeout               time.Duration
	DBMaxOpen                 int
	DBMaxIdle                 int
	DBMaxLifetime             time.Duration
	DBMaxIdleTime             time.Duration
	SessionKey                string
	CSRFKey                   string
	Pepper                    string
	JWTIssuer                 string
	AccessTokenTTL                time.Duration
	RefreshTokenTTL               time.Duration
	MessageBroker                 string
	KafkaBrokers                  []string
	KafkaConsumerGroup            string
	RabbitMQURL                   string
	GooglePubSubProjectID         string
	GooglePubSubCredentialsSource string
	MessageTopicPrefix            string
}

// UsesCloudSQL reports whether Cloud SQL connector settings are active.
func (c Config) UsesCloudSQL() bool {
	return strings.TrimSpace(c.DBInstanceConnectionName) != ""
}

func (c Config) MessagingEnabled() bool {
	return strings.TrimSpace(c.MessageBroker) != ""
}

func normalizeMessageBroker(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rabbitmq", "amqp", "rabbit":
		return "rabbitmq"
	case "googlepubsub", "gcp-pubsub", "pubsub":
		return "googlepubsub"
	case "kafka":
		return "kafka"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func (c Config) ValidateMessaging() error {
	broker := normalizeMessageBroker(c.MessageBroker)
	if broker == "" {
		return nil
	}
	switch broker {
	case "kafka":
		if len(c.KafkaBrokers) == 0 {
			return errors.New("KAFKA_BROKERS is required when MESSAGE_BROKER=kafka")
		}
		if strings.TrimSpace(c.KafkaConsumerGroup) == "" {
			return errors.New("KAFKA_CONSUMER_GROUP is required when MESSAGE_BROKER=kafka")
		}
	case "rabbitmq":
		if strings.TrimSpace(c.RabbitMQURL) == "" {
			return errors.New("RABBITMQ_URL is required when MESSAGE_BROKER=rabbitmq")
		}
	case "googlepubsub":
		if strings.TrimSpace(c.GooglePubSubProjectID) == "" {
			return errors.New("GOOGLE_PUBSUB_PROJECT_ID is required when MESSAGE_BROKER=googlepubsub")
		}
	default:
		return fmt.Errorf("unsupported MESSAGE_BROKER %q (want kafka, rabbitmq, or googlepubsub)", c.MessageBroker)
	}
	return nil
}

func Load() (Config, error) {
	cfg := Config{
		Environment:               env("APP_ENV", "local"),
		Address:                   env("APP_ADDR", defaultAddress),
		DBDriver:                  env("DB_DRIVER", defaultDBDriver),
		DatabaseURL:               env("DATABASE_URL", defaultDatabaseURL),
		DBUser:                    env("DB_USER", ""),
		DBPassword:                env("DB_PASSWORD", ""),
		DBName:                    env("DB_NAME", ""),
		DBInstanceConnectionName:  env("DB_INSTANCE_CONNECTION_NAME", ""),
		DBIAMAuthEnabled:          boolEnv("DB_IAM_AUTH_ENABLED", false),
		DBPrivateIPEnabled:        boolEnv("DB_PRIVATE_IP_ENABLED", true),
		DBGoogleCredentialsSource: env("DB_GOOGLE_CREDENTIALS_SOURCE", "workload-identity"),
		RedisURL:                  env("REDIS_URL", defaultRedisURL),
		TrustedProxies:            splitCSV(os.Getenv("APP_TRUSTED_PROXIES")),
		ShutdownTimeout:           duration("APP_SHUTDOWN_TIMEOUT", 15*time.Second),
		ReadTimeout:               duration("APP_READ_TIMEOUT", 15*time.Second),
		ReadHeaderTimeout:         duration("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		IdleTimeout:               duration("APP_IDLE_TIMEOUT", 60*time.Second),
		DBMaxOpen:                 integer("DB_POOL_MAX_OPEN", 25),
		DBMaxIdle:                 integer("DB_POOL_MAX_IDLE", 10),
		DBMaxLifetime:             durationOrSeconds("DB_POOL_MAX_LIFETIME", "DB_POOL_MAX_LIFETIME_SECONDS", 30*time.Minute),
		DBMaxIdleTime:             durationOrSeconds("DB_POOL_MAX_IDLE_TIME", "DB_POOL_MAX_IDLETIME_SECONDS", 10*time.Minute),
		SessionKey:                env("APP_SESSION_KEY", ""),
		CSRFKey:                   env("APP_CSRF_KEY", ""),
		Pepper:                    env("APP_PEPPER", ""),
		JWTIssuer:                 env("APP_JWT_ISSUER", "blog-api"),
		AccessTokenTTL:                duration("APP_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:               duration("APP_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		MessageBroker:                 strings.ToLower(env("MESSAGE_BROKER", "")),
		KafkaBrokers:                  splitCSV(os.Getenv("KAFKA_BROKERS")),
		KafkaConsumerGroup:            env("KAFKA_CONSUMER_GROUP", "blog-api"),
		RabbitMQURL:                   env("RABBITMQ_URL", ""),
		GooglePubSubProjectID:         env("GOOGLE_PUBSUB_PROJECT_ID", ""),
		GooglePubSubCredentialsSource: env("GOOGLE_PUBSUB_CREDENTIALS_SOURCE", "workload-identity"),
		MessageTopicPrefix:            env("MESSAGE_TOPIC_PREFIX", "blog."),
	}

	if cfg.Address == "" {
		return Config{}, errors.New("APP_ADDR must not be empty")
	}
	if len(cfg.SessionKey) < 32 {
		if cfg.Environment == "production" {
			return Config{}, errors.New("APP_SESSION_KEY must be at least 32 characters")
		}
		cfg.SessionKey = "local-dev-session-key-32bytes-min!!"
	}
	if cfg.Environment == "production" {
		if !cfg.UsesCloudSQL() {
			if strings.Contains(cfg.DatabaseURL, "sslmode=disable") {
				return Config{}, errors.New("DATABASE_URL cannot disable TLS in production")
			}
			if cfg.DatabaseURL == defaultDatabaseURL {
				return Config{}, errors.New("DATABASE_URL must be configured in production")
			}
		} else if cfg.DBInstanceConnectionName == "" || cfg.DBName == "" || cfg.DBUser == "" {
			return Config{}, errors.New("Cloud SQL requires DB_INSTANCE_CONNECTION_NAME, DB_NAME, and DB_USER in production")
		}
	}
	if cfg.DBMaxOpen < 1 || cfg.DBMaxIdle < 0 || cfg.DBMaxIdle > cfg.DBMaxOpen {
		return Config{}, fmt.Errorf("invalid database pool limits: idle=%d open=%d", cfg.DBMaxIdle, cfg.DBMaxOpen)
	}
	if err := cfg.ValidateMessaging(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func integer(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func duration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationOrSeconds(durationKey, secondsKey string, fallback time.Duration) time.Duration {
	if value := strings.TrimSpace(os.Getenv(durationKey)); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(secondsKey)); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
