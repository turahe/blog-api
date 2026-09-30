# Local GlitchTip

The Compose stack includes GlitchTip 6 for local Sentry-compatible error tracking.
It is isolated from the blog application's PostgreSQL and Redis services and is enabled
through the `observability` profile.

## Start GlitchTip

```bash
cp .env.example .env
docker compose --profile observability up --build
```

Open [http://127.0.0.1:8000](http://127.0.0.1:8000), create the first account, and create
a project for the Go application. Copy the generated DSN into `.env` as `SENTRY_DSN`.

When the API runs inside Compose, replace the DSN host with `glitchtip:8000` so the
container can resolve the GlitchTip service. For example:

```dotenv
SENTRY_DSN=http://PUBLIC_KEY@glitchtip:8000/PROJECT_ID
SENTRY_ENVIRONMENT=local
```

Restart the API after changing `.env`:

```bash
docker compose --profile observability up -d api
```

The Go service already initializes the Sentry SDK when `SENTRY_DSN` is non-empty. It
captures errors and traces, removes authorization/cookie/token data, and flushes events
on shutdown.

## Services and Data

| Service | Purpose | Local endpoint |
| --- | --- | --- |
| `glitchtip` | GlitchTip web UI and event ingestion | `http://127.0.0.1:8000` |
| `glitchtip-postgres` | GlitchTip event and project database | Internal only |
| `glitchtip-valkey` | Task queue, cache, and sessions | Internal only |

GlitchTip uploads are stored in `.data/glitchtip-uploads`; its database and Valkey data
are stored in their corresponding `.data` directories. These paths are local-development
state and should be backed up or replaced with managed services for production.