# Security

## Security Goals

- protect user accounts
- prevent privilege escalation
- avoid sensitive data exposure
- keep actions auditable

## Authentication Rules

- all admin and self-service profile endpoints require authentication
- users may access only their own profile unless an admin permission allows otherwise
- high-risk actions require recent 2FA verification
- use secure, http-only cookies or an equivalent server-managed session design

## Password and Secret Handling

- hash passwords with Argon2id or bcrypt
- encrypt TOTP secrets at rest
- hash backup codes before storage
- never log passwords, tokens, secrets, or backup codes

## RBAC

- enforce authorization on the server
- support users, roles, permissions, user-role mappings, and role-permission mappings
- keep self-service permissions separate from admin management permissions
- protect admin settings endpoints with explicit `settings.read`, `settings.update`, and `settings.history.read` permissions
- require 2FA step-up where available for security-sensitive setting updates
- strictly gate impersonation to the verified `superadmin` role + explicit `impersonation.*` permissions
- impersonation sessions must inherit the target user’s permissions only and never retain superadmin grants

## Impersonation Security

- impersonation start/stop requests require CSRF protection for browser clients
- impersonation start must require 2FA step-up for superadmins with 2FA enabled
- impersonation sessions must have a short TTL and auto-expire; no silent renewal
- every action performed while impersonating must include the impersonator identity in audit metadata
- impersonation of other superadmins is blocked by default
- a superadmin’s parent session revocation/expiry must immediately terminate the impersonation session

## Social Login

- validate OAuth `state` and OIDC `nonce`
- verify issuer, audience, and callback expectations
- do not auto-link accounts without an ownership check
- store only required provider data

## API Safety

- validate and sanitize every request
- use allowlists for editable fields
- avoid revealing sensitive existence checks in error messages
- add rate limiting to login, 2FA, recovery, and other sensitive endpoints
- default rate limit: 100 requests per minute per client or equivalent scoped identity
- enforce a CORS whitelist for allowed origins
- protect state-changing endpoints against CSRF
- validate media upload size, type, and transform parameters
- scan uploaded files for malware before finalizing storage
- reject malformed or corrupted images safely during transform requests
- analytics ingestion endpoints must be rate-limited and consent-gated
- never trust client-supplied timestamps for analytics without server-side occurred-at clamping
- admin analytics export and erasure actions require strong auth and audit trails
- admin settings `PUT` must use allowlisted keys, strict type validation, version checks, and tighter per-user rate limiting
- settings endpoints must never return `server_only` values or infrastructure secrets

## Audit Requirements

Audit at minimum:

- login success and failure
- password changes
- 2FA enable, disable, challenge failure, recovery
- social account link and unlink
- role and permission changes
- media upload, delete, and privileged access events
- analytics consent grant, reject, withdraw, and erasure requests
- analytics dashboard export and bulk download access
- analytics retention policy changes
- settings reads (if sensitive category) and all settings update attempts
- settings history or export access
- impersonation start attempts, start success, exit, expiry, and revocation
- all actions performed while impersonating, with impersonator_user_id + impersonation_session_id

## Secure Defaults

- deny by default
- least privilege for users and services
- secrets via environment variables only
- regular dependency review and patching

## Operational handbook

Day-to-day checklists and auth-mode tables: [docs/security](../security/README.md).
