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

## Server-sent events

The notification stream is tested at three levels, none of which needs a broker:

- `adapters/inbound/realtime/hub_test.go` — unit tests for the hub: routing to the recipient
  only, the per-user limit, drop-on-full counting, and shutdown.
- `handlers/notifications_stream_test.go` — the real handler behind `httptest.NewServer`, read
  with a `bufio.Reader` frame by frame: headers, the `retry` and `stream.opened` handshake,
  `notification.created` for the caller only, `ping` (interval set to one second), `429` on
  the extra stream, cleanup after the client cancels (`require.Eventually` on the hub's
  connection count), and `stream.closed` on shutdown. Read frames with a timeout-free reader
  but keep the ping interval short so a missing frame fails fast.
- `adapters/outbound/notificationbus/bus_test.go` — publish and consume through Watermill's
  in-memory `gochannel`, including a malformed message that must not stop delivery.

Broker-specific broadcast (every API process receives every event) is not in the default
suite; verify it against the compose brokers (`docker compose --profile messaging up`) by
opening two `messaging.OpenBroadcast` buses with different instance names and checking both
receive one published message.

## Priority areas

Start with auth, RBAC, privacy, media uploads, CSRF/step-up, SSE isolation.
Full matrix: [testing.md](../backend/testing.md).
