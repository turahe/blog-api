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

## Threat model: authentication

Covers password login, admin login, refresh sessions, logout, password reset, TOTP 2FA and
Google/GitHub OAuth, as implemented in `internal/core/auth` and its adapters. Last reviewed
2026-09-25 alongside the [checklist run](./checklist.md#2026-09-25--auth-surface).

### Assets

| Asset | Where it lives | Protection |
| --- | --- | --- |
| Passwords | `users.password_hash` | Argon2id (t=3, m=64 MiB, p=2); never logged |
| Access tokens | client only | ES256 JWT, `APP_ACCESS_TOKEN_TTL` (15 min); issuer, `exp` and algorithm checked |
| Refresh tokens | client; `refresh_sessions.token_hash` | 32 random bytes, stored as a keyed hash (`APP_SESSION_KEY`); rotated on every use |
| Reset and email-change tokens | email; `password_reset_tokens` | keyed hash, 1-hour TTL, single use, older pending tokens revoked |
| TOTP secrets, backup codes | `user_two_factor_*` | AES-256-GCM and HMAC under `APP_ENCRYPTION_KEY` |
| 2FA challenges, OAuth state | Redis | stored under a SHA-256 of the token; short TTL; single use |
| JWT signing key, session key, encryption key | env or mounted files | never committed; see [secrets-and-headers.md](./secrets-and-headers.md) |

### Trust boundaries

1. **Client ↔ API.** Everything in a request is untrusted. The client IP (used for rate limits)
   is only as trustworthy as `APP_TRUSTED_PROXIES`.
2. **API ↔ PostgreSQL and Redis.** Trusted, network-restricted. Redis loss makes rate limits
   and lockout fail open, but challenges and OAuth state fail closed.
3. **API ↔ OAuth providers.** The provider is trusted to authenticate its users and to report
   whether an email is verified. Nothing else it returns is trusted.
4. **API ↔ mail server.** Reset links cross this boundary; mailbox compromise equals account
   compromise, which is inherent to email-based recovery.

### Threats and mitigations

| Threat | Mitigation | Residual risk / open |
| --- | --- | --- |
| Online password guessing | Per-IP limit on every anonymous auth endpoint (`AUTH_LOGIN_PER_MINUTE`); per-email lockout after `AUTH_LOGIN_MAX_FAILURES` for `AUTH_LOGIN_LOCKOUT` | Both fail open when Redis is down. A distributed attack stays under per-IP limits; lockout then applies per email, which also lets an attacker lock a known email out for the lockout window |
| Account enumeration | Unknown and inactive accounts look like a wrong password (same code, a hash is always computed); forgot-password always answers `200` and mails in the background; admin login denies non-staff with the same `401` | Account status is revealed to a caller holding the correct password. OAuth `no_account` is visible only to someone controlling a verified provider identity for that email |
| Stolen access token | 15-minute lifetime; ES256 signature, issuer and `exp` required | Not revocable: logout, suspension and password change take effect when the token expires |
| Stolen refresh token | Rotation on every refresh; reuse of a rotated token revokes the whole family; sessions expire (7 days, or `APP_REFRESH_TOKEN_TTL` with remember-me) and rotation never extends that class; password reset and change revoke sessions | A thief who refreshes first holds the session until the victim's next refresh triggers reuse detection |
| Token forgery / algorithm confusion | Parser accepts ES256 only, with a P-256 key checked at startup | Key compromise requires rotating `APP_JWT_*` (all access tokens die) |
| Refresh / reset token theft from the database | Only keyed hashes are stored | Anyone with the database and `APP_SESSION_KEY` can check guesses; tokens are 256-bit random |
| Password reset abuse | Rate limited; single-use, 1-hour tokens; a new request revokes older tokens and a completed reset spends every pending one; all sessions are revoked on reset; the raw token never reaches the access log (routes log templates) | Mail flooding a victim is bounded by the per-IP limit only. Two simultaneous resets with the same token can both succeed (the used check is not atomic); both callers hold the token |
| 2FA bypass or brute force | Challenge token (5 min, single use, 5 attempts, per-IP limit); each TOTP step accepted once; backup codes single use; enrolled accounts fail closed without `APP_ENCRYPTION_KEY` | Losing `APP_ENCRYPTION_KEY` locks enrolled users out; there is no "recent 2FA" step-up yet |
| OAuth login CSRF / code injection | Server-side state bound to provider and redirect URI (10 min, single use); PKCE S256; exact redirect-URI allowlist | Relies on the client keeping the state it was given; the API cannot bind the state to a browser without cookies |
| OAuth account takeover | No account creation; linking only through a provider-verified email or an existing link; one identity per user per provider | A provider that wrongly marks an email as verified, or a recycled provider email, could sign in to the matching account |
| Privilege escalation through admin login | `admin.access` checked after the password; every admin route checks its own Casbin permission | Admin tokens are ordinary tokens; there is no separate admin session or step-up |
| CSRF | Not applicable: the API sets no cookies and requires bearer tokens | Revisit if cookie sessions are added (see the CSRF section above) |
| Secrets in logs | Access log records route templates, not raw paths; tokens, passwords and TOTP data are never logged | Upstream proxies may log raw URLs with reset tokens; configure them accordingly |
| Undetected takeover or repudiated admin actions | Audit log of sign-ins (including rejected attempts on an existing account), resets, 2FA and OAuth changes, and every admin mutation; owners see their own activity at `/me/activity` | Entries are dropped (and counted) when the queue is full or the database is down; audit rows are kept for `AUDIT_RETENTION_DAYS` only |

### Open items

- "Recent 2FA" step-up for high-risk actions.
- Rejecting unknown JSON keys (a contract decision).
- Optional access-token revocation (for example a per-user "tokens valid after" timestamp) if
  a 15-minute window after suspension is too long.
