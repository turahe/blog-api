# Agent Instructions

## Package Manager
- Go modules: `go` (1.26.5)

## Commands
| Task | Command |
|------|---------|
| Test all | `make test` |
| Test package | `go test -count=1 ./internal/adapters/inbound/http/` |
| Lint | `make lint` |
| Build | `make build` |
| Route↔contract check | `make routes-check` |
| Infra up/down | `make infra-up` / `make infra-down` |
| Migrate | `make migrate-up` |
| Serve | `make run` |

## Commit Attribution
AI commits MUST include:
```
Co-Authored-By: <agent model name> <noreply@example.com>
```

## Key Conventions
- Hexagonal modular monolith — core must not import Gin/GORM/Redis/Watermill
- Gin `routes.Register*` is source of truth — edit `internal/adapters/inbound/routes/`, then align OpenAPI
- OpenAPI (`contracts/` + `paths/`) is the published contract — keep bundles committed; `make routes-check`
- Route groups/auth modes are declared in each `Register*` bind — see `docs/backend/api.md`
- Relative links only in Markdown — see `docs/guides/relative-link-usage-rules.md`
- Envelope responses: `{ ok, code, data, meta, error }` (lists add Laravel-style `links` + pagination `meta` — see `docs/backend/data-wrapping-and-pagination.md`; packed `code` — see `docs/backend/response-codes.md`)

## Docs Map
| Area | Path |
|------|------|
| Architecture | `docs/architecture/` |
| Security ops | `docs/security/` |
| Testing ops | `docs/testing/` |
| Deployment | `docs/deployment/` |
| API surface | `docs/backend/api.md` |
| Data models | `docs/backend/model.md` |
| Task backlog | `docs/tasks/README.md` |
| AI context | `docs/ai-context.md` |

## Skills
Project skills under `.cursor/skills/` — invoke when matching the task.
