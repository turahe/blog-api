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
  - AsyncAPI: `npx @asyncapi/cli validate docs/architecture/asyncapi.yaml`
- run automated tests
- apply database migrations with `app migrate up`
- deploy API (`app serve`)
- deploy background workers (`app worker`)
- deploy scheduled jobs if separated (`app scheduler`)
- validate runtime dependencies with `app doctor`
- verify health and readiness endpoints

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
- event publishing and consumers are healthy
- critical auth flows succeed in smoke tests

## Operational handbook

Local Compose, Docker image, and release checklists: [docs/deployment](../deployment/README.md).
