# Impersonation Backend

## Hexagonal Placement

- `internal/core/impersonation/domain`
  - entity: `ImpersonationSession`
  - value objects: `ImpersonationState`, `ImpersonationTargetEligibility`, `ImpersonationContext`
  - domain errors: target ineligible, not superadmin, stepup required, csrf invalid, session already active
- `internal/core/impersonation/ports`
  - inbound: `ImpersonationPort` (start, stop, get current context)
  - outbound: `ImpersonationSessionRepository`, `ImpersonationAuditSink`, `ImpersonationEventPublisher`, `StepupVerifier`
- `internal/core/impersonation/service`
  - `ImpersonationService` enforces role + permissions + eligibility + session management

Adapters:

- inbound HTTP: `internal/adapters/inbound/http/admin/impersonation`
- outbound:
  - GORM repository for sessions
  - Redis-backed short-TTL session lookup
  - Watermill publisher
  - existing audit sink adapter
  - existing 2FA/step-up verification adapter

## Endpoints

All under `/api/v1/admin/impersonation` unless noted otherwise.

### `POST /api/v1/admin/impersonation/start`

Start impersonation. Requires `superadmin` role + `impersonation.start` permission + 2FA step-up where configured.

Headers:

- `Authorization: Bearer <access_token>`
- `Content-Type: application/json`
- `X-CSRF-Token: <token>` required for browser-origin requests
- `X-Request-ID` recommended
- `X-2FA-Stepup-Proof` optional but required when the user has 2FA enabled (implementation-specific proof)

Request body:

```json
{
  "target_user_id": "uuid",
  "document_scope": {
    "document_id": "optional-uuid-or-null",
    "reason": "string-max-255
  }
}
```

Validation rules:

