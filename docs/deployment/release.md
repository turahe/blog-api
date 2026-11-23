# Release Process

## CI gates (current)

Workflows under [.github/workflows/](../../.github/workflows/):

| Workflow | Role |
| --- | --- |
| [test.yml](../../.github/workflows/test.yml) | `go test -race` with coverage (Postgres + Kafka services) |
| [lint.yml](../../.github/workflows/lint.yml) | `gofmt` / `go vet` / golangci-lint |
| [swagger.yml](../../.github/workflows/swagger.yml) | OpenAPI regeneration / parity |
| [asyncapi.yml](../../.github/workflows/asyncapi.yml) | AsyncAPI validation |
| [security.yml](../../.github/workflows/security.yml) / [govulncheck.yaml](../../.github/workflows/govulncheck.yaml) | Dependency and vuln scans |
| [sonarqube.yml](../../.github/workflows/sonarqube.yml) | SonarQube analysis with coverage |
| [docker.yml](../../.github/workflows/docker.yml) | Image build |
| [release.yml](../../.github/workflows/release.yml) | Release publishing |
| [pr-validation.yml](../../.github/workflows/pr-validation.yml) | PR body RELEASE NOTES + `Type:` label |
| [dependabot-auto-merge.yml](../../.github/workflows/dependabot-auto-merge.yml) | Dependabot PR auto-merge helper |

Also run locally before release:

```bash
make lint
make vuln                    # govulncheck in Docker
make swagger                 # when handler annotations changed
make routes-check            # Go routes ↔ OpenAPI parity
make test                    # or make test-race / make coverage
```

## Recommended deploy order

1. Build image with immutable `VERSION` / `COMMIT` tags
2. Apply migrations: `app migrate up` (before traffic needs new schema)
3. Roll out API (`serve`)
4. Roll out `app worker` and `app scheduler` (separate processes; see [checklist.md](./checklist.md))
5. Smoke: health + auth + one public read + one admin read

## Migration policy

- schema is applied with GORM AutoMigrate (`app migrate up` → `persistence.Migrate`)
- models live under `internal/adapters/outbound/persistence` (`MigrateModels`, plus
  `schema_models_gen.go` for Raw-SQL-era tables); `posts.search_vector` is ensured after
  AutoMigrate via `RebuildPostSearchIndex`
- AutoMigrate does not recreate every historical CHECK/partial index from the old goose
  SQL tree; treat local Compose as greenfield (`make docker-down` + wipe `./.data/postgres18`
  when switching) and verify uniqueness in app code where indexes are incomplete
- prefer expand → migrate → contract → remove for breaking schema changes to models
- `app migrate down` drops AutoMigrate tables (destructive; local/dev only)

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
