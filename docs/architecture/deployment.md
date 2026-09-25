# Deployment

## Environments

Recommended environments:

- local
- staging
- production

## Local Environment

Use Docker Compose as the standard local environment.

Local Compose should be able to run:

- API service
- PostgreSQL
- Redis
- optional Kafka (`apache/kafka:3.9.0`) and RabbitMQ via Compose profile `messaging` (see [local deployment](../deployment/local.md#messaging-optional))
- local S3-compatible object storage such as MinIO for media workflows
- optional worker service for async consumers

## Runtime Components

- API service
- PostgreSQL
- Redis
- Watermill transport backend selected by `MESSAGE_BROKER` (`kafka`, `rabbitmq`, or `googlepubsub`; factory in `internal/platform/messaging`)
- object storage bucket for originals and optionally cached variants
- worker process for async jobs if consumers are split from the API

## Process roles

One image runs three commands. Deploy them as separate services so each scales and restarts
on its own:

| Process | Command | Replicas | Probes |
| --- | --- | --- | --- |
| API | `app serve` | 2+ behind the load balancer | `GET /health/live`, `GET /health/ready` on `APP_ADDR` |
| Worker | `app worker` | 1+ when `MESSAGE_BROKER` is set | `GET /healthz`, `GET /readyz` on `METRICS_ADDR` |
| Scheduler | `app scheduler` | 1+ (jobs run once per interval across replicas) | process liveness |

- **API readiness** fails only on the database or Redis. A broker outage is listed as a
  non-critical dependency: writes keep landing in the outbox, so replicas stay in rotation.
- **Worker readiness** fails when the database or the broker is down. Set `METRICS_ADDR`
  (for example `0.0.0.0:9090`) on workers so the probes exist; keep that port private.
- **Shared settings.** Workers need the same database, `MESSAGE_BROKER`, and
  `MESSAGE_TOPIC_PREFIX` as the API, plus `APP_ENCRYPTION_KEY` and `SMTP_*` to send queued
  email. The scheduler needs only the database settings.
- **Scaling.** Relays share the outbox with `FOR UPDATE SKIP LOCKED`, and consumers share a
  subscription (Kafka consumer group, RabbitMQ queue, Pub/Sub subscription), so worker
  replicas split the load. `CONSUMER_CONCURRENCY` adds handlers inside one worker.
- **Shutdown.** On SIGTERM the worker stops consuming and waits up to 30s for in-flight
  handlers; give it a termination grace period above that. Unacked messages are redelivered.
- **Load shedding.** `HTTP_MAX_INFLIGHT` caps concurrent API requests per replica; consumer
  circuit breakers pause a worker whose SMTP or database keeps failing.

Incident steps: [runbook.md](../deployment/runbook.md).

## Configuration

Use environment variables for:

- application address and mode
- database DSN
- Redis connection settings
- session secrets
- encryption keys
- OAuth provider credentials
- email provider credentials
- `MESSAGE_BROKER` and broker-specific settings (`KAFKA_BROKERS`, `RABBITMQ_URL`, `GOOGLE_PUBSUB_PROJECT_ID`, …)
- media storage driver, bucket, region, endpoint, and credentials
- image transform cache TTL and size controls

## Release Process

- build application artifact
- validate API and event contracts locally
  - Swagger: `make swagger` and commit `docs/`
  - AsyncAPI: `make asyncapi-validate` (also run by the `AsyncAPI` workflow)
- run automated tests
- apply database migrations with `app migrate up`
- deploy API (`app serve`)
- deploy background workers (`app worker`)
- deploy scheduled jobs if separated (`app scheduler`)
- validate runtime dependencies with `app doctor`
- verify health and readiness endpoints (`/health/ready` on the API, `/readyz` on workers)

## Migration Policy

- migrations must be explicit and reversible where practical
- run migrations before enabling new traffic-dependent features
- do not mix destructive schema changes with unverified code deploys

## Rollback Strategy

- support rolling back the app independently of non-breaking schema changes
- keep feature flags for high-risk launches when possible
- preserve audit trails during rollback

## Post-Deploy Checks

- health endpoints pass
- database connectivity works
- Redis connectivity works
- event publishing and consumers are healthy (`blog_outbox_lag_seconds` stays low, worker
  `/readyz` is 200)
- critical auth flows succeed in smoke tests

## Operational handbook

Local Compose, Docker image, and release checklists: [docs/deployment](../deployment/README.md).
