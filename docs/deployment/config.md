# Project Configuration

The application is configured exclusively through process environment variables.
`internal/platform/config/config.go` is the source of truth for variables consumed by the
Go process. [.env.example](../../.env.example) is the local template and also supplies
variable substitution for [compose.yaml](../../compose.yaml).

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
| `APP_ENV` | `local` | No | Runtime environment. Use `production` to enable production safety checks. |
| `APP_ADDR` | `0.0.0.0:8080` | Non-empty | HTTP listen address for `app serve`. |
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
| `APP_JWT_PRIVATE_KEY` / `APP_JWT_PRIVATE_KEY_PATH` | none | For `serve`, `seed`, `doctor` | P-256 private key PEM for ES256 access-token signing. Path wins when both are set. `worker`, `scheduler`, `migrate`, `outbox`, and `audit` never read it; do not deploy it there. |
| `APP_JWT_PUBLIC_KEY` / `APP_JWT_PUBLIC_KEY_PATH` | none | For `serve`, `seed`, `doctor` | Matching P-256 public key PEM for ES256 verification. Path wins when both are set. |
| `APP_JWT_ISSUER` | `blog-api` | No | JWT issuer claim. |
| `APP_ACCESS_TOKEN_TTL` | `15m` | No | Access-token lifetime. |
| `APP_REFRESH_TOKEN_TTL` | `720h` | No | Refresh-session lifetime for `"remember": true` logins, and the ceiling for all sessions. Other logins get `min(168h, APP_REFRESH_TOKEN_TTL)`. Each rotation renews the session for the lifetime it was issued with. |
| `AUTH_LOGIN_PER_MINUTE` | `10` | No | Per-IP request budget, per bucket, for each anonymous auth endpoint (login, admin login, refresh, password reset, 2FA challenge, OAuth); `0` disables it. |
| `AUTH_LOGIN_MAX_FAILURES` | `5` | No | Failed logins per email (known or unknown) before a lockout; `0` disables lockout. |
| `AUTH_LOGIN_LOCKOUT` | `15m` | No | Lockout length, and the window in which failures are counted. |
| `RBAC_POLICY_RELOAD_INTERVAL` | `30s` | No | How often each instance reloads the Casbin policy from `casbin_rules`; `0` disables the timer. Role writes also publish on Redis channel `rbac:policy:reload`, so other instances reload at once. |
| `APP_ENCRYPTION_KEY` | empty | For 2FA | Base64 32-byte key (`openssl rand -base64 32`). Encrypts TOTP secrets (AES-256-GCM) and keys the HMAC of backup codes and of stored client-IP hashes (comments, guest flags, newsletter consent). Empty disables 2FA enrollment and leaves IP hashes as reversible plain SHA-256; startup logs a warning. Keep it stable: losing or rotating it locks enrolled accounts out (their logins answer `503 auth.2fa.unavailable`), and new IP hashes stop matching old ones, so a guest can flag a comment again. |
| `AUTH_2FA_ISSUER` | `Blog` | No | Issuer label in the `otpauth://` URL shown by authenticator apps. |
| `OAUTH_GOOGLE_CLIENT_ID` / `OAUTH_GOOGLE_CLIENT_SECRET` | empty | For Google sign-in | Google OAuth client. Set both or neither; empty disables the provider (`404 auth.oauth.provider_unknown`). |
| `OAUTH_GITHUB_CLIENT_ID` / `OAUTH_GITHUB_CLIENT_SECRET` | empty | For GitHub sign-in | GitHub OAuth app. Set both or neither. |
| `OAUTH_REDIRECT_URIS` | empty | For OAuth | Comma-separated allowlist of client callback URLs, matched exactly. Each must be an absolute `http(s)` URL without a fragment, and `https` in production. Register the same URLs with the provider. |
| `AUDIT_RETENTION_DAYS` | `395` | No | Days audit rows are kept; `app audit prune` and the hourly `audit-prune` job in `app scheduler` delete older rows. Must be positive. |
| `AUDIT_QUEUE_SIZE` | `1024` | No | Entries buffered for the background audit writer. When full, new entries are dropped and counted in `blog_audit_entries_dropped_total`. Must be positive. |
| `IMPERSONATION_TTL` | `1h` | No | Lifetime of an impersonation session and its token; never renewed. `5m`–`2h`. See [impersonation.md](../backend/impersonation.md). |
| `NEWSLETTER_PROVIDER` | `smtp` | No | Newsletter issue sender: `smtp` (the SMTP settings, from `app worker`) or `custom_http` (signed JSON gateway). See [newsletter.md](../backend/newsletter.md). |
| `NEWSLETTER_HTTP_ENDPOINT` | — | With `custom_http` | Gateway URL for deliveries and contact syncs; must be `https` in production. Credentials and query are never shown by the admin API. |
| `NEWSLETTER_HTTP_SECRET` | — | With `custom_http` | HMAC-SHA256 secret, at least 32 bytes, for gateway requests and the bounce/complaint webhook. Set with `smtp` it enables only the webhook. |
| `NEWSLETTER_SEND_BATCH` | `50` | No | Recipients claimed per dispatch step, `1`–`500`. |
| `SEARCH_LANGUAGE` | `simple` | No | PostgreSQL text search configuration for post search (`simple` does no stemming; `english`, `indonesian`, … stem). Changing it takes effect after `app search reindex` and an `app serve` restart; until then searches keep the indexed language and `serve` logs a warning. See [search.md](../backend/search.md). |

