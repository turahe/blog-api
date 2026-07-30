# Multi-Broker Messaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Watermill messaging factory selectable via `MESSAGE_BROKER` for Kafka, RabbitMQ, and Google Cloud Pub/Sub, with local Compose for Kafka/RabbitMQ and a real `app worker` lifecycle.

**Architecture:** Config loads broker settings; `internal/platform/messaging` builds Watermill `message.Publisher` + `message.Subscriber` for the chosen transport; `app worker` opens the bus, runs a minimal router (health/noop handler until outbox consumers land), and shuts down cleanly. Core stays free of Watermill imports.

**Tech Stack:** Go 1.26.5, Watermill, `watermill-kafka/v3`, `watermill-amqp/v3`, `watermill-googlecloud/v2`, Docker Compose profiles.

## Global Constraints

- Core (`internal/core/**`) must not import Gin, GORM, Redis, Watermill, or broker SDKs.
- Delivery is at-least-once; do not claim exactly-once.
- No Google Pub/Sub emulator in Compose this phase.
- Do not implement the full outbox consumer catalogue; worker scaffolding + broker open/close is enough.
- Prefer Compose `--profile messaging` so default `make infra-up` stays light.
- Pin exact module versions in `go get` (no `@latest`).
- AI commits must include `Co-Authored-By: Composer <noreply@example.com>` only when the user asks for a commit.
- `make test` / `go test` must stay scoped to `./cmd/... ./internal/...` (avoid `data/` volume scan).
- Tech-stack + events Event Bus Strategy already updated; do not regress those docs.

## File map

| Path | Responsibility |
|------|----------------|
| `internal/platform/config/config.go` | Messaging env fields + optional/required validation helpers |
| `internal/platform/config/messaging_test.go` | Config validation unit tests |
| `internal/platform/messaging/messaging.go` | Broker constants, `Bus` type, `Open`/`Close`, topic prefix helper |
| `internal/platform/messaging/kafka.go` | Kafka publisher/subscriber |
| `internal/platform/messaging/rabbitmq.go` | RabbitMQ AMQP publisher/subscriber |
| `internal/platform/messaging/googlepubsub.go` | Google Pub/Sub publisher/subscriber |
| `internal/platform/messaging/messaging_test.go` | Normalize + Open rejects misconfig without broker |
| `cmd/app/command.go` | Replace `worker` placeholder; doctor reports messaging |
| `cmd/app/worker.go` | Worker RunE: open bus, run router until signal |
| `compose.yaml` | `kafka` + `rabbitmq` under `profiles: [messaging]` |
| `Makefile` | `infra-up-messaging` target |
| `.env.example` | Messaging vars |
| `docs/deployment/local.md` | Messaging profile section |
| `docs/architecture/architecture.md` | Platform `messaging/` path + events note |
| `docs/architecture/deployment.md` | Broker config mention |
| `CHANGELOG.md` | Entry for this feature |
| `docs/superpowers/specs/2026-07-30-messaging-brokers-design.md` | Mark status implemented when done |

---

### Task 1: Messaging config fields and validation

**Files:**
- Modify: `internal/platform/config/config.go`
- Create: `internal/platform/config/messaging_test.go`

**Interfaces:**
- Consumes: existing `env`, `boolEnv`, `splitCSV` helpers in config package
- Produces:
  - `Config` fields: `MessageBroker`, `KafkaBrokers []string`, `KafkaConsumerGroup`, `RabbitMQURL`, `GooglePubSubProjectID`, `GooglePubSubCredentialsSource`, `MessageTopicPrefix string`
  - `func (c Config) MessagingEnabled() bool`
  - `func (c Config) ValidateMessaging() error` — errors if broker set but incomplete; empty broker is OK

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/config/messaging_test.go`:

```go
package config

