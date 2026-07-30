# Testing Strategy

## Layers

| Layer | Location | Focus |
| --- | --- | --- |
| Unit | `internal/core/**/service`, `domain` | Pure rules, no Gin/GORM/Redis |
| Adapter unit | `internal/adapters/inbound/http` | Middleware, envelopes, route registration |
| Integration | `*_integration_test.go` (when added) | Postgres, Redis, MinIO via Compose |
| Contract | CI + `make contracts` | OpenAPI / AsyncAPI validity |
| Smoke | post-deploy | Health, login, one admin + one public path |

## Package rules

- domain and service tests must not import `gin`, GORM, or Redis clients
- HTTP tests use `httptest` + real `NewRouter` where possible
- prefer table-driven cases for status codes and error `code` values
- keep tests deterministic: no wall-clock sleeps unless testing timeouts; seed time via clocks/ports

## Naming

```text
*_test.go           # unit / adapter
*_integration_test.go  # needs external services (build tag optional)
```

## What to mock

| Dependency | Mock? |
| --- | --- |
| Domain ports (repos, mailer, clock) | Yes in unit tests |
| Gin engine | No — use `httptest` |
| Postgres / Redis | Real in integration; fake ports in unit |
| Generated `v1.Routes` | Never hand-edit; regenerate via `make routes` |

## Priority areas

Start with auth, RBAC, privacy, media uploads, CSRF/step-up, SSE isolation.
Full matrix: [testing.md](../backend/testing.md).
