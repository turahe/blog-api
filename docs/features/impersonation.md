# Impersonation Feature

## Summary

Allow a `superadmin` to securely operate as another user for the purpose of troubleshooting and administering access to documents, content, and workflows. The impersonated session must strictly inherit the target user’s effective permissions, leave a tamper-evident audit trail, be terminable at any time, and expire automatically to prevent prolonged misuse.

Impersonation is an admin governance feature. It is not a general “switch user” convenience, and it never grants more permissions than the target user already holds.

## Scope

In scope:

- restrict impersonation to users whose active roles include `superadmin`
- start impersonation of a selected target user from the admin user management UI
- operate under the target user’s permission set for the duration of the impersonation session
- record rich audit events for every impersonation lifecycle event and every action performed while impersonating
- provide explicit “exit impersonation” controls, with automatic expiry and forced re-auth rules
- protect all impersonation state changes against CSRF and forged requests
- isolate impersonation behavior so regular users experience no behavior change

Out of scope:

- impersonation of other `superadmin` users by default
- privilege escalation during impersonation (always the target’s effective permissions)
- persistent cross-device impersonation beyond the active session/refresh token chain

## User Roles

- `superadmin`
  - allowed to initiate and terminate impersonation
  - still must pass 2FA step-up where configured
- `admin`
  - no default access; must be explicitly promoted to `superadmin`
- `editor`, `author`, `moderator`, `public_reader`
  - no access whatsoever to impersonation APIs or UI

## Permissions

Permission keys (in addition to role check):

- `impersonation.start`
- `impersonation.stop`
- `impersonation.audit.read`
- `impersonation.target_user.read` — ability to list target users eligible for impersonation

Default grants:

- `superadmin` role holds all four permissions
- no other role holds any impersonation permission by default

Server-side rule: **the role-based `superadmin` check is performed first, then the explicit permission checks are applied**. The system treats both failing the role check or failing the permission check as 403.

## UI Triggers

The feature is only visible/usable under these conditions:

- the current viewer is authenticated
- the current viewer has the `superadmin` role (and passes permission checks)
- the viewer is accessing the **admin user management interface**
- the viewer has completed 2FA step-up within a configurable short window (if 2FA is enabled)

User management actions:

- each user row may show an “Impersonate” action (button/link) for superadmins
- an active impersonation state renders a persistent banner:
  - “You are impersonating <display_name> (<email>)”
  - shows impersonation started timestamp and remaining TTL
  - shows “Exit impersonation” action
- user switcher / impersonation entry point is hidden from all non-superadmin users

Rules for UI:

- never surface impersonation actions in the public site
- do not leak the existence of the impersonation feature in error messages for non-superadmin users
- document banners/controls for accessibility (screen-reader labels, high-contrast support)

## Impersonation Lifecycle

1. **Request** — superadmin selects target user from admin user management.
2. **Validate** — server verifies role, permissions, 2FA step-up, target eligibility, CSRF token, and request integrity.
3. **Start** — server creates an impersonation session record, emits audit events, then issues impersonated auth context without changing the superadmin’s base identity.
4. **Operate** — every subsequent request runs with:
   - effective user = target user
   - effective roles/permissions = target user only
   - audit fields always include impersonator_user_id + impersonation_session_id
5. **Exit** — superadmin clicks “Exit impersonation”:
   - server revokes the impersonation session
   - returns the caller to their real superadmin identity
6. **Auto expiry** — impersonation sessions expire after a short TTL (default 1 hour max; configurable lower) and cannot be refreshed without restarting.

## Target User Eligibility

Default rules:

- allowed target users are non-superadmin users in active status
- `superadmin` cannot impersonate another `superadmin` unless a separate “break-glass” config flag is enabled
- disabled/suspended users cannot be impersonated unless explicitly allowed by feature flag (disabled by default)
- system/service users are not eligible targets by default

## Permission Inheritance

During impersonation:

- **effective identity**: impersonated user
- **effective roles**: impersonated user’s roles only
- **effective permissions**: impersonated user’s permissions only
- **admin UI behavior**: the impersonated session sees the same menus/content that the impersonated user would normally see
- **forbidden elevation**: the session must not retain any `superadmin` permissions or shortcuts; permission lookups must re-resolve from the impersonated user on every request
- **doc/content access**: access-control checks, feature flags, and workflow states must use the target user’s identity

## Audit Requirements

Audit logs must record at minimum:

### Impersonation Lifecycle Events

- `impersonation.start_attempted`
- `impersonation.started`
- `impersonation.start_failed` (reason: role/permission/2FA/csrf/ineligible_target/etc.)
- `impersonation.exited_manually`
- `impersonation.expired`
- `impersonation.revoked_by_policy`

### Fields Per Event

- impersonator_user_id
- impersonator_session_id (superadmin’s base session)
- impersonated_user_id
- impersonation_session_id
- impersonation_started_at
- event timestamp
- request_id
- ip_address
- user_agent bucket
- target document_id or document context when present
- result (success / failure)
- failure reason when applicable

### Actions During Impersonation

Every business action performed while impersonated must be auditable as:

- actor_user_id = impersonated_user_id
- impersonator_user_id = superadmin
- impersonation_session_id recorded in the audit metadata
- document_id or resource identifiers when applicable
- action verb and parameters (redacted where needed for secrets)

## Security Controls

- `superadmin` role check + explicit permission checks
- 2FA step-up when the superadmin has 2FA enabled; recent verification within TTL
- strict CSRF protection for start/exit actions
- request validation of target user id, document scope, and session references
- short impersonation session TTL with no silent refresh
- automatic exit if the superadmin’s own session expires or is revoked
- every response header/page banner surfaces impersonation state to clients and APIs
- no stored cookies or tokens that would allow resuming impersonation after explicit exit

## End-to-End Test Expectations

- non-superadmin users get 403/404 on impersonation endpoints
- superadmin without 2FA step-up (when required) is blocked
- start impersonation succeeds only for eligible target users
- during impersonation, permission checks return the target user’s allowed/denied results
- actions performed during impersonation contain impersonator metadata in audit
- exit impersonation restores superadmin identity and revokes impersonation session
- expired sessions terminate impersonation
- CSRF token missing or invalid on start/exit results in 403
- cross-site forged impersonation start requests are rejected
