# Project Configuration

The application is configured exclusively through process environment variables.
Shared infrastructure comes from [`github.com/turahe/pkg/config`](https://pkg.go.dev/github.com/turahe/pkg/config).
`internal/platform/config.Config` embeds that package's `Configuration`, and `Load` fills
`Server`, `Database`, `Redis`, `Sentry`, `OpenTelemetry`, and `Timezone` from the variables
in the sections below. `internal/platform/config` is the source of truth for every name the
process reads.

A package variable wins when it is set. The historical name in the Alias column is read
only when the package variable is unset. [.env.example](../../.env.example) still uses those
historical names, and it also supplies variable substitution for
[compose.yaml](../../compose.yaml). Settings this API owns (HTTP limits, mail, messaging,
media, and the rest) have no package equivalent and are unchanged.

## Loading configuration

The Cobra root command loads an optional dotenv file in `PersistentPreRunE` before any
subcommand runs `config.Load()`:

- default: load `.env` from the working directory when the file exists
- `--env-file path` — load a specific file (must exist)
- `--env-file=-` — disable file loading (process env only)

Existing process environment variables always win over file values (so Docker/K8s/shell
exports are never overridden). Staging and production should inject variables through the
deployment platform; do not bake `.env` into the image.

```bash
cp .env.example .env
# Edit secrets and the selected database/message-broker settings.

go run . doctor
go run . serve
# equivalent: go run . --env-file .env serve
```

Docker accepts the same file explicitly (container env; the in-process dotenv loader is
usually unnecessary):

```bash
docker run --rm --env-file .env -p 8080:8080 blog-api:local serve
```

## Value formats

- Durations use Go duration syntax: `500ms`, `15s`, `30m`, `24h`.
- Boolean values use values accepted by `strconv.ParseBool`, normally `true` or `false`.
- Lists are comma-separated; surrounding whitespace and empty entries are removed.
- Empty strings use the documented fallback unless a variable is read as a list.
- Invalid duration, boolean, or integer values currently fall back silently. Validate the
  effective configuration with `go run . doctor` before deployment.
- Only the exact value `APP_ENV=production` activates production-only validation.

## Application and HTTP server

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `APP_ENV` | `local` | No | Runtime environment. Use `production` to enable production safety checks. Also the fallback for `SENTRY_ENVIRONMENT` and `OTEL_ENVIRONMENT`. |
| `APP_ADDR` | `0.0.0.0:8080` | Non-empty | HTTP listen address for `app serve`. Wins over `SERVER_PORT`. |
| `SERVER_PORT` | port of `APP_ADDR` | No | When `APP_ADDR` is unset, the process listens on `0.0.0.0:$SERVER_PORT` and that value is `Server.Port`. When `APP_ADDR` is set, it remains the listen address, and a set `SERVER_PORT` is only `Server.Port`. |
| `SERVER_MODE` | `release` when `APP_ENV=production`, `test` when `APP_ENV=test`, otherwise `debug` | No | Embedded server mode: `debug`, `release`, or `test`. |
| `SERVER_TIMEZONE` | `UTC` | No | IANA timezone on the embedded configuration. Database sessions use it when `DATABASE_TIMEZONE` is unset. |
| `APP_SWAGGER_ENABLED` | `true` when `APP_ENV=local`, else `false` | No | Serve Swagger UI at `/swagger` and the OpenAPI document at `/openapi.yaml`. |
| `APP_TRUSTED_PROXIES` | empty | No | Comma-separated proxies trusted by Gin. Leave empty unless the proxy addresses are known. |
| `APP_SHUTDOWN_TIMEOUT` | `15s` | No | Graceful shutdown deadline. |
| `APP_READ_TIMEOUT` | `15s` | No | Maximum request body read time. |
| `APP_READ_HEADER_TIMEOUT` | `5s` | No | Maximum request header read time. |
| `APP_IDLE_TIMEOUT` | `60s` | No | Keep-alive idle timeout. |
| `HTTP_MAX_INFLIGHT` | `0` | No | Concurrent requests one `app serve` process handles before answering `503` `server.overloaded` with `Retry-After: 1`. `0` disables shedding. Health probes and SSE streams are not counted. |

Do not use `0.0.0.0/0`, `*`, or arbitrary client-controlled addresses in
`APP_TRUSTED_PROXIES`. See [docker.md](./docker.md) for reverse-proxy deployment notes.

## Authentication and security

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `APP_SESSION_KEY` | local development fallback | Yes in production | Pepper for opaque refresh/reset token hashes. Production requires at least 32 characters. |
| `APP_CSRF_KEY` | empty | Not currently enforced | Reserved CSRF secret loaded into config; CSRF integration is not wired yet. |
| `APP_PEPPER` | empty | Not currently enforced | Reserved server-side pepper loaded into config; current password hashing does not consume it. |
| `JWT_PRIVATE_KEY` | none | For `serve`, `seed`, `doctor` | P-256 private key for ES256 access tokens: a PEM (inline, with literal `\n` allowed) or a filesystem path. When unset, `APP_JWT_PRIVATE_KEY_PATH` then `APP_JWT_PRIVATE_KEY`. `worker`, `scheduler`, `migrate`, `outbox`, and `audit` never read it; do not deploy it there. |
| `JWT_PUBLIC_KEY` | none | For `serve`, `seed`, `doctor` | Matching P-256 public key PEM or path. When unset, `APP_JWT_PUBLIC_KEY_PATH` then `APP_JWT_PUBLIC_KEY`. |
| `JWT_ISSUER` | `blog-api` | No | JWT issuer claim (`iss`). `APP_JWT_ISSUER` is the alias. |
| `JWT_SIGNING_ALGORITHM` | `ES256` | No | Access tokens are ES256. The embedded `Server.JWTSigningAlgorithm` is `ES256`. |
| `APP_ACCESS_TOKEN_TTL` | `15m` | No | Access-token lifetime. When unset, `SERVER_ACCESS_TOKEN_EXPIRY` (whole hours) is used. The embedded `Server.AccessTokenExpiry` stores that lifetime in whole hours, so a value under one hour stays `0` there while issuance still uses this duration. |
| `APP_REFRESH_TOKEN_TTL` | `720h` | No | Refresh-session lifetime for `"remember": true` logins, and the ceiling for all sessions. Other logins get `min(168h, APP_REFRESH_TOKEN_TTL)`. Each rotation renews the session for the lifetime it was issued with. When unset, `SERVER_REFRESH_TOKEN_EXPIRY` (whole days) is used. |
| `AUTH_LOGIN_PER_MINUTE` | `10` | No | Per-IP request budget, per bucket, for each anonymous auth endpoint (login, admin login, refresh, password reset, 2FA challenge, OAuth); `0` disables it. |
| `AUTH_LOGIN_MAX_FAILURES` | `5` | No | Failed logins per email (known or unknown) before a lockout; `0` disables lockout. |
| `AUTH_LOGIN_LOCKOUT` | `15m` | No | Lockout length, and the window in which failures are counted. |
| `RBAC_POLICY_RELOAD_INTERVAL` | `30s` | No | How often each instance reloads the Casbin policy from `casbin_rules`; `0` disables the timer. Role writes also publish on Redis channel `rbac:policy:reload`, so other instances reload at once. |
| `APP_ENCRYPTION_KEY` | empty | For 2FA | Base64 32-byte key (`openssl rand -base64 32`). Encrypts TOTP secrets (AES-256-GCM) and keys the HMAC of backup codes and of stored client-IP hashes (comments, guest flags, newsletter consent). Empty disables 2FA enrollment and leaves IP hashes as reversible plain SHA-256; startup logs a warning. Also encrypts queued emails and audit batches; required in production when `MESSAGE_BROKER` is set. Keep it stable: losing or rotating it locks enrolled accounts out (their logins answer `503 auth.2fa.unavailable`), and new IP hashes stop matching old ones, so a guest can flag a comment again. |
| `AUTH_2FA_ISSUER` | `Blog` | No | Issuer label in the `otpauth://` URL shown by authenticator apps. |
| `OAUTH_GOOGLE_CLIENT_ID` / `OAUTH_GOOGLE_CLIENT_SECRET` | empty | For Google sign-in | Google OAuth client. Set both or neither; empty disables the provider (`404 auth.oauth.provider_unknown`). |
| `OAUTH_GITHUB_CLIENT_ID` / `OAUTH_GITHUB_CLIENT_SECRET` | empty | For GitHub sign-in | GitHub OAuth app. Set both or neither. |
| `OAUTH_REDIRECT_URIS` | empty | For OAuth | Comma-separated allowlist of client callback URLs, matched exactly. Each must be an absolute `http(s)` URL without a fragment, and `https` in production. Register the same URLs with the provider. |
| `AUDIT_RETENTION_DAYS` | `395` | No | Days audit rows are kept; `app audit prune` and the hourly `audit-prune` job in `app scheduler` delete older rows. Must be positive. |
| `AUDIT_QUEUE_SIZE` | `1024` | No | Entries buffered for the background audit writer, which inserts them or, with a broker and `APP_ENCRYPTION_KEY`, publishes them for `app worker`. When full, new entries are dropped and counted in `blog_audit_entries_dropped_total`. Must be positive. |
| `ANALYTICS_INGEST_PER_MINUTE` | `300` | No | Requests per minute per client IP across the five `/analytics/ingest/*` routes; `0` disables the limit. See [analytics.md](../backend/analytics.md). |
| `ANALYTICS_QUEUE_SIZE` | `10000` | No | Events buffered for the background analytics writer. When full, new events are dropped and counted in `blog_analytics_events_dropped_total`. Must be positive. |
| `ANALYTICS_EXPORT_RETENTION` | `72h` | No | How long an admin analytics export archive stays in `S3_BUCKET` (under `analytics-exports/`, which must not be publicly readable) before `app scheduler` deletes it. Must be positive. See [analytics.md](../backend/analytics.md#export). |
| `ANALYTICS_EXPORT_URL_TTL` | `15m` | No | Lifetime of each presigned analytics export download link; a new link is issued on every `GET /admin/analytics/exports/{id}`. 1s–168h. |
| `ANALYTICS_COUNTRY_HEADER` | empty | No | Header carrying the visitor's ISO country code from the edge proxy (for example `CF-IPCountry`). Read only from peers in `APP_TRUSTED_PROXIES`, which must be set. Empty stores no country. |
| `IMPERSONATION_TTL` | `1h` | No | Lifetime of an impersonation session and its token; never renewed. `5m`–`2h`. See [impersonation.md](../backend/impersonation.md). |
| `NEWSLETTER_PROVIDER` | `MAIL_DRIVER` | No | Newsletter issue sender, run by `app worker`: `smtp` (the SMTP settings), `resend` (`RESEND_API_KEY`), or `custom_http` (signed JSON gateway). Empty follows `MAIL_DRIVER`. See [newsletter.md](../backend/newsletter.md). |
| `NEWSLETTER_HTTP_ENDPOINT` | — | With `custom_http` | Gateway URL for deliveries and contact syncs; must be `https` in production. Credentials and query are never shown by the admin API. |
| `NEWSLETTER_HTTP_SECRET` | — | With `custom_http` | HMAC-SHA256 secret, at least 32 bytes, for gateway requests and the bounce/complaint webhook. Set with `smtp` it enables only the webhook. |
| `NEWSLETTER_SEND_BATCH` | `50` | No | Recipients claimed per dispatch step, `1`–`500`. |
| `SEARCH_LANGUAGE` | `simple` | No | PostgreSQL text search configuration for post search (`simple` does no stemming; `english`, `indonesian`, … stem). Changing it takes effect after `app search reindex` and an `app serve` restart; until then searches keep the indexed language and `serve` logs a warning. See [search.md](../backend/search.md). |

A locked or throttled login answers `429` with `Retry-After`. Lockout is keyed by email, so
a correct password is refused until the lock expires; both limits are stored in Redis and fail
open when Redis is unavailable.

Use independent random values for each secret. Never log or commit production PEMs.
Local development can point `JWT_PRIVATE_KEY` and `JWT_PUBLIC_KEY` at [`configs/dev/`](../../configs/dev/README.md). Rotating the ES256
private key invalidates existing access tokens; rotating `APP_SESSION_KEY` invalidates hashed
refresh/reset tokens. Additional handling rules are in
[secrets-and-headers.md](../security/secrets-and-headers.md).

## Email

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `MAIL_DRIVER` | `smtp` | No | Transport for account, notification, and newsletter confirmation email: `smtp` or `resend`. |
| `MAIL_FROM` | `SMTP_FROM`, else `Blog <blog@localhost>` | No | From address on outbound mail. With `resend` it must be on a domain verified in Resend. |
| `RESEND_API_KEY` | — | With `resend` | Resend API key (`re_…`), sent only to the Resend API. Required when `MAIL_DRIVER` or `NEWSLETTER_PROVIDER` is `resend`. |
| `SMTP_HOST` | empty | No | SMTP host for the `smtp` driver. Empty keeps account notices in the log. Compose sets this to `mailpit`. |
| `SMTP_PORT` | `1025` | No | SMTP port. Mailpit listens on 1025. |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | empty | No | Optional SMTP auth. Mailpit accepts mail without it. |
| `SMTP_FROM` | `Blog <blog@localhost>` | No | Older name for `MAIL_FROM`, read when `MAIL_FROM` is unset. |
| `APP_PUBLIC_URL` | `http://127.0.0.1:8080` | No | Origin printed in password-reset and email-change messages. |

Local Mailpit UI is [http://127.0.0.1:8025](http://127.0.0.1:8025). Messages cover password reset, password change, and email change. The raw token is only in the message body.

## Metrics

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `METRICS_ADDR` | empty | No | Listen address for Prometheus `GET /metrics` (e.g. `0.0.0.0:9090`). Empty disables metrics. In `app worker` the same listener serves the `GET /healthz` and `GET /readyz` probes. |

Metrics are served on their own listener so they never share the public API port; keep that
port off the public load balancer. Series use the `blog_` prefix:

- `blog_http_requests_total{method,route,status}` and `blog_http_request_duration_seconds{method,route}`,
  labelled by route template (`unmatched` for 404s), so login attempt and lockout rates are
  `route="/api/v1/auth/login"` by status
- `blog_http_requests_in_flight`, `blog_build_info{version}`
- `blog_audit_entries_dropped_total`: audit entries lost to a full queue or a failed insert;
  alert when it increases
- `blog_analytics_events_dropped_total`: analytics events lost to a full queue or a failed
  insert; a steady rise means the writer cannot keep up (raise `ANALYTICS_QUEUE_SIZE` or check
  the database)
- `blog_db_*` connection-pool stats, plus Go runtime (`go_*`) and process (`process_*`) series

## Database

Database settings are `github.com/turahe/pkg/config.DatabaseConfiguration`. The package
variable wins when it is set; the alias is the historical name.

The connection mode is selected by `DATABASE_CLOUD_SQL_INSTANCE` (alias
`DB_INSTANCE_CONNECTION_NAME`):

- Empty: connect directly with the `DATABASE_*` fields below.
- Set: connect through the Google Cloud SQL connector. `DATABASE_HOST`, `DATABASE_PORT`,
  and `DATABASE_SSLMODE` are ignored in Cloud SQL mode. The embedded driver is
  `cloudsql-postgres`.

`DATABASE_DRIVER` accepts `postgres` (aliases `pg`, `postgresql`) and `cloudsql-postgres`
(treated as Postgres; Cloud SQL still requires the instance name). MySQL and SQL Server
are not supported; the migrations are PostgreSQL-specific. The default is `postgres`.

### Direct connection

| Variable | Default | Alias | Purpose |
| --- | --- | --- | --- |
| `DATABASE_DRIVER` | `postgres` | `DB_DRIVER` | Postgres driver. |
| `DATABASE_HOST` | `127.0.0.1` | `DB_HOST` | Database hostname or IP. |
| `DATABASE_PORT` | `5432` | `DB_PORT` | Port. |
| `DATABASE_USERNAME` | `blog` | `DB_USER` | Database user. Required non-empty. |
| `DATABASE_PASSWORD` | `blog` | `DB_PASSWORD` | Database password. |
| `DATABASE_DBNAME` | `blog` | `DB_NAME` | Database name. Required non-empty. |
| `DATABASE_SSLMODE` | `false` | `DB_SSLMODE` | Package boolean: `true` is libpq `require`, `false` is `disable`. |
| `DATABASE_TIMEZONE` | `SERVER_TIMEZONE`, else `UTC` | — | IANA timezone of the database session for connections opened through turahe/pkg. |

`DB_SSLMODE` is the libpq `sslmode` string. `github.com/turahe/pkg/database` accepts only
`disable` and `require` (`DATABASE_SSLMODE` maps the same two values). Any other mode,
including `verify-ca` and `verify-full`, is rejected at startup. When `APP_ENV=production`,
`disable` is rejected. `DATABASE_URL` is not read.

Examples:

```dotenv
# PostgreSQL, turahe/pkg names
DATABASE_DRIVER=postgres
DATABASE_HOST=db.example.com
DATABASE_PORT=5432
DATABASE_USERNAME=blog
DATABASE_PASSWORD=secret
DATABASE_DBNAME=blog
DATABASE_SSLMODE=true
DATABASE_TIMEZONE=UTC
```

### Google Cloud SQL

| Variable | Default | Alias | Purpose |
| --- | --- | --- | --- |
| `DATABASE_CLOUD_SQL_INSTANCE` | empty | `DB_INSTANCE_CONNECTION_NAME` | Instance name in `project:region:instance` form. Set to enable Cloud SQL. |
| `DATABASE_USERNAME` | empty | `DB_USER` | Database or IAM database user. |
| `DATABASE_PASSWORD` | empty | `DB_PASSWORD` | Required when IAM DB authentication is off. |
| `DATABASE_DBNAME` | empty | `DB_NAME` | Database name inside the instance. |
| `DB_IAM_AUTH_ENABLED` | `false` | — | IAM DB authentication. Passed as a dial option; turahe/pkg has no env var for it. |
| `DB_PRIVATE_IP_ENABLED` | `true` | — | Routes connector traffic through the instance's private IP. Dial option, not a package env var. |
| `DB_GOOGLE_CREDENTIALS_SOURCE` | `workload-identity` | — | `workload-identity`, `adc`, or `path:/absolute/key.json`. |

`workload-identity`, `adc`, and an empty credential source all use Application Default
Credentials. Prefer workload identity or the runtime service account in production. Reserve
`path:` credentials for exceptional local use and mount the file outside the image.

### Connection pool

Pool settings are the package fields. The alias is accepted when the package variable is unset.
`DB_POOL_MAX_IDLE_TIME` has no package equivalent (the package's lifetime field is minutes only).

| Variable | Default | Alias | Purpose |
| --- | --- | --- | --- |
| `DATABASE_MAX_OPEN_CONNS` | `25` | `DB_POOL_MAX_OPEN` | Maximum open connections. At least `1`. |
| `DATABASE_MAX_IDLE_CONNS` | `10` | `DB_POOL_MAX_IDLE` | Maximum idle connections. `0` through the open limit. |
| `DATABASE_CONN_MAX_LIFETIME` | `30` | `DB_POOL_MAX_LIFETIME` | Maximum connection lifetime in minutes. The alias is a Go duration (`30m`) and is used only when this variable is unset. |
| `DB_POOL_MAX_IDLE_TIME` | `10m` | — | Maximum idle lifetime. |
| `DB_POOL_MAX_LIFETIME_SECONDS` | unset | — | Legacy seconds fallback used only when both lifetime variables above are absent or invalid. |
| `DB_POOL_MAX_IDLETIME_SECONDS` | unset | — | Legacy fallback used only when `DB_POOL_MAX_IDLE_TIME` is absent or invalid. |

See [database.md](../backend/database.md) for dialect, IAM, networking, and pool guidance.

## Redis

Redis settings are `github.com/turahe/pkg/config.RedisConfiguration`. This API always
enables Redis (`Redis.Enabled` is true) and opens it from `REDIS_HOST`, `REDIS_PORT`,
`REDIS_PASSWORD`, and `REDIS_DB`. `REDIS_DRIVER` is specific to this API.

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
escaping and IPv6 host formatting included). `REDIS_URL` is not read.

Redis/Valkey is ephemeral infrastructure, not a source of truth. Restrict network access and
use authentication outside local development.

### Public read cache

Public post, category, tag and user-profile reads are cached in Redis. The contract (keys,
generations, invalidation) is in [services.md](../backend/services.md#public-read-caching).

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `CACHE_ENABLED` | `true` | No | `false` removes the cache everywhere; every read hits the database. |
| `CACHE_BYPASS_HEADER` | `false` | No | `true` lets a request skip the cache with `Cache-Control: no-cache`. Debugging only. |
| `CACHE_TTL_POSTS` | `1m` | No | Public post list and detail. `0` disables the family. |
| `CACHE_TTL_CATEGORIES` | `10m` | No | Public category list and detail. |
| `CACHE_TTL_TAGS` | `10m` | No | Public tag list. |
| `CACHE_TTL_USERS` | `15m` | No | Public user profiles (`GET /api/v1/users/{username}`). |
| `CACHE_TTL_SETTINGS` | `10m` | No | Stored admin settings, read by the settings endpoints and by services. Invalidated on every change. |

## OpenTelemetry

Tracing settings are `github.com/turahe/pkg/config.OpenTelemetryConfiguration` and are
passed to `github.com/turahe/pkg/otelx`. Export starts when `OTEL_TRACES_EXPORTER` is `gcp`
(or `google`, `cloudtrace`), or when the exporter is `otlp` / `otlp_grpc` and an endpoint
is set. An empty exporter is treated as `otlp` and stays off until an endpoint is set.
GORM tracing inside the package stays off for this API's pool; Sentry still records SQL
spans inside a sampled transaction.

| Variable | Default | Purpose |
| --- | --- | --- |
| `OTEL_TRACES_EXPORTER` | empty (`otlp`, disabled without an endpoint) | `otlp`, `otlp_grpc`, or `gcp`. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | `http/protobuf` or `grpc`. `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` overrides it. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | OTLP endpoint (`:4318` HTTP, `:4317` gRPC). |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | empty | Traces endpoint override. |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | `false` verifies TLS. |
| `OTEL_EXPORTER_OTLP_HEADERS` | empty | Comma-separated `key=value` headers. |
| `OTEL_SERVICE_NAME` | `blog-api` | Resource service name. |
| `OTEL_ENVIRONMENT` | `APP_ENV` | Resource environment. |
| `OTEL_SERVICE_VERSION` | build version | Resource version. When set, it overrides the version passed at startup. |
| `OTEL_TRACES_SAMPLER_ARG` | `1.0` | Trace sampler argument, `0`–`1`. |
| `OTEL_SHUTDOWN_TIMEOUT` | `5s` | Tracer shutdown deadline. |
| `OTEL_GORM_ENABLED` | `true` | Stored on the embedded configuration. SQL spans for this API's pool are the Sentry spans described under Sentry. |
| `OTEL_GCP_PROJECT_ID` | `GOOGLE_CLOUD_PROJECT` | Cloud Trace project. |
| `OTEL_GCP_PROPAGATOR` | `false` | Also on for the `gcp` exporter. Propagates `X-Cloud-Trace-Context`. |

## Messaging

Leave `MESSAGE_BROKER` empty to disable application messaging. `app worker` requires a
broker; `app serve` can run without one.

When `MESSAGE_BROKER` is set, `GET /health/ready` adds a `messaging` check that opens a TCP
connection (2s timeout) to the broker: any entry in `KAFKA_BROKERS`, the `RABBITMQ_URL` host
(default port 5672, or 5671 for `amqps`), or `pubsub.googleapis.com:443` (`PUBSUB_EMULATOR_HOST`
when set). It proves reachability only; `app doctor` opens a real publisher and subscriber.

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `MESSAGE_BROKER` | empty | For worker/messaging | `kafka`, `rabbitmq`, or `googlepubsub`. Aliases `amqp`, `rabbit`, `gcp-pubsub`, and `pubsub` are accepted. |
| `MESSAGE_TOPIC_PREFIX` | `blog.` | No | Prefix applied to every topic/exchange name. Use an environment-specific value to avoid collisions. |
| `KAFKA_BROKERS` | empty | For Kafka | Comma-separated bootstrap servers. |
| `KAFKA_CONSUMER_GROUP` | `blog-api` | For Kafka | Consumer group name. |
| `KAFKA_TLS` | `false` | Required with SASL in production | Connect over TLS 1.2+, verifying brokers against the system roots. |
| `KAFKA_TLS_CA_PATH` | empty | No | PEM bundle to verify brokers instead of the system roots (needs `KAFKA_TLS=true`). |
| `KAFKA_SASL_MECHANISM` | empty | No | `PLAIN`, `SCRAM-SHA-256`, or `SCRAM-SHA-512`. Empty connects without authentication. |
| `KAFKA_SASL_USERNAME` / `KAFKA_SASL_PASSWORD` | empty | With `KAFKA_SASL_MECHANISM` | SASL credentials. Store the password as a secret. |
| `RABBITMQ_URL` | empty | For RabbitMQ | AMQP connection URL. |
| `GOOGLE_PUBSUB_PROJECT_ID` | empty | For Pub/Sub | Google Cloud project containing topics and subscriptions. |
| `GOOGLE_PUBSUB_CREDENTIALS_SOURCE` | `workload-identity` | No | `workload-identity`, `adc`, or `path:/absolute/key.json` (service-account key file). |

Examples:

```dotenv
# Kafka
MESSAGE_BROKER=kafka
KAFKA_BROKERS=kafka-1:9092,kafka-2:9092
KAFKA_CONSUMER_GROUP=blog-api-production
KAFKA_TLS=true
KAFKA_SASL_MECHANISM=SCRAM-SHA-512
KAFKA_SASL_USERNAME=blog-api
KAFKA_SASL_PASSWORD=<secret>
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
`MEDIA_*` define the upload policy enforced by the media service.

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
| `MEDIA_PURGE_AFTER` | `720h` | No | How long soft-deleted media keeps its row and object before the `media-orphans` job in `app scheduler` deletes both. `0` keeps deleted media. Must not be negative. |
| `PRIVACY_EXPORT_RETENTION` | `72h` | No | How long a personal data export archive stays in `S3_BUCKET` (under `privacy-exports/`, which must not be publicly readable) before `app scheduler` deletes it. Must be positive. |
| `PRIVACY_EXPORT_URL_TTL` | `15m` | No | Lifetime of each presigned export download link; a new link is issued on every `GET /me/activity/export`. 1s–168h. |
| `AVATAR_MAX_BYTES` | `5242880` | No | Maximum avatar upload size (`POST /api/v1/me/avatar`); the smaller of this and `MEDIA_MAX_UPLOAD_BYTES` applies. Must be positive. |
| `IMGPROXY_URL` | empty | For transforms | Public imgproxy origin (usually behind a CDN). Empty makes `GET /media/{id}/transform` answer `501`. |
| `IMGPROXY_KEY` / `IMGPROXY_SALT` | empty | With `IMGPROXY_URL` | Hex signing key and salt; must equal imgproxy's own `IMGPROXY_KEY` / `IMGPROXY_SALT`. Secrets. |
| `MEDIA_TRANSFORM_WIDTHS` | `64,128,256,320,480,640,768,1024,1280,1536,1920` | No | Allowed `w` values (1–8192). Keeps the number of variants per image bounded. |
| `MEDIA_TRANSFORM_URL_TTL` | `24h` | No | Minimum lifetime of a signed transform URL; URLs expire between one and two TTLs after issue. |

The bucket must already exist; the API never creates it. Avatar upload is available only
when media is enabled.

Media is enabled only when `S3_BUCKET`, `S3_ACCESS_KEY`, and `S3_SECRET_KEY` are all set.
In that mode, `S3_DISK` must be one of the supported values, the allowlist must not be
empty, and the size/TTL values must be positive.

## Comments

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `COMMENTS_GUEST_ENABLED` | `false` | No | Allow comments without a bearer token (`author_name` and `author_email` required). Guest comments always start `pending`. |
| `COMMENTS_REQUIRE_APPROVAL` | `false` | No | Start signed-in users' comments as `pending` instead of `approved`. |
| `COMMENTS_EDIT_WINDOW` | `15m` | No | How long after posting an author may edit their own comment. |
| `COMMENTS_FLAG_THRESHOLD` | `3` | No | Distinct flags that move an approved comment to `flagged` (hidden from readers). |
| `COMMENTS_CREATE_PER_MINUTE` | `6` | No | Comment creates per caller per minute; `0` disables the limit. |
| `COMMENTS_ACTIONS_PER_MINUTE` | `30` | No | Flags and upvote toggles per caller per minute; `0` disables the limit. |
| `TURNSTILE_SECRET_KEY` | empty | No | Cloudflare Turnstile secret. When set, guest comments must include a valid `turnstile_response` (`400 comments.spam.challenge_invalid` otherwise; `503 comments.spam.challenge_unavailable` if Cloudflare cannot be reached). The token and the guest's IP are sent to Cloudflare's siteverify API. Signed-in users and honeypot hits skip the check. |

Rate limits are Redis fixed windows keyed by the authenticated user, or by client IP for
guests (see `APP_TRUSTED_PROXIES`). If Redis is unreachable, requests are let through and a
warning is logged.

## Notification stream (SSE)

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `SSE_PING_INTERVAL` | `15s` | No | How often an idle stream sends `event: ping`, to keep proxies from closing it. Values below `1s` use the default. |
| `SSE_MAX_CONCURRENT_PER_USER` | `3` | No | Open streams per user on one API process; the next one gets `429` with `Retry-After`. |

The stream needs `MESSAGE_BROKER`: each API process publishes new notifications to
`notifications.created` (with `MESSAGE_TOPIC_PREFIX`) and reads them back through a
subscription of its own, so a notice created on one replica reaches streams on every
replica. Without a broker, or when the broker cannot be reached at startup, the stream
returns `503 notifications.stream_unavailable` while the inbox endpoints keep working.

The live analytics stream (`/api/v1/admin/analytics/realtime/stream`) works the same way on
the topic `analytics.live`, shares the `SSE_MAX_CONCURRENT_PER_USER` limit (counted
separately from notification streams), and returns `503 analytics.realtime_unavailable`
without a broker.

## Domain events (outbox)

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `OUTBOX_BATCH_SIZE` | `100` | No | Outbox rows the relay claims per transaction. |
| `OUTBOX_POLL_INTERVAL` | `1s` | No | How long the relay waits when no row is due. |
| `OUTBOX_MAX_ATTEMPTS` | `10` | No | Publishes before a row is parked as failed. Retries back off from 1s, doubling up to 10m. |
| `OUTBOX_RETENTION` | `168h` | No | How long published rows are kept before the relay deletes them. Failed rows are kept. |

When `MESSAGE_BROKER` is set, services append domain events to `outbox_events` in the same
transaction as the change, and `app worker` publishes them (see
[events.md](../backend/events.md#delivery)). Without a broker no events are stored. Keep at
least one worker running while a broker is configured, or the backlog grows; watch
`blog_outbox_lag_seconds`. `app outbox retry` makes parked rows due again.

### Worker consumers

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `CONSUMER_MAX_RETRIES` | `3` | No | In-process retries of a failing handler before the message goes to `blog.dead_letter`. |
| `CONSUMER_RETRY_INTERVAL` | `1s` | No | First retry delay; each retry doubles it. |
| `CONSUMER_RETRY_MAX_INTERVAL` | `30s` | No | Ceiling for the retry delay. |
| `CONSUMER_DEDUPE_RETENTION` | `168h` | No | How long handled message ids are kept to skip redeliveries. Keep it above the broker's redelivery window. |
| `CONSUMER_CONCURRENCY` | `1` | No | Handler copies per consumer in one worker. RabbitMQ copies compete for queue messages; Kafka copies share partitions, so more copies than partitions sit idle. |
| `CONSUMER_BREAKER_FAILURES` | `5` | No | Consecutive transient handler failures (retries included) that open a consumer's circuit breaker. `0` disables it. |
| `CONSUMER_BREAKER_TIMEOUT` | `30s` | No | How long an open breaker refuses messages before one trial message is let through. |

While a breaker is open the consumer nacks messages after a short pause (at most 5s) instead
of running them, so a down mail provider or database pauses delivery rather than dead-lettering
every message. See the [runbook](./runbook.md#consumer-circuit-breaker-open).

A broker moves three kinds of request-side work to the worker, each with an in-process
fallback (details in [events.md](../backend/events.md#email-dispatch)):

| Queue | Needs | Worker consumer | Fallback |
| --- | --- | --- | --- |
| Email | `MESSAGE_BROKER`, `APP_ENCRYPTION_KEY` | `email-dispatch`, with the same key and mail settings (`MAIL_*`, `SMTP_*`, `RESEND_API_KEY`) | The API sends inline when the command cannot be stored |
| In-app notifications | `MESSAGE_BROKER` | `notification-dispatch` | The API delivers inline when the command cannot be stored |
| Audit log | `MESSAGE_BROKER`, `APP_ENCRYPTION_KEY` | `audit-writer`, with the same key | The API inserts the batch when the publish fails |

In production, `MESSAGE_BROKER` without `APP_ENCRYPTION_KEY` is a startup error. Elsewhere the
email and audit queues stay off without the key and the API does that work itself. Audit
batches are published directly rather than through the outbox, so with RabbitMQ or Google
Pub/Sub start `app worker` once before the API gets `MESSAGE_BROKER`, or batches published
before the worker's queue or subscription exists are discarded.

## Error tracking (Sentry)

Sentry settings are `github.com/turahe/pkg/config.SentryConfiguration`. `SENTRY_LOGS_LEVEL`
is specific to this API. The embedded configuration also sets server name `blog-api`,
attaches stack traces, samples errors at `1`, and flushes for `2s`. The release sent with
events is the build version.

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `SENTRY_DSN` | empty | No | Enables Sentry for `serve`, `worker`, `scheduler`, and `doctor`. Empty disables it entirely. |
| `SENTRY_ENVIRONMENT` | `APP_ENV` | No | Environment tag on events and transactions. |
| `SENTRY_TRACES_SAMPLE_RATE` | `0.1` | No | Fraction (`0`–`1`) of requests and consumed messages recorded as performance transactions. |
| `SENTRY_LOGS_LEVEL` | `info` | No | Parsed and validated (`off`, `debug`, `info`, `warn`, `error`). `github.com/turahe/pkg/sentryx` does not forward log records, so this value does not change what Sentry receives. |

`github.com/turahe/pkg/sentryx` initializes the client from `SentryConfiguration`. An empty
`SENTRY_DSN` leaves Sentry off. An invalid DSN is logged and Sentry stays off; startup
continues. The release is the build version. HTTP transactions are named after the route
template (e.g. `GET /api/v1/posts/:slug`), never the raw path.

`sentryx` does not turn log records into Sentry events, does not send Sentry Logs, does not
record GORM statement spans, and does not strip the query string, cookies, body, or
credential headers from events. Do not log secrets.

Each `app scheduler` job reports Sentry cron check-ins to a monitor named after the job
(e.g. `audit-prune`), created on the first check-in with an interval schedule matching the
job, a 2-minute margin, and a timeout of twice the interval (at least 30 minutes). Only the
replica that runs a job checks in, so a missed or timed-out monitor means no replica ran
it. An issue opens after two failed runs in a row, and job error events carry a `job` tag.

## Production validation

`config.Load` fails startup when:

- `APP_ADDR` is empty;
- the JWT keys are missing (`serve`, `seed`, and `doctor` only; background commands use
  `config.LoadBackground`, which skips them) — `JWT_PRIVATE_KEY` / `JWT_PUBLIC_KEY`, or the
  `APP_JWT_*` aliases;
- `APP_ENV=production` and `APP_SESSION_KEY` has fewer than 32 characters;
- production direct database configuration uses `sslmode=disable` (`DATABASE_SSLMODE=false`
  or `DB_SSLMODE=disable`);
- production Cloud SQL configuration omits `DATABASE_CLOUD_SQL_INSTANCE`, `DATABASE_DBNAME`,
  or `DATABASE_USERNAME` (or the `DB_*` aliases);
- direct-mode host, user, or database name is empty, or the port is out of range;
- database pool limits are inconsistent;
- the Redis driver, host, port, or database number is invalid;
- a selected message broker is unsupported or lacks its required variables;
- `KAFKA_SASL_MECHANISM` is unknown or lacks credentials, credentials are set without a
  mechanism, `KAFKA_TLS_CA_PATH` is set without `KAFKA_TLS`, or (in production) SASL is used
  without `KAFKA_TLS=true`;
- media storage is enabled but `S3_DISK` is unsupported, the MIME allowlist is empty, or
  `MEDIA_MAX_UPLOAD_BYTES` / `MEDIA_PRESIGN_TTL` are not positive;
- `MEDIA_PURGE_AFTER` is negative;
- `IMGPROXY_URL` is set without hex `IMGPROXY_KEY` / `IMGPROXY_SALT` (in production at least
  32 and 16 bytes), `MEDIA_TRANSFORM_WIDTHS` has an entry outside 1–8192, or
  `MEDIA_TRANSFORM_URL_TTL` is not positive;
- `SENTRY_TRACES_SAMPLE_RATE` is outside `0`–`1`, or `SENTRY_LOGS_LEVEL` is not `off`,
  `debug`, `info`, `warn`, or `error`.

Before deploying:

```bash
go run . doctor
```

Then follow [checklist.md](./checklist.md).
