# Testing Strategy

## Layers

| Layer | Location | Focus |
| --- | --- | --- |
| Unit | `internal/core/**/service`, `domain` | Pure rules, no Gin/GORM/Redis |
| Adapter unit | `internal/adapters/inbound/http` | Middleware, envelopes, route registration |
| Integration | `internal/adapters/outbound/persistence` (repository tests) | Postgres via Compose; the Redis adapter uses miniredis |
| Contract | CI + committed `docs/` (swag) | Swagger / AsyncAPI validity |
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

## Repository tests against Postgres

Persistence tests run against a real database when `TEST_DATABASE_URL` is set and skip
otherwise, so `make test` stays hermetic. `make test-integration` recreates a scratch
database (`TEST_DB`) in the Compose Postgres, migrates it, and runs the persistence package
against it:

```bash
docker compose up -d postgres
make test-integration
```

Migrations run once per test binary, and every test works inside a transaction that is
rolled back at the end, so the suite can re-run against the same database.

## What to mock

| Dependency | Mock? |
| --- | --- |
| Domain ports (repos, mailer, clock) | Yes in unit tests |
| Gin engine | No — use `httptest` |
| Postgres / Redis | Real in integration; fake ports in unit |
| Hand-maintained `routes.Routes` | Edit `internal/adapters/inbound/routes/api.go`; keep OpenAPI aligned via `make routes-check` |

## Priority areas

Start with auth, RBAC, privacy, media uploads, CSRF/step-up, SSE isolation.
Full matrix: [testing.md](../backend/testing.md).
