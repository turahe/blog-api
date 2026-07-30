# Security Overview

## Trust boundaries

| Boundary | Inside | Outside |
| --- | --- | --- |
| Public edge | Gin router, CORS allowlist, rate limits | Browsers, bots, third-party clients |
| Auth boundary | JWT / session validation, CSRF for browsers | Unauthenticated callers |
| Admin boundary | RBAC (`settings.*`, `user.*`, `impersonation.*`, …) | Authenticated non-admin users |
| Data plane | Postgres, Redis, object storage, Watermill | App process only via adapters |

## Non-negotiables

- authorize on the server; never trust client roles, user IDs, or media URLs
- never log passwords, tokens, TOTP secrets, backup codes, or decrypted PII
- state-changing browser requests require CSRF; API tokens only when explicitly allowlisted
- high-risk actions (password/email change, privacy tighten, impersonation start, admin profile edit) require step-up
- contract `security` blocks drive auth mode (`required` / `none` / `optional`); see [api.md](../backend/api.md)
- `server_only` settings and infrastructure secrets never leave the API

## Threat classes (priority)

1. Privilege escalation (RBAC gaps, impersonation leaks, IDOR on `/me` and `/admin`)
2. Auth abuse (credential stuffing, reset enumeration, 2FA bypass)
3. Injection / unsafe media (upload type/size, transform params, malware)
4. Data exposure (privacy toggles, audit oversharing, SSE fan-out to wrong user)
5. Availability (unbounded SSE connections, analytics ingest floods)

## Where enforcement lives

| Concern | Layer |
| --- | --- |
| Auth mode + route group chains | `internal/adapters/inbound/http` + generated `v1` routes |
| Password / session / 2FA rules | `internal/core/auth` (when implemented) |
| Permission checks | middleware + service (Casbin); never UI-only |
| Input allowlists | HTTP adapters + validation docs |
| Audit | service layer on every security-sensitive mutation |

Full policy detail: [security.md](../architecture/security.md).
