# Docker Image

## Build

```bash
docker build \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t blog-api:local .
```

Or via Compose / Make:

```bash
make docker-build      # compose build api (production target)
make docker-up         # production api + infra
make docker-dev        # api = Dockerfile `dev` target (air live-reload)
```

Multi-stage [Dockerfile](../../Dockerfile):

1. `dev` — Go toolchain + air; source bind-mounted under Compose (`compose.dev.yaml`)
2. `production` — `golang:1.27-alpine` compiles `.` (root `main.go`) with `-trimpath` and
   version ldflags; runtime is `gcr.io/distroless/static-debian12:nonroot` with binary only

## Runtime (production target)

| Item | Value |
| --- | --- |
| Entrypoint | `/bin/app` |
| Default CMD | `serve` |
| User | `nonroot` |
| Port | `8080` |
| CGO | disabled in the final image |

Pass configuration via environment (see [config.md](./config.md) and
[.env.example](../../.env.example)). Do not bake `.env` into the image.

One-shot CLI against Compose (migrations and seed) must use the production image:

```bash
make docker-migrate    # docker compose run --rm migrate
make docker-seed       # docker compose run --rm --no-deps migrate seed
```

## Example run (API only)

Infra must already be reachable (Compose or managed services):

```bash
docker run --rm -p 8080:8080 --env-file .env blog-api:local serve
```

## Hardening notes

- distroless + nonroot reduces shell/CVE surface
- no package manager in final image
- secrets only via env / secret mounts
- set `APP_TRUSTED_PROXIES` correctly behind a reverse proxy
- set `CORS_ORIGINS` for browser frontends (see [config.md](./config.md#cors))
