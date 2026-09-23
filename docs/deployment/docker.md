# Docker Image

## Build

```bash
docker build \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t blog-api:local .
```

Multi-stage [Dockerfile](../../Dockerfile):

1. `golang:1.26-alpine` — compile `.` (root `main.go`) with `-trimpath` and version ldflags into `cmd`
2. `gcr.io/distroless/static-debian12:nonroot` — copy binary only

## Runtime

| Item | Value |
| --- | --- |
| Entrypoint | `/bin/app` |
| Default CMD | `serve` |
| User | `nonroot` |
| Port | `8080` |
| CGO | disabled |

Pass configuration via environment (see [config.md](./config.md) and
[.env.example](../../.env.example)). Do not bake `.env` into the image.

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
