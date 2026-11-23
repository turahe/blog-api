# Deployment

Runtime and release handbook. Broader environment notes:
[deployment.md](../architecture/deployment.md).

Shared runtime pieces (config projection, logger, crypto, storage setup, HTTP middleware)
come from [`github.com/turahe/pkg`](https://pkg.go.dev/github.com/turahe/pkg); see
[config.md](./config.md).

| Doc | Purpose |
| --- | --- |
| [config.md](./config.md) | Environment variables, defaults, validation, direct DB / Cloud SQL, messaging, CORS, hashing, mTLS |
| [local.md](./local.md) | Compose infra, env, migrate, seed, serve |
| [docker.md](./docker.md) | Image build (dev vs production), distroless runtime, ports |
| [dockerhub-overview.md](./dockerhub-overview.md) | Public Docker Hub description / quick start |
| [glitchtip.md](./glitchtip.md) | Local GlitchTip 6 error tracking with Compose |
| [release.md](./release.md) | CI gates, migrate order, smoke checks |
| [checklist.md](./checklist.md) | Pre-deploy / post-deploy checklist |
| [runbook.md](./runbook.md) | Broker outage, outbox backlog, consumer crash loops, overload |
