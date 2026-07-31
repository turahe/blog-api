# Local Deployment

## Prerequisites

- Go `1.26.5` (see [go.mod](../../go.mod))
- Docker + Compose
- Node (for contract validation scripts)
- Python 3 (route generation)

## Boot sequence

```bash
cp .env.example .env
# edit APP_SESSION_KEY, APP_CSRF_KEY, APP_PEPPER to random values (≥32 chars for session key)
set -a
. ./.env
set +a

make infra-up          # postgres, redis, minio
make migrate-up        # go run ./cmd migrate up
go run ./cmd seed      # roles + admin@example.com / ChangeMeNow!123
make run               # go run ./cmd serve
```

The Go process does not load `.env` automatically; source it as shown above. See
[config.md](./config.md) for all variables and the distinction between application and
Compose-only settings.

Auth smoke:

```bash
curl -sS -X POST localhost:8080/api/v1/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"admin@example.com","password":"ChangeMeNow!123"}'
```

Optional:

```bash
make contracts
make routes
make test
```

## Services (Compose)

| Service | Host port | Role |
| --- | --- | --- |
| postgres | 5432 | primary DB (`blog`/`blog`/`blog`) |
| redis | 6379 | cache / ephemeral |
| minio | 9000 / 9001 | S3-compatible media + console |

Compose file: [compose.yaml](../../compose.yaml). Data dirs under `./data/` (gitignored).

## Health checks

- `GET /health/live`
- `GET /health/ready`
- `GET /health/version`
- Convenience: `GET /api/v1/health` (alias of live; not in OpenAPI)

## Tear down

```bash
make infra-down
```

Volumes under `./data/` persist until removed manually.

## Messaging (optional)

```bash
make infra-up-messaging   # kafka :9092, rabbitmq :5672 / management :15672
make infra-down-messaging # stop/remove kafka and rabbitmq only
```

Compose profile `messaging` starts Kafka (`apache/kafka:3.9.0`, KRaft single-node) and RabbitMQ (`rabbitmq:3.13-management-alpine`). Default `make infra-up` does not start brokers.

Set in `.env`:

- Kafka: `MESSAGE_BROKER=kafka`, `KAFKA_BROKERS=127.0.0.1:9092`
- RabbitMQ: `MESSAGE_BROKER=rabbitmq`, `RABBITMQ_URL=amqp://blog:blog@127.0.0.1:5672/`

Run worker:

```bash
go run ./cmd worker
```

Google Cloud Pub/Sub uses a real GCP project (`MESSAGE_BROKER=googlepubsub`); no Compose emulator.
