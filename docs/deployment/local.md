# Local Deployment

## Prerequisites

- Docker + Compose — every `make` target (tests, lint, swagger, dev keys, stack) runs in containers
- Go `1.26.5` (optional; only for running the API directly on the host with `go run` — see [go.mod](../../go.mod))

## Boot sequence (Docker — recommended)

```bash
cp .env.example .env
# edit APP_SESSION_KEY, APP_CSRF_KEY, APP_PEPPER to random values (≥32 chars for session key)
# JWT ES256: .env.example already points at configs/dev/*.pem

make dev-keys          # generate configs/dev/*.pem if missing; chmod 644 (containers run as nonroot)
make docker-up         # build + postgres/redis/rustfs + migrate + api
make docker-seed       # roles + admin@example.com / ChangeMeNow!123
# API: http://localhost:8080  (Swagger UI: /swagger/index.html when local)
```

Compose overrides `DB_HOST`/`REDIS_HOST`/`S3_ENDPOINT` to service DNS names so the same
`.env` works for both Docker and a host `go run . serve` (host keeps `127.0.0.1`).

Useful:

```bash
make docker-logs       # follow api logs
make docker-migrate    # re-run migrations
make docker-down       # stop the stack (volumes under ./.data/ keep data)
```

## Boot sequence (host API + Compose infra)

For IDE debugging; requires Go on the host.

```bash
cp .env.example .env
# edit secrets as above

make infra-up          # postgres, redis, rustfs
go run . migrate up
go run . seed          # roles + admin@example.com / ChangeMeNow!123
go run . serve         # http://localhost:8080
```

The Cobra CLI auto-loads `.env` from the working directory when present (`--env-file`
overrides; `--env-file=-` disables). See [config.md](./config.md) for all variables and the
distinction between application and Compose-only settings.

Auth smoke:

```bash
curl -sS -X POST localhost:8080/api/v1/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"admin@example.com","password":"ChangeMeNow!123"}'
```

Optional:

```bash
make routes-check      # Gin route registration smoke tests (in golang container)
make test              # full test suite (in golang container)
make lint              # golangci-lint (in golangci-lint container)
```

## Services (Compose)

| Service | Host port | Role |
| --- | --- | --- |
| api | 8080 | HTTP API (built from [Dockerfile](../../Dockerfile)) |
| migrate | — | one-shot `migrate up` before api starts |
| postgres | 5432 | primary DB (`blog`/`blog`/`blog`) |
| redis | 6379 | cache / ephemeral |
| rustfs | 9000 / 9001 | S3-compatible media + console |

Compose file: [compose.yaml](../../compose.yaml). Data dirs under `./.data/` (gitignored).

## Health checks

- `GET /health/live`
- `GET /health/ready`
- `GET /health/version`
- Convenience: `GET /api/v1/health` (alias of live; not in OpenAPI)

## Tear down

```bash
make docker-down   # or: make infra-down
```

Volumes under `./.data/` persist until removed manually.

## Messaging (optional)

```bash
make infra-up-messaging   # kafka :9092, rabbitmq :5672 / management :15672
make infra-down-messaging # stop/remove kafka and rabbitmq only
```

Compose profile `messaging` starts Kafka (`apache/kafka:3.9.0`, KRaft single-node) and RabbitMQ (`rabbitmq:3.13-management-alpine`). Default `make docker-up` / `make infra-up` does not start brokers; the api service clears `MESSAGE_BROKER` so serve works without them.

To run the worker against Compose brokers (host or a custom compose service), set in `.env`:

- Kafka: `MESSAGE_BROKER=kafka`, `KAFKA_BROKERS=127.0.0.1:9092`
- RabbitMQ: `MESSAGE_BROKER=rabbitmq`, `RABBITMQ_URL=amqp://blog:blog@127.0.0.1:5672/`

```bash
go run . worker
```

Google Cloud Pub/Sub uses a real GCP project (`MESSAGE_BROKER=googlepubsub`); no Compose emulator.
