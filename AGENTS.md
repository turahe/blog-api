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

## Cursor Cloud specific instructions
The startup update script already runs `go mod download` and `npm install`. The Go
toolchain, Node, Docker + Compose, and Postgres/Redis/MinIO images are preinstalled.
Standard commands live in the `## Commands` table above and the README §4; only the
non-obvious caveats are captured here.

- The app does **not** autoload `.env`. `config.Load()` reads process env vars only.
  Create it once with `cp .env.example .env`, set `APP_SESSION_KEY` / `APP_CSRF_KEY` /
  `APP_PEPPER` to real values, then export before any `go run ./cmd ...`:
  `set -a && . ./.env && set +a`. `.env` is gitignored. Docker Compose reads `.env`
  automatically, so `make infra-up` does not need the export.
- `make migrate-up`, `go run ./cmd seed`, `make run`, and `go run ./cmd doctor` all
  need Postgres/Redis (and MinIO for media) up first: `make infra-up`. Confirm the
  Docker daemon is running (`docker info`); start it with `sudo dockerd &` if not.
- Keep `MESSAGE_BROKER` empty in `.env` to run the API without Kafka/RabbitMQ (worker
  bus disabled). Only set it + run `make infra-up-messaging` when testing messaging.
- Unit/adapter tests (`make test`) are hermetic (in-memory SQLite) and need no infra.
- `make routes` invokes `python`, which is not on PATH here — only `python3` is. Run
  `python3 scripts/contracts/generate_go_routes.py` directly (then `gofmt -w` the file).
- Pre-existing (not caused by env setup): `make lint` fails on `gofmt` for a few
  committed files, and `make contracts` reports OpenAPI 3.1 `nullable` errors (README
  §4.7 documents these as known/tracked). `go build`, `go vet`, and `make test` pass.
- Seed default admin for smoke tests: `go run ./cmd seed` →
  `admin@example.com` / `ChangeMeNow!123`. Some handlers (e.g. `admin.posts.list`) are
  scaffolded and return `operation.not_implemented`; `admin.posts.create` works and
  forces new posts to `draft` (public GET only serves `published`).
