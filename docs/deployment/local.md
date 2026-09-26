# Local Deployment

## Prerequisites

- Docker + Compose — every `make` target (tests, lint, swagger, dev keys, stack) runs in containers
- Go `1.27.1` (optional; only for running the API directly on the host with `go run` — see [go.mod](../../go.mod))

## Boot sequence (Docker — recommended)

```bash
cp .env.example .env
# edit APP_SESSION_KEY, APP_CSRF_KEY, APP_PEPPER to random values (≥32 chars for session key)
# JWT ES256: .env.example already points at configs/dev/*.pem

make dev-keys          # generate configs/dev/*.pem if missing; chmod 644 (containers run as nonroot)
make docker-up         # build + every service (see Services below) + migrate + api
make docker-seed       # roles + admin@example.com / ChangeMeNow!123
# API: http://localhost:8080  (Swagger UI: /swagger/index.html when local)
# Mailpit: http://localhost:8025  (SMTP catcher for password and email-change mail)
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

make infra-up          # every service except migrate and api
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
| postgres | 5432 | primary DB, PostgreSQL 18 (`blog`/`blog`/`blog`) |
| redis | 6379 | cache / ephemeral |
| rustfs | 9000 / 9001 | S3-compatible media + console |
| imgproxy | 8081 (loopback) | image transforms from the private bucket |
| mailpit | 1025 / 8025 | SMTP catcher + web UI |
| kafka | 9092 | message broker (KRaft single-node) |
| rabbitmq | 5672 / 15672 | message broker + management UI |

Compose file: [compose.yaml](../../compose.yaml). Data dirs under `./.data/` (gitignored).

## Health checks

- `GET /health/live`
- `GET /health/ready` (database, Redis, and the broker when `MESSAGE_BROKER` is set)
- `GET /health/version`
- Convenience: `GET /api/v1/health` (alias of live; not in OpenAPI)

## Tear down

```bash
make docker-down   # or: make infra-down
```

Volumes under `./.data/` persist until removed manually.

## Upgrading local data from PostgreSQL 16

Compose runs `postgres:18-alpine` with its data in `./.data/postgres18`. A PostgreSQL 16
cluster from before the upgrade stays in `./.data/postgres`; the 18 server cannot read it, so
the stack starts with an empty database. Either run `make docker-migrate` and
`make docker-seed` for a fresh start, or copy the old data across:

```bash
make docker-down
docker run -d --rm --name pg16 -v "$PWD/.data/postgres:/var/lib/postgresql/data" postgres:16-alpine
until docker exec pg16 pg_isready -q; do sleep 1; done
docker exec pg16 pg_dump -U blog -d blog > blog-pg16.sql
docker stop pg16

docker compose up -d --wait postgres
docker compose exec -T postgres psql -U blog -d blog -v ON_ERROR_STOP=1 < blog-pg16.sql
make docker-up
```

Once the new stack looks right, delete `blog-pg16.sql` and `./.data/postgres` (owned by the
container user, so `sudo rm -rf .data/postgres`).

## Messaging

`make docker-up` and `make infra-up` start Kafka (`apache/kafka:3.9.0`) and RabbitMQ
(`rabbitmq:4-management-alpine`) along with everything else. The Compose api service still
clears `MESSAGE_BROKER`, because no worker runs in the stack, so it delivers inline.

To run the worker against Compose brokers (host or a custom compose service), set in `.env`:

- Kafka: `MESSAGE_BROKER=kafka`, `KAFKA_BROKERS=127.0.0.1:9092`
- RabbitMQ: `MESSAGE_BROKER=rabbitmq`, `RABBITMQ_URL=amqp://blog:blog@127.0.0.1:5672/`

```bash
go run . worker
```

Google Cloud Pub/Sub uses a real GCP project (`MESSAGE_BROKER=googlepubsub`); no Compose emulator.