import (
	"os"
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
	_ = os.Unsetenv // keep import if needed; t.Setenv cleans up
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 ./internal/platform/config/ -run Messaging`
Expected: FAIL (missing fields / methods)

- [ ] **Step 3: Implement config fields and validation**

Add to `Config` struct:

```go
MessageBroker                 string
KafkaBrokers                  []string
KafkaConsumerGroup            string
RabbitMQURL                   string
GooglePubSubProjectID         string
GooglePubSubCredentialsSource string
MessageTopicPrefix            string
```

In `Load()`, populate:

```go
MessageBroker:                 strings.ToLower(env("MESSAGE_BROKER", "")),
KafkaBrokers:                  splitCSV(os.Getenv("KAFKA_BROKERS")),
KafkaConsumerGroup:            env("KAFKA_CONSUMER_GROUP", "blog-api"),
RabbitMQURL:                   env("RABBITMQ_URL", ""),
GooglePubSubProjectID:         env("GOOGLE_PUBSUB_PROJECT_ID", ""),
GooglePubSubCredentialsSource: env("GOOGLE_PUBSUB_CREDENTIALS_SOURCE", "workload-identity"),
MessageTopicPrefix:            env("MESSAGE_TOPIC_PREFIX", "blog."),
```

Add methods:

```go
func (c Config) MessagingEnabled() bool {
	return strings.TrimSpace(c.MessageBroker) != ""
}

func (c Config) ValidateMessaging() error {
	broker := strings.ToLower(strings.TrimSpace(c.MessageBroker))
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
```

Call `ValidateMessaging()` at the end of `Load()` so bad env fails fast for all commands.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 ./internal/platform/config/ -run Messaging`
Expected: PASS

- [ ] **Step 5: Commit** (only if user requested commits)

```bash
git add internal/platform/config/config.go internal/platform/config/messaging_test.go
git commit -m "$(cat <<'EOF'
feat(config): add MESSAGE_BROKER and transport settings

EOF
)"
```

---

### Task 2: Messaging package skeleton + Open misconfig tests

**Files:**
- Create: `internal/platform/messaging/messaging.go`
- Create: `internal/platform/messaging/messaging_test.go`
- Create: `internal/platform/messaging/kafka.go` (stub returning error until Task 3)
- Create: `internal/platform/messaging/rabbitmq.go` (stub)
- Create: `internal/platform/messaging/googlepubsub.go` (stub)

**Interfaces:**
- Consumes: `config.Config`, `config.ValidateMessaging`
- Produces:
  - `const BrokerKafka = "kafka"`, `BrokerRabbitMQ = "rabbitmq"`, `BrokerGooglePubSub = "googlepubsub"`
  - `type Bus struct { Broker string; Publisher message.Publisher; Subscriber message.Subscriber; Logger watermill.LoggerAdapter; cleanup []func() error }`
  - `func Open(ctx context.Context, cfg config.Config) (*Bus, error)`
  - `func (b *Bus) Close() error`
  - `func (b *Bus) Topic(name string) string` — applies `MessageTopicPrefix`
  - `func NormalizeBroker(raw string) (string, error)`

- [ ] **Step 1: Write failing tests**

```go
package messaging

import (
	"context"
	"testing"

	"github.com/turahe/blog-api/internal/platform/config"
)

func TestNormalizeBroker(t *testing.T) {
	t.Parallel()
	got, err := NormalizeBroker("Kafka")
	if err != nil || got != BrokerKafka {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := NormalizeBroker("nats"); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenRequiresMessaging(t *testing.T) {
	t.Parallel()
	_, err := Open(context.Background(), config.Config{})
	if err == nil {
		t.Fatal("expected error when MESSAGE_BROKER empty")
	}
}

func TestOpenRejectsIncompleteKafkaWithoutDial(t *testing.T) {
	t.Parallel()
	_, err := Open(context.Background(), config.Config{
		MessageBroker:      "kafka",
		KafkaConsumerGroup: "blog-api",
		// no brokers
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestTopicPrefix(t *testing.T) {
	t.Parallel()
	b := &Bus{topicPrefix: "blog."}
	if got := b.Topic("post.published"); got != "blog.post.published" {
		t.Fatalf("got %q", got)
	}
}
```

Note: put `topicPrefix` as unexported field on `Bus`, set in `Open`.

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test -count=1 ./internal/platform/messaging/`
Expected: FAIL package does not exist / undefined

- [ ] **Step 3: Add Watermill deps (pinned)**

Run:

```bash
go get github.com/ThreeDotsLabs/watermill@v1.5.1 \
  github.com/ThreeDotsLabs/watermill-kafka/v3@v3.0.6 \
  github.com/ThreeDotsLabs/watermill-amqp/v3@v3.0.2 \
  github.com/ThreeDotsLabs/watermill-googlecloud/v2@v2.0.1
```

If a version tag is missing, pick the latest **tagged** release matching major shown in the design (`v3` / `v2`) and record the exact versions in CHANGELOG later. Do not use `@latest`.

- [ ] **Step 4: Implement skeleton**

`messaging.go`:

```go
package messaging

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/turahe/blog-api/internal/platform/config"
)

const (
	BrokerKafka         = "kafka"
	BrokerRabbitMQ      = "rabbitmq"
	BrokerGooglePubSub  = "googlepubsub"
)

type Bus struct {
	Broker      string
	Publisher   message.Publisher
	Subscriber  message.Subscriber
	Logger      watermill.LoggerAdapter
	topicPrefix string
	cleanup     []func() error
}

func NormalizeBroker(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case BrokerKafka:
		return BrokerKafka, nil
	case BrokerRabbitMQ, "amqp", "rabbit":
		return BrokerRabbitMQ, nil
	case BrokerGooglePubSub, "gcp-pubsub", "pubsub":
		return BrokerGooglePubSub, nil
	case "":
		return "", fmt.Errorf("MESSAGE_BROKER is required")
	default:
		return "", fmt.Errorf("unsupported MESSAGE_BROKER %q", raw)
	}
}

