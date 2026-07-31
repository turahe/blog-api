---
name: add-http-route-group
description: Adds a new HTTP route group namespace end-to-end (generator constant, middleware chain registry, api.md, tests). Use when introducing a new operationId prefix or when Register fails with an unknown group.
---

# Add HTTP route group

A route group is the first segment of `operationId` (e.g. `admin.users.list` → `admin`).

## Checklist

```text
- [ ] 1. Document group in docs/backend/api.md Route Grouping table
- [ ] 2. Add GROUP_CONSTANTS entry in generate_go_routes.py
- [ ] 3. Add Group const in v1/routes.go
- [ ] 4. Register ByGroup chain in router.go groupMiddleware()
- [ ] 5. make routes + tests
- [ ] 6. Update docs/security/authn-authz.md if auth expectations differ
```

### Generator

In `scripts/contracts/generate_go_routes.py`, add to `GROUP_CONSTANTS`:

```python
"billing": "GroupBilling",
```

### Go types

In `internal/adapters/inbound/http/v1/routes.go`:

```go
GroupBilling Group = "billing"
```

### Chain registry

In `groupMiddleware()` every group key must exist (empty chain is OK until middleware lands):

```go
v1.GroupBilling: nil,
```

Missing keys make `NewRouter` fail — by design.

### Verify

```bash
make routes
go test ./internal/adapters/inbound/http/... -count=1
```

## References

- [api.md](../../../docs/backend/api.md)
- [authn-authz.md](../../../docs/security/authn-authz.md)
