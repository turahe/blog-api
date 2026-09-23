# Project Configuration

The application is configured exclusively through process environment variables.
`internal/platform/config/config.go` is the source of truth for variables consumed by the
Go process. [.env.example](../../.env.example) is the local template and also supplies
variable substitution for [compose.yaml](../../compose.yaml).

## Loading configuration

The application does **not** load `.env` files itself. Export the variables before running
the CLI:

```bash
cp .env.example .env
# Edit secrets and the selected database/message-broker settings.
set -a
. ./.env
set +a

go run ./cmd doctor
go run ./cmd serve
```

Docker accepts the same file explicitly:

```bash
docker run --rm --env-file .env -p 8080:8080 blog-api:local serve
```

In staging and production, inject variables through the deployment platform's secret and
configuration facilities. Do not copy `.env` into the image.

## Value formats

- Durations use Go duration syntax: `500ms`, `15s`, `30m`, `24h`.
- Boolean values use values accepted by `strconv.ParseBool`, normally `true` or `false`.
- Lists are comma-separated; surrounding whitespace and empty entries are removed.
- Empty strings use the documented fallback unless a variable is read as a list.
- Invalid duration, boolean, or integer values currently fall back silently. Validate the
  effective configuration with `go run ./cmd doctor` before deployment.
- Only the exact value `APP_ENV=production` activates production-only validation.

## Application and HTTP server

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `APP_ENV` | `local` | No | Runtime environment. Use `production` to enable production safety checks. |
| `APP_ADDR` | `0.0.0.0:8080` | Non-empty | HTTP listen address for `app serve`. |
| `APP_TRUSTED_PROXIES` | empty | No | Comma-separated proxies trusted by Gin. Leave empty unless the proxy addresses are known. |
| `APP_SHUTDOWN_TIMEOUT` | `15s` | No | Graceful shutdown deadline. |
| `APP_READ_TIMEOUT` | `15s` | No | Maximum request body read time. |
| `APP_READ_HEADER_TIMEOUT` | `5s` | No | Maximum request header read time. |
| `APP_IDLE_TIMEOUT` | `60s` | No | Keep-alive idle timeout. |

Do not use `0.0.0.0/0`, `*`, or arbitrary client-controlled addresses in
`APP_TRUSTED_PROXIES`. See [docker.md](./docker.md) for reverse-proxy deployment notes.

## Authentication and security

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `APP_SESSION_KEY` | local development fallback | Yes in production | Pepper for opaque refresh/reset token hashes. Production requires at least 32 characters. |
| `APP_CSRF_KEY` | empty | Not currently enforced | Reserved CSRF secret loaded into config; CSRF integration is not wired yet. |
| `APP_PEPPER` | empty | Not currently enforced | Reserved server-side pepper loaded into config; current password hashing does not consume it. |
| `APP_JWT_PRIVATE_KEY` / `APP_JWT_PRIVATE_KEY_PATH` | none | Yes | RSA private key PEM for RS256 access-token signing. Path wins when both are set. |
| `APP_JWT_PUBLIC_KEY` / `APP_JWT_PUBLIC_KEY_PATH` | none | Yes | RSA public key PEM for RS256 verification. Path wins when both are set. |
| `APP_JWT_ISSUER` | `blog-api` | No | JWT issuer claim. |
| `APP_ACCESS_TOKEN_TTL` | `15m` | No | Access-token lifetime. |
| `APP_REFRESH_TOKEN_TTL` | `720h` | No | Refresh-session lifetime. |

Use independent random values for each secret. Never log or commit production PEMs.
Local development can point at [`configs/dev/`](../../configs/dev/README.md). Rotating the RSA
private key invalidates existing access tokens; rotating `APP_SESSION_KEY` invalidates hashed
refresh/reset tokens. Additional handling rules are in
[secrets-and-headers.md](../security/secrets-and-headers.md).

## Database

The connection mode is selected by `DB_INSTANCE_CONNECTION_NAME`:

- Empty: connect directly using split `DB_*` fields (`DB_DRIVER`, `DB_HOST`, `DB_PORT`, …).
- Set: connect through the Google Cloud SQL connector using the Cloud SQL variables below.
  `DB_HOST` / `DB_PORT` / `DB_SSLMODE` are ignored in Cloud SQL mode.

