# Authentication and Authorization

## Auth modes (contract-derived)

| Mode | Contract form | Runtime |
| --- | --- | --- |
| `required` | inherits document `security: [bearerAuth]` | `401` without valid bearer or session |
| `none` | `security: []` | credentials unused for authorization |
| `optional` | `security: [bearerAuth, []]` | attach identity if present; never `401` for missing token |

Do not attach auth middleware to a shared URL prefix when the prefix hosts mixed modes
(`/api/v1/comments/:id`, `/api/v1/posts/:id/comments`, `/api/v1/admin` login). Grouping rules:
[api.md](../backend/api.md).

## Route groups (security-relevant)

| Group | Expectation |
| --- | --- |
| `auth` | Timing-safe responses; strict rate limits; no user enumeration |
| `self-service` | Effective user = JWT `sub` (or impersonation target); CSRF on mutations; ownership on owned resources |
| `admin` | Bearer + RBAC + CSRF + audit; login is the lone anonymous admin op |
| `public` | Anonymous-safe; privacy filtering; spam/captcha on writes |
| `analytics` | Consent-gated ingest; no privileged data |
| `health` | No auth; keep cheap |

## CSRF

- required for browser cookie sessions on `POST`/`PUT`/`PATCH`/`DELETE`
- exempt only explicitly allowlisted non-browser API tokens
- applies to impersonation start/stop and all `/me/*` mutations

## Step-up

Require recent password re-verify or 2FA for:

- password change, email change, avatar delete
- privacy `public → private`
- activity erase
- admin profile edit, admin password reset
- impersonation start (when 2FA enabled)

## Impersonation

- `superadmin` + `impersonation.*` only
- short TTL; no silent renewal
- effective permissions = target only
- audit every lifecycle event and every subsequent action with impersonator metadata

Details: [impersonation.md](../backend/impersonation.md), [rbac-casbin.md](../backend/rbac-casbin.md).
