# Contracts

Published API contracts and embedded Swagger UI assets.

| Path | Purpose |
| --- | --- |
| [openapi.yaml](openapi.yaml) | OpenAPI 3.1 entry (split `$ref`s) |
| [openapi.bundle.yaml](openapi.bundle.yaml) | Bundled OpenAPI — embedded and served at `/openapi.yaml` |
| [openapi.bundle.deref.yaml](openapi.bundle.deref.yaml) | Fully dereferenced OpenAPI bundle |
| [asyncapi.yaml](asyncapi.yaml) | AsyncAPI 2.6 events/jobs |
| [swagger/index.html](swagger/index.html) | Swagger UI page |
| [embed.go](embed.go) | `go:embed` for the bundle + UI |

Swagger is mounted by `internal/adapters/inbound/http/swagger` when `APP_SWAGGER_ENABLED=true`.
