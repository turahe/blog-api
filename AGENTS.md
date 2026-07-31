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

The startup update script only refreshes deps (`go mod download`, `npm install`). Docker,
`.env`, and services are NOT started automatically — start them per this section.

- **Docker daemon must be started manually** (no systemd in this VM). Docker/Compose are
  pre-installed in the snapshot. Start once per session in a tmux session, e.g.
  `sudo dockerd &` (config already set: fuse-overlayfs storage driver + containerd-snapshotter
  disabled in `/etc/docker/daemon.json`, iptables-legacy). Then `make infra-up`.
- **The Go app does NOT auto-load `.env`.** Before `migrate`/`seed`/`serve`/`doctor` (and
  before `make infra-up`, since `compose.yaml` interpolates `${DB_*}`/`${S3_*}`), run
  `set -a; . ./.env; set +a`. This is the most common cause of "connection refused" / empty
  compose vars.
- **`.env` is required and gitignored.** If missing, `cp .env.example .env` and set
  `APP_SESSION_KEY`, `APP_CSRF_KEY` (≥32 chars each), `APP_PEPPER` to random values
  (e.g. `openssl rand -hex 32`). Keep `MESSAGE_BROKER=` **empty** unless testing the worker —
  a non-empty value makes `app doctor`/`app worker` try to reach RabbitMQ.
- **Required services for `serve`:** PostgreSQL + Redis only (bootstrap pings both and fails
  hard if unreachable). MinIO (media) and message brokers are optional; brokers are only used
  by `app worker` (Compose `messaging` profile).
- **Local boot order:** start dockerd → `make infra-up` (wait for `docker compose ps` healthy)
  → `make migrate-up` → `go run ./cmd seed` → `make run` (API on `:8080`). See
  `.cursor/skills/local-stack-deploy/SKILL.md` and `docs/deployment/local.md`.
- **Seeded admin login:** `admin@example.com` / `ChangeMeNow!123`. `POST /api/v1/auth/login`
  returns a Bearer `access_token`; admin write endpoints (`/api/v1/admin/...`) need it.
- **`make contracts` currently fails** with pre-existing OpenAPI 3.1 `nullable: true` spec
  violations (tracked separately, see README §4.7); this is a spec-authoring issue, not an
  environment problem. Redocly tooling itself works.
- **Don't run `go mod download all`** — it pulls the full transitive Google Cloud graph into
  `go.sum`. Plain `go mod download` is enough to build/test.
- **Ports:** API `8080`, Postgres `5432`, Redis `6379`, MinIO `9000` (console `9001`).
