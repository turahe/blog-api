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

Every generated route must resolve to a known `Group` and `AuthMode`. Tests in
`internal/adapters/inbound/http/v1` assert:

- registry completeness
- auth chain runs before group chain
- missing group/auth registration fails at `Register` time

After changing [generate_go_routes.py](../../scripts/contracts/generate_go_routes.py):

```bash
make routes
go test ./internal/adapters/inbound/http/...
```

## Contract workflow

1. Update `contracts/openapi.yaml` / `paths/*.yaml` first
2. `make contracts` (Docker Redocly lint + bundle)
3. `make routes` to refresh `routes_gen.go`
4. Add or update HTTP tests for the new `operationId`
5. Implement handler; remove stub only when behavior matches contract

Never invent paths that are absent from OpenAPI. See
[api-contracts.md](../architecture/api-contracts.md).

## File-scoped runs

```bash
go test ./internal/adapters/inbound/http/ -count=1
go test ./internal/adapters/inbound/http/v1/ -run TestEveryGenerated -count=1
go test -run TestHealthLive ./internal/adapters/inbound/http/
```
