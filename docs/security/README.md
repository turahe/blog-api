# Security

Operational security handbook for the blog API. Architecture-level goals live in
[security.md](../architecture/security.md). This folder is the day-to-day checklist for
authn/authz, secrets, HTTP hardening, and change review.

| Doc | Purpose |
| --- | --- |
| [overview.md](./overview.md) | Threat model, trust boundaries, non-negotiables |
| [authn-authz.md](./authn-authz.md) | Auth modes, RBAC, CSRF, step-up, impersonation |
| [secrets-and-headers.md](./secrets-and-headers.md) | Env secrets, headers, logging redaction |
| [checklist.md](./checklist.md) | Pre-merge security checklist for API changes |

Related:

- [api.md](../backend/api.md) — route groups and auth modes
- [rbac-casbin.md](../backend/rbac-casbin.md) — Casbin policy model
- [impersonation.md](../backend/impersonation.md) — impersonation lifecycle