func Open(ctx context.Context, cfg config.Config) (*Bus, error) {
	if err := cfg.ValidateMessaging(); err != nil {
		return nil, err
	}
	broker, err := NormalizeBroker(cfg.MessageBroker)
	if err != nil {
		return nil, err
	}
	logger := watermill.NewStdLogger(false, false)
	bus := &Bus{Broker: broker, Logger: logger, topicPrefix: cfg.MessageTopicPrefix}
	switch broker {
	case BrokerKafka:
		err = openKafka(ctx, bus, cfg)
	case BrokerRabbitMQ:
		err = openRabbitMQ(ctx, bus, cfg)
	case BrokerGooglePubSub:
		err = openGooglePubSub(ctx, bus, cfg)
	}
	if err != nil {
		_ = bus.Close()
		return nil, err
	}
	return bus, nil
}

func (b *Bus) Topic(name string) string {
	name = strings.TrimPrefix(name, "/")
	if b == nil || b.topicPrefix == "" {
		return name
	}
	if strings.HasPrefix(name, b.topicPrefix) {
		return name
	}
	return b.topicPrefix + name
}

func (b *Bus) Close() error {
	if b == nil {
		return nil
	}
	var first error
	if b.Publisher != nil {
		if err := b.Publisher.Close(); err != nil && first == nil {
			first = err
		}
	}
	if b.Subscriber != nil {
		if err := b.Subscriber.Close(); err != nil && first == nil {
			first = err
		}
	}
	for i := len(b.cleanup) - 1; i >= 0; i-- {
		if err := b.cleanup[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}
```

Stub openers return `fmt.Errorf("%s transport not implemented", broker)` until Task 3–5 fill them in — **except** validation paths already return before stubs for incomplete config. For `TestOpenRequiresMessaging`, empty broker fails in `NormalizeBroker` / `ValidateMessaging`.

Adjust `TestOpenRequiresMessaging` expectation to match: empty → `"MESSAGE_BROKER is required"` from `NormalizeBroker` after ValidateMessaging returns nil. Prefer calling `NormalizeBroker` first when empty:

Update `Open` to:

```go
broker, err := NormalizeBroker(cfg.MessageBroker)
if err != nil {
	return nil, err
}
if err := cfg.ValidateMessaging(); err != nil {
	return nil, err
}
```

- [ ] **Step 5: Run unit tests**

Run: `go test -count=1 ./internal/platform/messaging/ ./internal/platform/config/`
Expected: PASS for normalize/validation; Open with full kafka config may fail with "not implemented" until Task 3 — keep incomplete-kafka test only.

- [ ] **Step 6: Commit** (if requested)

---

### Task 3: Kafka transport

**Files:**
- Modify: `internal/platform/messaging/kafka.go`
- Modify: `internal/platform/messaging/messaging_test.go` (optional skip-if-no-broker integration later)

**Interfaces:**
- Consumes: `Bus`, `config.Config` Kafka fields
- Produces: `openKafka` sets `Publisher` + `Subscriber`

- [ ] **Step 1: Implement `openKafka`**

```go
package messaging

import (
	"context"

	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	"github.com/turahe/blog-api/internal/platform/config"
)

func openKafka(_ context.Context, bus *Bus, cfg config.Config) error {
	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:   cfg.KafkaBrokers,
			Marshaler: kafka.DefaultMarshaler{},
		},
		bus.Logger,
	)
	if err != nil {
		return err
	}
	subscriber, err := kafka.NewSubscriber(
		kafka.SubscriberConfig{
			Brokers:       cfg.KafkaBrokers,
			Unmarshaler:   kafka.DefaultMarshaler{},
			ConsumerGroup: cfg.KafkaConsumerGroup,
		},
		bus.Logger,
	)
	if err != nil {
		_ = publisher.Close()
		return err
	}
	bus.Publisher = publisher
	bus.Subscriber = subscriber
	return nil
}
```

Verify API against the installed `watermill-kafka/v3` package (`PublisherConfig` / `SubscriberConfig` field names). Adjust to match the module’s exported constructors if they differ.

- [ ] **Step 2: Build package**

Run: `go test -count=1 ./internal/platform/messaging/`
Expected: PASS

- [ ] **Step 3: Commit** (if requested)

---

### Task 4: RabbitMQ transport

**Files:**
- Modify: `internal/platform/messaging/rabbitmq.go`

**Interfaces:**
- Produces: `openRabbitMQ` sets Publisher + Subscriber via durable pub/sub config

- [ ] **Step 1: Implement `openRabbitMQ`**

```go
package messaging