### Direct connection

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `DB_DRIVER` | `postgres` | No | `postgres`, `mysql`, or `sqlserver`. Aliases `pg`, `postgresql`, `mariadb`, and `mssql` are accepted. |
| `DB_HOST` | `127.0.0.1` | Non-empty | Database hostname or IP. |
| `DB_PORT` | driver default | No | Port (`5432` postgres, `3306` mysql, `1433` sqlserver when unset). |
| `DB_USER` | `blog` | Non-empty | Database user. |
| `DB_PASSWORD` | `blog` | No | Database password. |
| `DB_NAME` | `blog` | Non-empty | Database name. |
| `DB_SSLMODE` | `disable` | No | PostgreSQL `sslmode` only (`disable`, `require`, `verify-full`, …). |

Examples:

```dotenv
# PostgreSQL
DB_DRIVER=postgres
DB_HOST=db.example.com
DB_PORT=5432
DB_USER=blog
DB_PASSWORD=secret
DB_NAME=blog
DB_SSLMODE=require

# MySQL
DB_DRIVER=mysql
DB_HOST=db.example.com
DB_PORT=3306
DB_USER=blog
DB_PASSWORD=secret
DB_NAME=blog

# Microsoft SQL Server
DB_DRIVER=sqlserver
DB_HOST=db.example.com
DB_PORT=1433
DB_USER=blog
DB_PASSWORD=secret
DB_NAME=blog
```

The application builds a driver-specific DSN from these fields. `DATABASE_URL` is no longer
consumed. When `APP_ENV=production` and the dialect is PostgreSQL, `DB_SSLMODE=disable` is
rejected.

### Google Cloud SQL

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `DB_INSTANCE_CONNECTION_NAME` | empty | To enable Cloud SQL | Instance name in `project:region:instance` form. |
| `DB_USER` | empty | Yes | Database or IAM database user. |
| `DB_PASSWORD` | empty | Conditional | Required when IAM DB authentication is off and always required for SQL Server. |
| `DB_NAME` | empty | Yes | Database name inside the instance. |
| `DB_IAM_AUTH_ENABLED` | `false` | No | Enables IAM DB authentication for PostgreSQL/MySQL. It is not used for SQL Server. |
| `DB_PRIVATE_IP_ENABLED` | `true` | No | Routes connector traffic through the instance's private IP. |
| `DB_GOOGLE_CREDENTIALS_SOURCE` | `workload-identity` | No | `workload-identity`, `adc`, or `path:/absolute/key.json`. |

`workload-identity`, `adc`, and an empty credential source all use Application Default
Credentials. Prefer workload identity or the runtime service account in production. Reserve
`path:` credentials for exceptional local use and mount the file outside the image.

### Connection pool

| Variable | Default | Constraint | Purpose |
| --- | --- | --- | --- |
| `DB_POOL_MAX_OPEN` | `25` | At least `1` | Maximum open connections. |
| `DB_POOL_MAX_IDLE` | `10` | `0` through `DB_POOL_MAX_OPEN` | Maximum idle connections. |
| `DB_POOL_MAX_LIFETIME` | `30m` | Go duration | Maximum connection lifetime. |
| `DB_POOL_MAX_IDLE_TIME` | `10m` | Go duration | Maximum idle lifetime. |
| `DB_POOL_MAX_LIFETIME_SECONDS` | unset | Positive integer | Legacy fallback used only when `DB_POOL_MAX_LIFETIME` is absent or invalid. |
| `DB_POOL_MAX_IDLETIME_SECONDS` | unset | Positive integer | Legacy fallback used only when `DB_POOL_MAX_IDLE_TIME` is absent or invalid. |

The duration-form variables take precedence over their legacy seconds equivalents. See
[database.md](../backend/database.md) for dialect, IAM, networking, and pool guidance.

## Redis

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `REDIS_DRIVER` | `redis` | No | Cache engine: `redis` or `valkey`. Both use the Redis wire protocol via go-redis. |
| `REDIS_HOST` | `127.0.0.1` | Non-empty | Hostname or IP address of the Redis/Valkey server. |
| `REDIS_PORT` | `6379` | No | Port, from `1` through `65535`. |
| `REDIS_PASSWORD` | empty | No | Password. Keep it in a secret store outside local development. |
| `REDIS_DB` | `0` | No | Logical database number; must be zero or greater. |

Example with authentication against Valkey:

```dotenv
REDIS_DRIVER=valkey
REDIS_HOST=cache.example.com
REDIS_PORT=6379
REDIS_PASSWORD=secret
REDIS_DB=0
```

The application builds an internal `redis://` connection URL from these fields (password
escaping and IPv6 host formatting included). `REDIS_URL` is no longer consumed.

Redis/Valkey is ephemeral infrastructure, not a source of truth. Restrict network access and
use authentication outside local development.

## Messaging

