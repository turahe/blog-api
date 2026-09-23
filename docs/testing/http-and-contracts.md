# HTTP and Contract Tests

## Router tests

Pattern used today:

1. Build router with `NewRouter(Dependencies{...})`
2. `httptest.NewRequest` + `ServeHTTP`
3. Assert status, security headers, envelope (`ok`, `error.code`, `meta.request_id`)

Cover at least:

- implemented health routes (`/health/live`, ready, version)
- one contract stub → `501` with `operation_id`, `route_group`, `auth_mode`
- `404` / `405` envelopes
- access-log fields for a sample admin route (when logger is injectable)

## Route groups

Every hand-maintained route in `internal/adapters/inbound/routes/` must resolve to a known `Group` and `AuthMode`.
Tests in `routes` assert auth runs before handlers and stubs return 501.

After editing Go routes or OpenAPI:

```bash
make routes-check   # routes.Register* smoke tests
go test ./internal/adapters/inbound/http/...
```

## Contract workflow

1. Add or change the route in [routes/](../../internal/adapters/inbound/routes/) (and handler wiring)
2. Update `contracts/openapi.yaml` / `paths/*.yaml` to match (schemas, security, status codes)
3. Refresh committed bundles (`openapi.bundle.yaml`, `openapi.bundle.deref.yaml`)
4. `make routes-check` + HTTP tests for the new `operationId`
5. Implement handler; remove stub only when behavior matches the contract

OpenAPI is the published contract standard for clients; the Gin table is what the server mounts.
See [api-contracts.md](../architecture/api-contracts.md).

## File-scoped runs

```bash
go test ./internal/adapters/inbound/http/ -count=1
go test ./internal/adapters/inbound/routes/ -run TestEveryRoute -count=1
go test -run TestHealthLive ./internal/adapters/inbound/http/
```