import (
	"context"

	"github.com/ThreeDotsLabs/watermill-amqp/v3/pkg/amqp"
	"github.com/turahe/blog-api/internal/platform/config"
)

func openRabbitMQ(_ context.Context, bus *Bus, cfg config.Config) error {
	amqpConfig := amqp.NewDurablePubSubConfig(cfg.RabbitMQURL, amqp.GenerateQueueNameTopicName)
	publisher, err := amqp.NewPublisher(amqpConfig, bus.Logger)
	if err != nil {
		return err
	}
	subscriber, err := amqp.NewSubscriber(amqpConfig, bus.Logger)
	if err != nil {
		_ = publisher.Close()
		return err
	}
	bus.Publisher = publisher
	bus.Subscriber = subscriber
	return nil
}
```

Align with `watermill-amqp/v3` docs if helper names differ (`NewDurablePubSubConfig` vs similar).

- [ ] **Step 2: `go test -count=1 ./internal/platform/messaging/`** — PASS

- [ ] **Step 3: Commit** (if requested)

---

### Task 5: Google Cloud Pub/Sub transport

**Files:**
- Modify: `internal/platform/messaging/googlepubsub.go`

**Interfaces:**
- Produces: `openGooglePubSub`; credentials via ADC / file path option mirroring Cloud SQL pattern

- [ ] **Step 1: Implement `openGooglePubSub`**

Use `github.com/ThreeDotsLabs/watermill-googlecloud/v2/pkg/googlecloud` (confirm path after `go get`).

```go
package messaging

import (
	"context"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/pubsub/v2"
	"github.com/ThreeDotsLabs/watermill-googlecloud/v2/pkg/googlecloud"
	"github.com/turahe/blog-api/internal/platform/config"
	"google.golang.org/api/option"
)

func openGooglePubSub(ctx context.Context, bus *Bus, cfg config.Config) error {
	opts, err := googleCredentialsOptions(cfg.GooglePubSubCredentialsSource)
	if err != nil {
		return err
	}
	publisher, err := googlecloud.NewPublisher(ctx, googlecloud.PublisherConfig{
		ProjectID: cfg.GooglePubSubProjectID,
		ClientOptions: opts,
	}, bus.Logger)
	if err != nil {
		return err
	}
	subscriber, err := googlecloud.NewSubscriber(ctx, googlecloud.SubscriberConfig{
		ProjectID: cfg.GooglePubSubProjectID,
		ClientOptions: opts,
		GenerateSubscriptionName: googlecloud.TopicSubscriptionName,
	}, bus.Logger)
	if err != nil {
		_ = publisher.Close()
		return err
	}
	bus.Publisher = publisher
	bus.Subscriber = subscriber
	_ = pubsub.ScopePubSub // only if needed to keep import; remove unused imports after compile
	return nil
}

