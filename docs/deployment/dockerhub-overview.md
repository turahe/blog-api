# Blog Platform REST API

A contracts-first, production-ready Go backend for a multi-role blog platform.

The API includes authentication, RBAC, media management, analytics, post revisions, moderation, impersonation, audit logs, and transactional domain events.

## Highlights

- Email/password authentication with Argon2id and ES256 access tokens
- Secure sessions, TOTP 2FA, OAuth/OIDC login, and password reset flows
- Role-based access control for superadmins, admins, editors, authors, and moderators
- Draft, scheduled, published, and archived content workflows
- Append-only audit logs and privacy-conscious analytics
- S3-compatible media storage with image transformation support
- PostgreSQL, Redis, and optional Kafka, RabbitMQ, or Google Pub/Sub integrations
- OpenAPI and AsyncAPI contracts
- Prometheus metrics and health/readiness endpoints

## Quick Start

The production image listens on port `8080` and runs as the unprivileged `nonroot` user.

```bash
docker pull <dockerhub-namespace>/blog-api:latest

docker run --rm \
  --name blog-api \
  --env-file .env \
  -p 8080:8080 \
  <dockerhub-namespace>/blog-api:latest \
  serve
```

Replace `<dockerhub-namespace>` with the Docker Hub account or organization that publishes the image.

The API is then available at:

```text
http://localhost:8080
```

## Configuration

The container is configured through environment variables. Do not bake `.env` files, private keys, or other secrets into the image.

At minimum, configure:

```dotenv
APP_ENV=production
APP_ADDR=0.0.0.0:8080
DB_HOST=postgres
DB_PORT=5432
DB_USER=blog
DB_PASSWORD=change-me
DB_NAME=blog
DB_SSLMODE=require
REDIS_HOST=redis
REDIS_PORT=6379
APP_SESSION_KEY=use-a-random-value-of-at-least-32-characters
APP_JWT_PRIVATE_KEY_PATH=/run/secrets/jwt-private.pem
APP_JWT_PUBLIC_KEY_PATH=/run/secrets/jwt-public.pem
```

Add S3-compatible storage variables when media uploads are enabled:

```dotenv
S3_ENDPOINT=https://s3.example.com
S3_REGION=auto
S3_BUCKET=blog-media
S3_ACCESS_KEY=change-me
S3_SECRET_KEY=change-me
S3_FORCE_PATH_STYLE=false
```

See the [configuration reference](https://github.com/turahe/blog-api/blob/main/docs/deployment/config.md) for all supported variables and validation rules.

## Database Migrations

Run migrations before starting the API:

```bash
docker run --rm \
  --env-file .env \
  <dockerhub-namespace>/blog-api:latest \
  migrate up
```

For production deployments, run migrations as a one-off release job and wait for completion before rolling out the API containers.

## Health and Metrics

- API health: `GET /api/v1/health`
- Prometheus metrics: configure `METRICS_ADDR=0.0.0.0:9090`
- Keep the metrics listener private and expose it only to your monitoring system.

Example with metrics enabled:

```bash
docker run --rm \
  --env-file .env \
  -e METRICS_ADDR=0.0.0.0:9090 \
  -p 8080:8080 \
  -p 127.0.0.1:9090:9090 \
  <dockerhub-namespace>/blog-api:latest \
  serve
```

## Docker Compose

For local development with PostgreSQL, Redis, RustFS, imgproxy, Mailpit, Kafka, and RabbitMQ:

```bash
git clone https://github.com/turahe/blog-api.git
cd blog-api
cp .env.example .env
docker compose up --build
```

The Compose stack is intended for development and evaluation. Use managed or separately operated dependencies for production.

## Image Details

| Property | Value |
| --- | --- |
| Runtime image | `gcr.io/distroless/static-debian12:nonroot` |
| Entrypoint | `/bin/app` |
| Default command | `serve` |
| HTTP port | `8080` |
| Metrics port | `9090` when enabled |
| Runtime user | `nonroot` |
| CGO | Disabled |

The final image contains only the statically linked application binary and does not include a shell or package manager.

## Documentation

- [Project README](https://github.com/turahe/blog-api)
- [Docker deployment guide](https://github.com/turahe/blog-api/blob/main/docs/deployment/docker.md)
- [Configuration reference](https://github.com/turahe/blog-api/blob/main/docs/deployment/config.md)
- [OpenAPI specification](https://github.com/turahe/blog-api/blob/main/docs/swagger.json)
- [Architecture](https://github.com/turahe/blog-api/blob/main/docs/architecture/architecture.md)

## License

Released under the [MIT License](https://github.com/turahe/blog-api/blob/main/LICENSE).