Leave `MESSAGE_BROKER` empty to disable application messaging. `app worker` requires a
broker; `app serve` can run without one.

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `MESSAGE_BROKER` | empty | For worker/messaging | `kafka`, `rabbitmq`, or `googlepubsub`. Aliases `amqp`, `rabbit`, `gcp-pubsub`, and `pubsub` are accepted. |
| `MESSAGE_TOPIC_PREFIX` | `blog.` | No | Prefix applied to every topic/exchange name. Use an environment-specific value to avoid collisions. |
| `KAFKA_BROKERS` | empty | For Kafka | Comma-separated bootstrap servers. |
| `KAFKA_CONSUMER_GROUP` | `blog-api` | For Kafka | Consumer group name. |
| `RABBITMQ_URL` | empty | For RabbitMQ | AMQP connection URL. |
| `GOOGLE_PUBSUB_PROJECT_ID` | empty | For Pub/Sub | Google Cloud project containing topics and subscriptions. |
| `GOOGLE_PUBSUB_CREDENTIALS_SOURCE` | `workload-identity` | No | `workload-identity`, `adc`, or `path:/absolute/key.json`. |

Examples:

```dotenv
# Kafka
MESSAGE_BROKER=kafka
KAFKA_BROKERS=kafka-1:9092,kafka-2:9092
KAFKA_CONSUMER_GROUP=blog-api-production
MESSAGE_TOPIC_PREFIX=production.blog.

# RabbitMQ
MESSAGE_BROKER=rabbitmq
RABBITMQ_URL=amqps://blog:secret@rabbitmq.example.com:5671/blog
MESSAGE_TOPIC_PREFIX=production.blog.

# Google Cloud Pub/Sub
MESSAGE_BROKER=googlepubsub
GOOGLE_PUBSUB_PROJECT_ID=my-production-project
GOOGLE_PUBSUB_CREDENTIALS_SOURCE=workload-identity
MESSAGE_TOPIC_PREFIX=production.blog.
```

Local Kafka and RabbitMQ instructions are in [local.md](./local.md). Broker architecture and
delivery semantics are in [events.md](../backend/events.md).

## Media storage

`config.Load` consumes the media variables below. `S3_*` configure the storage backend, and
`MEDIA_*` define the upload policy used by the future media adapter.

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `S3_ENDPOINT` | `http://127.0.0.1:9000` | No | S3-compatible API endpoint. Local compose uses RustFS. |
| `S3_REGION` | `auto` | No | S3 region hint for the SDK. |
| `S3_BUCKET` | empty | To enable media | Bucket name for uploads and object reads. |
| `S3_ACCESS_KEY` | empty | To enable media | Storage access key. |
| `S3_SECRET_KEY` | empty | To enable media | Storage secret key. |
| `S3_PUBLIC_BASE_URL` | empty | No | Optional public base URL for generated object links. |
| `S3_DISK` | `minio` | No | Storage target selector: `minio`, `s3`, `r2`, or `do_spaces`. |
| `S3_FORCE_PATH_STYLE` | `true` | No | Use path-style addressing for MinIO-compatible endpoints. |
| `MEDIA_ALLOWED_MIME_TYPES` | `image/jpeg,image/png,image/webp,image/gif` | No | Comma-separated MIME allowlist for uploads. |
| `MEDIA_MAX_UPLOAD_BYTES` | `10485760` | No | Maximum declared upload size in bytes. |
| `MEDIA_PRESIGN_TTL` | `15m` | No | Presigned URL lifetime. |

Media is enabled only when `S3_BUCKET`, `S3_ACCESS_KEY`, and `S3_SECRET_KEY` are all set.
In that mode, `S3_DISK` must be one of the supported values, the allowlist must not be
empty, and the size/TTL values must be positive.

## Production validation

`config.Load` fails startup when:

- `APP_ADDR` is empty;
- `APP_ENV=production` and `APP_SESSION_KEY` has fewer than 32 characters;
- production direct database configuration uses the built-in local DSN or includes
  `sslmode=disable`;
- production Cloud SQL configuration omits `DB_INSTANCE_CONNECTION_NAME`, `DB_NAME`, or
  `DB_USER`;
- production direct PostgreSQL sets `DB_SSLMODE=disable`;
- direct-mode `DB_HOST`, `DB_USER`, or `DB_NAME` is empty, or `DB_PORT` is out of range;
- database pool limits are inconsistent;
- the Redis driver, host, port, or database number is invalid;
- a selected message broker is unsupported or lacks its required variables.
- media storage is enabled but `S3_DISK` is unsupported, the MIME allowlist is empty, or
  `MEDIA_MAX_UPLOAD_BYTES` / `MEDIA_PRESIGN_TTL` are not positive.

Before deploying:

```bash
go run ./cmd doctor
```

Then follow [checklist.md](./checklist.md).