func googleCredentialsOptions(source string) ([]option.ClientOption, error) {
	source = strings.TrimSpace(source)
	switch {
	case source == "" || source == "workload-identity" || source == "adc":
		return nil, nil
	case strings.HasPrefix(source, "path:"):
		path := strings.TrimSpace(strings.TrimPrefix(source, "path:"))
		if path == "" {
			return nil, fmt.Errorf("GOOGLE_PUBSUB_CREDENTIALS_SOURCE path is empty")
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("GOOGLE_PUBSUB_CREDENTIALS_SOURCE: %w", err)
		}
		return []option.ClientOption{option.WithCredentialsFile(path)}, nil
	default:
		return nil, fmt.Errorf("unsupported GOOGLE_PUBSUB_CREDENTIALS_SOURCE %q", source)
	}
}
```

**Compile against installed module** and fix field names (`ClientOptions`, subscription generators) to match v2 API. Do not invent fields that do not compile.

- [ ] **Step 2: Unit-test credentials helper**

Add `TestGoogleCredentialsOptions` for empty/`path:` missing file cases (no live GCP call).

- [ ] **Step 3: `go test -count=1 ./internal/platform/messaging/`** — PASS

- [ ] **Step 4: Commit** (if requested)

---

### Task 6: `app worker` + doctor messaging check

**Files:**
- Create: `cmd/app/worker.go`
- Modify: `cmd/app/command.go`

**Interfaces:**
- Consumes: `messaging.Open`, `message.NewRouter`, config.Load
- Produces: working `app worker` that opens bus and blocks until SIGINT/SIGTERM; doctor prints `messaging (<broker>): ok` when enabled

- [ ] **Step 1: Implement worker**

`cmd/app/worker.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

func newWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run asynchronous event consumers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if !cfg.MessagingEnabled() {
				return fmt.Errorf("MESSAGE_BROKER must be set to run the worker")
			}
			logger := newLogger(cfg.Environment)
			bus, err := messaging.Open(ctx, cfg)
			if err != nil {
				return fmt.Errorf("open messaging: %w", err)
			}
			defer bus.Close()

			router, err := message.NewRouter(message.RouterConfig{}, bus.Logger)
			if err != nil {
				return err
			}
			// Placeholder no-op handler proves subscribe path; real outbox handlers come later.
			topic := bus.Topic("worker.heartbeat")
			router.AddNoPublisherHandler(
				"worker-heartbeat",
				topic,
				bus.Subscriber,
				func(msg *message.Message) error {
					logger.Debug("heartbeat message", "uuid", msg.UUID)
					msg.Ack()
					return nil
				},
			)

			logger.Info("worker started", "broker", bus.Broker, "topic", topic)
			go func() {
				<-ctx.Done()
				_ = router.Close()
			}()
			if err := router.Run(ctx); err != nil && ctx.Err() == nil {
				return fmt.Errorf("router: %w", err)
			}
			return nil
		},
	}
}
```

If `AddNoPublisherHandler` / Ack pattern differs for the Watermill version, use the documented handler signature (`func(*message.Message) error` returning nil implies Ack).

- [ ] **Step 2: Wire command**

In `newRootCommand`, replace `newPlaceholderCommand("worker", ...)` with `newWorkerCommand()`. Keep scheduler as placeholder.

- [ ] **Step 3: Extend doctor**

After redis ok line:

```go
if app.Config.MessagingEnabled() {
	bus, err := messaging.Open(cmd.Context(), app.Config)
	if err != nil {
		return fmt.Errorf("messaging: %w", err)
	}
	_ = bus.Close()
	fmt.Fprintf(cmd.OutOrStdout(), "messaging (%s): ok\n", bus.Broker)
} else {
	fmt.Fprintln(cmd.OutOrStdout(), "messaging: skipped (MESSAGE_BROKER unset)")
}
```

Note: doctor currently builds full Runtime (DB+Redis). Keep that; add messaging probe after.

- [ ] **Step 4: Build**

Run: `go build -o /tmp/blog-api ./cmd/app && go test -count=1 ./cmd/... ./internal/...`
Expected: PASS / binary built

- [ ] **Step 5: Commit** (if requested)

---

### Task 7: Compose messaging profile + Makefile + env

**Files:**
- Modify: `compose.yaml`
- Modify: `Makefile`
- Modify: `.env.example`

- [ ] **Step 1: Add Compose services**

Append to `compose.yaml` (KRaft single-node Kafka + RabbitMQ):

```yaml
  kafka:
    profiles: ["messaging"]
    image: bitnami/kafka:3.9
    ports:
      - "9092:9092"
    environment:
      KAFKA_CFG_NODE_ID: "0"
      KAFKA_CFG_PROCESS_ROLES: controller,broker
      KAFKA_CFG_LISTENERS: PLAINTEXT://:9092,CONTROLLER://:9093
      KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP: CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT
      KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: 0@kafka:9093
      KAFKA_CFG_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_CFG_ADVERTISED_LISTENERS: PLAINTEXT://127.0.0.1:9092
      ALLOW_PLAINTEXT_LISTENER: "yes"
    healthcheck:
      test: ["CMD-SHELL", "kafka-topics.sh --bootstrap-server localhost:9092 --list || exit 1"]
      interval: 10s
      timeout: 10s
      retries: 10

  rabbitmq:
    profiles: ["messaging"]
    image: rabbitmq:3.13-management-alpine
    ports:
      - "5672:5672"
      - "15672:15672"
    environment:
      RABBITMQ_DEFAULT_USER: blog
      RABBITMQ_DEFAULT_PASS: blog
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"]
      interval: 5s
      timeout: 5s
      retries: 20