- target_user_id is required, valid UUID
- target user must exist, be active, and not another user (default) eligible (rule
- `document_scope.reason` required for governance/audit (optional but recommended to enforce)
- request size limits for reason, CSRF, 2FA step-up checks
- cannot start if already impersonating another user unless explicitly resetting behavior (recommended: reject 409 with message and exit first

Response: 200

```json
{
  "data": {
    "impersonation_session_id": "uuid",
    "target_user": {
      "id": "...",
      "display_name": "...",
      "email": "..."
    },
    "expires_at": "2026-07-29T12:00:00Z",
    "effective_permissions_scope": "target_user_only"
  },
  "meta": {
    "request_id": "..."
  },
  "error": null
}
```

After a successful response the system issues a new impersonation-scoped context for subsequent requests (token, cookie, or refreshable actor chain in internal claim set). The exact mechanism is implementation-dependent but must ensure that authorization middleware resolves effective user = target user for subsequent requests.

### `POST /api/v1/admin/impersonation/stop`

Explicitly exit impersonation state. Requires only requires the user already be impersonating.

Headers:

- `Authorization: Bearer <access_token>`
- `X-CSRF-Token: <token>` required browser clients
- `X-Request-ID

Request body (optional):

```json
{
  "impersonation_session_id": "optional uuid"
}
```

Response: 200

```json
{
  "data": {
    "restored_identity": {
      "user_id": "superadmin uuid"
    },
    "status": "exited"
  },
  "meta": {"request_id": "..."},
  "error": null
}
```

### `GET /api/v1/admin/impersonation/current`

Return current impersonation context when caller is currently impersonating; otherwise a 204 or explicit 200 with null state. Requires auth + permissions

Response:

```json
{
  "data": {
    "active": true,
    "impersonation_session_id": "uuid",
    "impersonator_user_id": "uuid",
    "target_user_id": "uuid",
    "document_scope": {},
    "started_at": "...",
    "expires_at": "..."
  },
  "meta": {"request_id": "..."},
  "error": null
}
```

### `GET /api/v1/admin/users/:id/eligible-impersonation-targets

Search/...` alias list eligible users for impersonation.

Supports filters: query, role, status. Pagination follows project pagination standards.

## Session Storage

### Recommended table: impersonation_sessions

Fields:

- id (uuid pk)
- impersonator_user_id (indexed)
- impersonator_session_id nullable for stronger binding
- impersonated_user_id indexed
- state active/expired/revoked/exited
- started_at
- expires_at indexed
- exited_at nullable
- revoked_reason nullable enum: manual, expired, policy, parent_session_expired
- document_id nullable
- reason text nullable
- stepup_verified boolean
- csrf_token_hash nullable
- request_id nullable
- ip_address truncated masked per privacy rules
- created_at
- updated_at

Unique/Index:

- primary key id
- composite index (impersonator_user_id, state where state='active' to enforce one active session default)
- (expires_at, state) for cleanup jobs
- (impersonated_user_id, created_at desc)

### Session Rules

- max active impersonation/superadmin at a time (default: 1)
- short ttl default 1h, configurable 15m-2h
- no refresh or renewal automatic exit
- parent superadmin session is also revoked/expired -> auto exit impersonation
- explicit stop/when reading user identity middleware every request

## Authentication/Authz Middleware Stack

Request pipeline for protected endpoints:

1. request id
2. cors
3. rate limit
4. auth (resolve real identity)
5. csrf for state-changing
6. impersonation context resolver
   - validates active impersonation session
   - swaps effective user = impersonated user
   - populates request context: real_identity + impersonation_session_id
7. rbac middleware runs against effective user
8. audit context enrichment (impersonator metadata)
8. business handler

Important: authorization checks always use effective user for permission checks (target), while audit trails always record both real user + impersonator.

## Permission Inheritance Enforcement

- service-level resolution in authorization: effective_user_id=target_user_id
- roles/permissions are reloaded from target user on every request or cached keyed by target user
- superadmin grants are stripped during impersonation unless explicitly restored after exit
- any cached rights

## CSRF & Validation

Hard rules:

- `start/stop require CSRF token for sameSite cookies, cookie-based
- validate:
- validate target_user_id is a valid active
- validate impersonator has role superadmin
- validate impersonator permissions
- validate stepup proof/ttl
- validate document_scope is real (when provided)
- reject payload max size enforcement

Error responses

## Auditing and events see

Audit every attempt (success and failures) to:

- audit_logs and/or impersonation_sessions_history

Minimum audit entries in audit_logs:

- actor_id real impersonator user id)
 action impersonation.start
 resource_type impersonation_session
 resource_id impersonation_session_id
 metadata containing target user id, document scope, reason, result, failure codes, 2fa status
- additionally augment every business action audit row while impersonating:

 impersonator_user_id impersonation_session_id in audit metadataJSON

## Watermill Events

Event catalog:

- blog.impersonation.start_attempted
- blog.impersonation.started
- blog.impersonation.start_failed
- blog.impersonation.exit_attempted
- blog.impersonation.exited_manually
- blog.impersonation.expired
- blog.impersonation.parent_session_revoked
- blog.impersonation.revoked_by_policy
- blog.action_performed_while_impersonating

Payload: always include event id, occurred at, aggregate_id impersonation_session_id when applicable).

## Background Jobs / Workers

- impersonation_session_expiry worker (scheduler + workers: mark expired sessions state)
- impersonation_cleanup (soft/hard retention after retention)
- impersonation_audit_export (optional)Run via:

- `app scheduler` periodic expiry sweeper
- `app worker` event listeners if async hooks

## Testing Plan

### Unit tests

- ImpersonationService
  - start rejects non-superadmin
  - start rejects missing permissions
  - start rejects 2fa stepup when required
  - start rejects suspended/disallowed targets
  - start respects ttl writes session
  - stop correctly closes
  - current returns correct when active and 204/200/null state when inactive

### Integration tests

- endpoints 401/no 403/no role
- endpoints 403 missing impersonation.start permission
- /start 403 missing csrf
- /start success → subsequent endpoints use effective user permissions author/blog.media endpoints; doc access, doc deny become author 403; post publish denied for author; become editor publish allowed; etc)
- /stop success + subsequent req restore real
- expired session triggers auto exit
- parent session logout/revokement session revoked_by_policy
- audit_logs entries include impersonator_user_id session metadata present

### End-to-end tests

- admin UI start impersonate target → enter doc manage → actions audited → exit
- non-superadmin cannot call start impersonate via direct; blocked; ui element absent
- csrf attack attempt rejected
- session expiry exits impersonation
- doc access decisions follow impersonated user's rules exactly zero elevation
