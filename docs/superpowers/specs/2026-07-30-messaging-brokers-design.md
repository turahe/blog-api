# Multi-Broker Messaging Design

Date: 2026-07-30  
Status: implemented — implementation plan at [../plans/2026-07-30-messaging-brokers.md](../plans/2026-07-30-messaging-brokers.md)  
Scope: Watermill transport selection for Kafka, RabbitMQ, and Google Cloud Pub/Sub, plus local Compose for Kafka/RabbitMQ.

## Goal

Keep Watermill as the event-bus abstraction and make the underlying broker selectable at runtime via configuration, matching the multi-dialect database pattern. Local development must be able to run Kafka and RabbitMQ through Docker Compose. Google Cloud Pub/Sub remains a managed/production target (no local emulator in this scope).

## Non-goals

- Replacing Watermill with vendor-native SDKs in domain/core code
- Implementing the full outbox consumer catalogue in this change
- Shipping a Google Pub/Sub emulator in Compose
- Exactly-once delivery guarantees (platform remains at-least-once; consumers must be idempotent)

## Architecture

```text
Domain / services
        │  (ports only — no Watermill imports)
        ▼
Outbound event adapter / outbox publisher
        ▼
internal/platform/messaging  ← factory(MESSAGE_BROKER)
        ├── kafka      (watermill-kafka/v3)
        ├── rabbitmq   (watermill-amqp/v3)
        └── googlepubsub (watermill-googlecloud/v2)
```

- Core packages must not import Watermill or broker SDKs.
- `app worker` uses the messaging factory to create Publisher/Subscriber and run the Watermill router lifecycle.
- HTTP `serve` may publish via the same factory (or via outbox → worker); bootstrap should share config validation.

## Configuration

| Variable | Purpose |
|----------|---------|
| `MESSAGE_BROKER` | `kafka` \| `rabbitmq` \| `googlepubsub` (required when workers/publishers are enabled) |
| `KAFKA_BROKERS` | Comma-separated broker list (e.g. `127.0.0.1:9092`) |
| `KAFKA_CONSUMER_GROUP` | Consumer group for `app worker` |
| `RABBITMQ_URL` | AMQP URL (e.g. `amqp://blog:blog@127.0.0.1:5672/`) |
| `GOOGLE_PUBSUB_PROJECT_ID` | GCP project id |
| `GOOGLE_PUBSUB_CREDENTIALS_SOURCE` | `workload-identity` \| `adc` \| `path:/secrets/sa-key.json` (same pattern as Cloud SQL) |
| `MESSAGE_TOPIC_PREFIX` | Optional topic/exchange prefix for env isolation (default empty or `blog.`) |

Validation rules:

- Unknown `MESSAGE_BROKER` → config error
- Broker-specific required vars must be present when that broker is selected
- Production must not use insecure defaults without explicit override

## Local Compose

Add optional Compose services (prefer profiles so default `infra-up` stays light):

- `kafka` (+ ZooKeeper or KRaft single-node) on `9092`
- `rabbitmq` (management optional on `15672`) on `5672`

Document profile usage in deployment/local docs and Makefile targets if needed (`infra-up-messaging` or compose `--profile messaging`).

## Delivery semantics

- At-least-once across all brokers
- Consumers remain idempotent (dedupe by event ID / outbox ID)
- Transactional outbox remains the write-path durability mechanism; brokers are the fan-out transport

## Testing

- Unit: normalize broker name, validate required env per broker, factory rejects misconfig without dialing
- Integration (optional / CI with Compose profile): publish + subscribe round-trip on Kafka and RabbitMQ
- Google Pub/Sub: unit/config tests only in this phase; live integration against GCP is manual/ops

## Docs updates

- `docs/architecture/tech-stack.md` — event bus line + Data and Async broker table
- `docs/backend/events.md` — Event Bus Strategy section for selectable transports
- `docs/architecture/deployment.md` / `docs/deployment/local.md` — local broker profiles
- `.env.example` — messaging vars
- `CHANGELOG.md` — entry when implementation lands

## Implementation packages (planned)

- `internal/platform/messaging` — Open/factory, Close, broker constants
- Config fields in `internal/platform/config`
- Wire `app worker` (replace placeholder) at least for router start/stop + broker ping in `app doctor`
- Compose + Makefile + env example

## Success criteria

1. `MESSAGE_BROKER=kafka|rabbitmq|googlepubsub` selects the correct Watermill adapter
2. Local Kafka and RabbitMQ start via Compose profile
3. Tech stack and events docs describe all three transports
4. Core remains free of Watermill/broker imports
5. Misconfigured broker fails fast at startup with a clear error
