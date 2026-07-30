# Deployment Checklist

## Pre-deploy

- [ ] CI green on the release commit
- [ ] Image tagged with version + git SHA
- [ ] Migrations reviewed; expand/contract plan if breaking
- [ ] Env vars set from [.env.example](../../.env.example) shapes (real secrets in vault)
- [ ] `APP_TRUSTED_PROXIES` matches the edge proxy
- [ ] CORS allowlist updated for this environment (when enabled)
- [ ] Object storage bucket + credentials verified

## Deploy

- [ ] `migrate up` completed successfully
- [ ] API rolled out; old replicas drained
- [ ] Workers restarted if schema-dependent consumers exist

## Post-deploy

- [ ] `/health/live` and `/health/ready` return OK
- [ ] `/health/version` shows expected version
- [ ] Smoke: public posts, auth login, admin gated route
- [ ] Error rate / latency dashboards nominal
- [ ] No unexpected 501 spikes on newly implemented operations

## Rollback triggers

- ready probe failing after settle window
- auth completely broken
- migration failed mid-apply (stop traffic; fix forward or restore)
