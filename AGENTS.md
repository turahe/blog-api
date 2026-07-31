# Agent Instructions

## Package Manager
- Go modules: `go` (1.26.5)
- Node (contracts only): `npm`
- Python (route gen): `python` / `python3`

## Commands
| Task | Command |
|------|---------|
| Test all | `make test` |
| Test package | `go test -count=1 ./internal/adapters/inbound/http/` |
| Lint | `make lint` |
| Build | `make build` |
| Contracts | `make contracts` |
| Regenerate routes | `make routes` |
| Infra up/down | `make infra-up` / `make infra-down` |
| Migrate | `make migrate-up` |
| Serve | `make run` |
| Doc links | `node scripts/docs/validate_relative_links.cjs docs contracts paths README.md` |

## Commit Attribution
AI commits MUST include:
```
Co-Authored-By: <agent model name> <noreply@example.com>
```

## Key Conventions
- Hexagonal modular monolith — core must not import Gin/GORM/Redis/Watermill
- OpenAPI is source of truth — update `contracts/` + `paths/` before handlers; then `make routes`
- Never hand-edit `internal/adapters/inbound/http/v1/routes_gen.go`
- Route groups/auth modes come from `operationId` namespace + `security` — see `docs/backend/api.md`
- Relative links only in Markdown — see `docs/guides/relative-link-usage-rules.md`
- Envelope responses: `{ ok, data, meta, error }`

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