```

If Bitnami env keys change, use a known-good single-node Kafka image documented in the PR notes; keep host port `9092`.

- [ ] **Step 2: Makefile targets**

```makefile
infra-up-messaging:
	docker compose --profile messaging up -d kafka rabbitmq

infra-down-messaging:
	docker compose stop kafka rabbitmq
	docker compose rm -f kafka rabbitmq
```

Add to `.PHONY`.

- [ ] **Step 3: `.env.example` messaging block**

```bash
# Messaging (Watermill). Leave MESSAGE_BROKER empty to disable worker bus.
# MESSAGE_BROKER=kafka|rabbitmq|googlepubsub
MESSAGE_BROKER=
MESSAGE_TOPIC_PREFIX=blog.
KAFKA_BROKERS=127.0.0.1:9092
KAFKA_CONSUMER_GROUP=blog-api
RABBITMQ_URL=amqp://blog:blog@127.0.0.1:5672/
# GOOGLE_PUBSUB_PROJECT_ID=my-project
# GOOGLE_PUBSUB_CREDENTIALS_SOURCE=workload-identity
```

- [ ] **Step 4: Smoke Compose (if Docker available)**

Run: `docker compose --profile messaging up -d kafka rabbitmq`
Expected: healthy containers; `make infra-down-messaging` stops/removes only kafka and rabbitmq (base stack stays up).

- [ ] **Step 5: Commit** (if requested)

---

### Task 8: Docs + CHANGELOG + spec status

**Files:**
- Modify: `docs/deployment/local.md`
- Modify: `docs/architecture/architecture.md` (platform tree: `messaging/` next to `database/`)
- Modify: `docs/architecture/deployment.md` (broker transport line)
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/specs/2026-07-30-messaging-brokers-design.md` status → `implemented`
- Verify: `docs/architecture/tech-stack.md` and `docs/backend/events.md` still accurate (already done)

- [ ] **Step 1: Update local.md**

Add section:

```markdown
## Messaging (optional)

```bash
make infra-up-messaging   # kafka :9092, rabbitmq :5672 / management :15672
```

Set in `.env`:

- Kafka: `MESSAGE_BROKER=kafka`, `KAFKA_BROKERS=127.0.0.1:9092`
- RabbitMQ: `MESSAGE_BROKER=rabbitmq`, `RABBITMQ_URL=amqp://blog:blog@127.0.0.1:5672/`

Run worker:

```bash
go run ./cmd/app worker
```

Google Cloud Pub/Sub uses a real GCP project (`MESSAGE_BROKER=googlepubsub`); no Compose emulator.
```

- [ ] **Step 2: Architecture tree** — replace or add `messaging/` under `internal/platform/`

- [ ] **Step 3: CHANGELOG entry** under new `## 2026-07-30 — Multi-broker Watermill transports`

- [ ] **Step 4: Relative link check**

Run: `node scripts/docs/validate_relative_links.cjs docs contracts paths README.md`
Expected: exit 0 (or fix broken links introduced)

- [ ] **Step 5: Final verification**

```bash
go test -count=1 ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build -o /tmp/blog-api ./cmd/app
```

Expected: all PASS / clean build

- [ ] **Step 6: Commit** (if requested)

---

## Spec coverage checklist

| Spec requirement | Task |
|------------------|------|
| Watermill factory kafka/rabbitmq/googlepubsub | 2–5 |
| Config env vars + validation | 1 |
| Compose Kafka + RabbitMQ profiles | 7 |
| `app worker` lifecycle | 6 |
| Doctor / fail-fast misconfig | 1, 6 |
| Docs tech-stack/events (already done) + local/deploy/CHANGELOG | 8 |
| No full outbox catalogue / no GCP emulator | honored (non-goals) |
| Core free of Watermill | enforced by package placement |

## Self-review notes

- No TBD placeholders left in tasks; API field names for Watermill modules must be confirmed against installed packages at implement time (Tasks 3–5 explicitly say compile-fix).
- `Bus.Topic` / `Open` / `ValidateMessaging` names are consistent across tasks.
- Commits are optional pending user request (matches repo commit policy).
