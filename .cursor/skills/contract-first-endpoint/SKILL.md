---
name: contract-first-endpoint
description: Adds or changes a REST endpoint by updating OpenAPI first, regenerating Gin routes, then implementing handlers and tests. Use when adding API paths, operationIds, auth modes, or implementing a stubbed contract route.
---

# Contract-first endpoint

## Workflow

Copy and track:

```text
- [ ] 1. Update OpenAPI / paths YAML
- [ ] 2. Validate contracts
- [ ] 3. Regenerate routes
- [ ] 4. Wire handler (or keep 501 stub)
- [ ] 5. Tests + docs
```

### 1. Contract

Edit `contracts/openapi.yaml` and/or `paths/*.yaml`:

- path, method, `operationId` (`namespace.rest...`)
- `security` (`[]`, inherit bearer, or optional `[bearerAuth, []]`)
- request/response schemas, status codes, errors

### 2. Validate

```bash
make contracts
```

### 3. Routes

```bash
make routes
go test ./internal/adapters/inbound/http/v1/ -count=1
```

Confirm `routes_gen.go` shows correct `Group` + `Auth`. If namespace is new, stop and follow the **add-http-route-group** skill.

### 4. Handler

- Map `operationId` in `handlerFor` (or domain-specific register) to a real handler.
- Use envelope helpers; validate allowlisted fields in the adapter.
- Apply CSRF/RBAC via group/auth chains when those middlewares exist.

### 5. Tests and docs

- HTTP test: status + `error.code` / success envelope.
- Update feature docs or `docs/backend/api.md` examples if the surface is user-facing.
- Run: `go test ./internal/adapters/inbound/http/ -count=1`

## Anti-patterns

- Implementing a path missing from OpenAPI
- Hand-editing `routes_gen.go`
- Prefix-level auth on mixed-mode paths

## References

- [api-contracts.md](../../../docs/architecture/api-contracts.md)
- [http-and-contracts.md](../../../docs/testing/http-and-contracts.md)
- [api.md](../../../docs/backend/api.md)
