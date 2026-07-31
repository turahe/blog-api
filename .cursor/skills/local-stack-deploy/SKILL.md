---
name: local-stack-deploy
description: Boots the local blog-api stack with Compose infra, migrations, and API serve. Use when setting up local development, verifying Docker, or debugging health/readiness failures.
---

# Local stack deploy

## Quick path

```bash
cp -n .env.example .env
# set APP_SESSION_KEY, APP_CSRF_KEY, APP_PEPPER to random ≥32-byte values

make infra-up
make migrate-up
make run
```

Verify:

```bash
curl -sS localhost:8080/health/live
curl -sS localhost:8080/health/ready
curl -sS localhost:8080/health/version
```

## Image path

```bash
docker build -t blog-api:local .
docker run --rm -p 8080:8080 --env-file .env blog-api:local serve
```

Infra must be reachable from the container network (adjust `DB_HOST` / `DB_PORT`,
`REDIS_HOST` / `REDIS_PORT`, and `S3_ENDPOINT` for Docker DNS if API runs in Compose later).

## Failure triage

| Symptom | Check |
| --- | --- |
| ready ≠ ok | Postgres/Redis health; `DB_HOST` / `DB_PORT` / `REDIS_HOST` / `REDIS_PORT` |
| migrate fails | `make infra-up`; wait for pg_isready |
| MinIO media fails | ports 9000; `S3_*` in `.env` |
| trusted proxy issues | `APP_TRUSTED_PROXIES` |

## References

- [local.md](../../../docs/deployment/local.md)
- [docker.md](../../../docs/deployment/docker.md)
- [checklist.md](../../../docs/deployment/checklist.md)
