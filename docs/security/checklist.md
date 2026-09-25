# Security Change Checklist

Use before merging any auth, RBAC, media, settings, impersonation, or privacy change.

## Contract

- [ ] OpenAPI `security` matches intended auth mode (`required` / `none` / `optional`)
- [ ] OpenAPI tag base name maps to the correct route group
- [ ] Error codes documented; no user enumeration on auth endpoints

## Enforcement

- [ ] Authorization in middleware and/or service — not UI-only
- [ ] Allowlisted patch/body fields; unknown keys → `400`
- [ ] CSRF on browser state-changing routes
- [ ] Step-up on high-risk actions (see [authn-authz.md](./authn-authz.md))
- [ ] Rate limits defined for abuse-prone endpoints

## Data

- [ ] No secrets or `server_only` settings in responses
- [ ] Privacy toggles enforced on public profile reads
- [ ] Media uploads validate type, size, magic bytes; malware scan before finalize
- [ ] Audit log written for security-sensitive mutations (actor + target + request_id)

## Tests

- [ ] Positive and negative authz cases (401 / 403 / 404 privacy)
- [ ] Impersonation: no privilege elevation; audit metadata present
- [ ] Timing-safe auth responses where required (forgot password)

## Ops

- [ ] New env vars added to [.env.example](../../.env.example) without real secrets
- [ ] Docs updated under [docs/security](./README.md) if policy changed

## Run log

### 2026-09-25 — auth surface

Scope: `/api/v1/auth/*`, `/api/v1/admin/auth/login`, `/api/v1/me/password`, `/api/v1/me/2fa*`,
bearer middleware, token service. Threat model: [authn-authz.md](./authn-authz.md#threat-model-authentication).

| Item | Result | Notes |
| --- | --- | --- |
| OpenAPI `security` matches auth mode | Pass, automated | `routes/swagger_parity_test.go` now checks every route (it caught `GET /admin/categories`) |
| Tag maps to route group | Pass | auth operations are tagged `auth`, admin login `admin` |
| No user enumeration | **Fixed** | Login answered "Account is not active" before checking the password, and skipped hashing for unknown emails. Both now pay for a hash and look like a wrong password; forgot-password mail is sent in the background so response time does not reveal the account |
| Authorization in middleware/service | Pass | bearer middleware plus Casbin checks per admin route; admin login requires `admin.access` |
| Unknown body keys → `400` | Open | JSON binding ignores unknown keys. Auth DTOs are explicit structs, so there is no mass assignment; rejecting unknown keys globally is a contract change left for a later decision |
| CSRF on browser state-changing routes | N/A | the API issues no cookies; bearer tokens only |
| Step-up on high-risk actions | Partial | password change and 2FA disable re-check the password (and a code); "recent 2FA" step-up is open (phase 3) |
| Rate limits on abuse-prone endpoints | **Fixed** | refresh (`auth.refresh`) and forgot/reset password (`auth.password`) were unlimited; all anonymous auth endpoints now share `AUTH_LOGIN_PER_MINUTE` per IP |
| No secrets in responses | Pass | only 2FA setup returns a secret, with `Cache-Control: no-store`; refresh and reset tokens are stored as keyed hashes |
| Audit log for sensitive mutations | Open | needs the phase 3 audit writer |
| Positive and negative authz tests | Pass | handler, service and PostgreSQL integration tests cover 401/403, token reuse and expiry |
| Timing-safe forgot password | **Fixed** | see enumeration row |
| Access token validation | **Fixed** | `ParseAccess` now requires `exp`, checks the issuer and pins ES256 through the parser options |
| Env vars in `.env.example` | Pass | includes `APP_ENCRYPTION_KEY`, `AUTH_2FA_ISSUER`, `OAUTH_*` |
