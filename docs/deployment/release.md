# Release Process

## CI gates (current)

Workflow: [.github/workflows/go.yml](../../.github/workflows/go.yml)

1. `go mod download`
2. `go test -count=1 ./...`
3. `go vet ./...`
4. `go build -o app .`

Also run locally before release:

```bash
make lint
committed OpenAPI bundles   # when contracts/paths changed
make routes-check # Go routes ↔ OpenAPI parity
```

## Recommended deploy order

1. Build image with immutable `VERSION` / `COMMIT` tags
2. Apply migrations: `app migrate up` (before traffic needs new schema)
3. Roll out API (`serve`)
4. Roll out workers / scheduler when those commands exist
5. Smoke: health + auth + one public read + one admin read

## Migration policy

- explicit goose SQL under `internal/platform/migrations/sql`
- prefer expand → migrate → contract → remove for breaking schema
- do not ship destructive drops in the same release as unverified code

## Rollback

- roll back application image independently when schema is backward-compatible
- keep audit rows; never delete audit to “fix” a bad deploy
- feature-flag high-risk surfaces when available

## Post-deploy smoke

- `/health/live` and `/health/ready` OK
- Postgres and Redis connectivity
- `POST /api/v1/auth/login` (or admin login) smoke
- `GET /api/v1/posts` public list
- one admin authenticated list (e.g. users) when RBAC is live

Architecture companion: [deployment.md](../architecture/deployment.md).