A locked or throttled login answers `429` with `Retry-After`. Lockout is keyed by email, so
a correct password is refused until the lock expires; both limits are stored in Redis and fail
open when Redis is unavailable.

Use independent random values for each secret. Never log or commit production PEMs.
Local development can point at [`configs/dev/`](../../configs/dev/README.md). Rotating the ES256
private key invalidates existing access tokens; rotating `APP_SESSION_KEY` invalidates hashed
refresh/reset tokens. Additional handling rules are in
[secrets-and-headers.md](../security/secrets-and-headers.md).

## Email (SMTP)

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `SMTP_HOST` | empty | No | SMTP host. Empty keeps account notices in the log. Compose sets this to `mailpit`. |
| `SMTP_PORT` | `1025` | No | SMTP port. Mailpit listens on 1025. |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | empty | No | Optional SMTP auth. Mailpit accepts mail without it. |
| `SMTP_FROM` | `Blog <blog@localhost>` | No | From address on outbound mail. |
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
- `blog_db_*` connection-pool stats, plus Go runtime (`go_*`) and process (`process_*`) series

## Database

The connection mode is selected by `DB_INSTANCE_CONNECTION_NAME`:

- Empty: connect directly using split `DB_*` fields (`DB_DRIVER`, `DB_HOST`, `DB_PORT`, …).
- Set: connect through the Google Cloud SQL connector using the Cloud SQL variables below.
  `DB_HOST` / `DB_PORT` / `DB_SSLMODE` are ignored in Cloud SQL mode.

### Direct connection

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `DB_DRIVER` | `postgres` | No | Only `postgres` (aliases `pg`, `postgresql`). MySQL and SQL Server were dropped because the migrations are PostgreSQL-specific. |
| `DB_HOST` | `127.0.0.1` | Non-empty | Database hostname or IP. |
| `DB_PORT` | `5432` | No | Port. |
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
```

The application builds a PostgreSQL DSN from these fields. `DATABASE_URL` is no longer
consumed. When `APP_ENV=production`, `DB_SSLMODE=disable` is rejected.

### Google Cloud SQL

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `DB_INSTANCE_CONNECTION_NAME` | empty | To enable Cloud SQL | Instance name in `project:region:instance` form. |
| `DB_USER` | empty | Yes | Database or IAM database user. |
| `DB_PASSWORD` | empty | Conditional | Required when IAM DB authentication is off. |
| `DB_NAME` | empty | Yes | Database name inside the instance. |
| `DB_IAM_AUTH_ENABLED` | `false` | No | Enables IAM DB authentication. |
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
of running them, so a down SMTP server or database pauses delivery rather than dead-lettering
every message. See the [runbook](./runbook.md#consumer-circuit-breaker-open).

With `MESSAGE_BROKER` and `APP_ENCRYPTION_KEY` both set, the API does not talk to SMTP:
each email is encrypted and stored as a `notification.email.requested` command, and the
worker sends it. The worker then needs the same `APP_ENCRYPTION_KEY` and `SMTP_*`
settings as the API. Without the key, or when the command cannot be stored, the API sends
inline as before.

## Error tracking (Sentry)

| Variable | Default | Required | Purpose |
| --- | --- | --- | --- |
| `SENTRY_DSN` | empty | No | Enables Sentry for `serve`, `worker`, and `doctor`. Empty disables it entirely. |
| `SENTRY_ENVIRONMENT` | `APP_ENV` | No | Environment tag on events and transactions. |
| `SENTRY_TRACES_SAMPLE_RATE` | `0.1` | No | Fraction (`0`–`1`) of requests and consumed messages recorded as performance transactions. |

Every Error-level log record becomes a Sentry event: recovered panics, 5xx responses, and
worker handler failures. Attributes go through the same redaction as the JSON logs, and a
panic is reported once (not again by the resulting 500 access log). HTTP transactions are
named after the route template (e.g. `GET /api/v1/posts/:slug`), never the raw path. The
release is the build version. Default PII collection is off, and the query string,
cookies, request body, and credential-bearing headers are stripped before sending.

## Production validation

`config.Load` fails startup when:

- `APP_ADDR` is empty;
- the JWT keys are missing (`serve`, `seed`, and `doctor` only; background commands use
  `config.LoadBackground`, which skips them);
- `APP_ENV=production` and `APP_SESSION_KEY` has fewer than 32 characters;
- production direct database configuration uses the built-in local DSN or includes
  `sslmode=disable`;
- production Cloud SQL configuration omits `DB_INSTANCE_CONNECTION_NAME`, `DB_NAME`, or
  `DB_USER`;
- production direct PostgreSQL sets `DB_SSLMODE=disable`;
- direct-mode `DB_HOST`, `DB_USER`, or `DB_NAME` is empty, or `DB_PORT` is out of range;
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
- `SENTRY_TRACES_SAMPLE_RATE` is outside `0`–`1`.

Before deploying:

```bash
go run . doctor
```

Then follow [checklist.md](./checklist.md).
