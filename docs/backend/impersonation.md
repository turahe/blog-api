# Impersonation Backend

Staff with `impersonation.start` can act as another user to reproduce a problem. Starting a
session returns a separate, short-lived access token whose subject is the target and whose RFC 8693
`act` claim names the staff member. Every request with that token runs as the target, is checked
against the live session, and is audited with both identities.

Product spec: [impersonation.md](../features/impersonation.md).

## Placement

| Layer | Package | Role |
| --- | --- | --- |
| Domain | `internal/core/impersonation/domain` | `Session`, states and end reasons, `Grants.CanImpersonate` (eligibility), errors |
| Ports | `internal/core/impersonation/ports` | `Repository`, `Users`, `Permissions`, `StepUp`, `Tokens` |
| Service | `internal/core/impersonation/service` | `Start`, `Verify`, `Stop`, `Current`, `ExpireStale`; records lifecycle events |
| Persistence | `persistence.ImpersonationRepository` | `impersonation_sessions` (migration 00029) |
| Permissions | `rbac.Enforcer` + `rbac.RoleStore.Grants` | live permission checks; roles and permissions for eligibility |
| Step-up | `AuthService.VerifyStepUp` | current password, plus TOTP or backup code when 2FA is enabled |
| Tokens | `AuthService.IssueAccess` → `jwt.Service` | ES256 access token with `act` and `sid` |
| HTTP | `handlers/impersonation.go`, `middleware/auth.go`, `middleware/impersonation.go` | endpoints, per-request session check, blocked operations |
| Scheduler | `impersonation-expire` job | closes sessions past `expires_at` |

## Token

| Claim | Value |
| --- | --- |
| `sub` | Target user uuid |
| `act.sub` | Staff member (impersonator) uuid |
| `sid` | Impersonation session uuid |
| `exp` | The session's `expires_at` |
| `email`, `username` | The target's |

`act` and `sid` come together or not at all; a token with only one of them, with a non-uuid
`act.sub`, or with `act.sub` equal to `sub` is rejected when parsed. There is no refresh token and
the session is never renewed: the token dies at `expires_at` (`IMPERSONATION_TTL`, default `1h`,
allowed `5m`–`2h`).

## Session check on every request

`BearerAuth` and `OptionalBearerAuth` pass impersonation tokens to `Service.Verify`, which accepts
the token only while all of these hold:

1. the session exists, is `active`, and has the token's actor and target;
2. `expires_at` has not passed — otherwise the session is closed as `expired`;
3. both accounts are active and the staff member still holds `impersonation.start` — otherwise the
   session is closed as `revoked` (`policy`).

A failed check answers `401 auth.impersonation_ended` (optional-auth routes treat the caller as
anonymous). Stopping the session therefore kills its token immediately. Without an impersonation
service wired, impersonation tokens are always refused.

## Eligibility and step-up

`Start` checks, in order:

1. `reason` is 10–255 characters (trimmed) — `400 validation_error`;
2. the caller holds `impersonation.start` — `403 rbac.forbidden`;
3. `current_password`, plus `two_factor_code` (TOTP or unused backup code, which is consumed) when
   the caller has 2FA enabled — `403 impersonation.step_up_required`. An enrolled account whose
   codes cannot be checked (no encryption key) fails closed;
4. the target exists, is active, and is not the caller — `422 impersonation.target_ineligible`;
5. the target does not have the `admin` role or `impersonation.start`, and every permission the
   target has is one the caller has (`*` covers everything) — `422 impersonation.target_ineligible`.
   Impersonation never adds rights;
6. the caller has no other active session — `409 impersonation.already_active` (a partial unique
   index enforces one active session per staff member).

The endpoint is rate limited to 10 requests per minute per caller (`impersonation.start`).

## Blocked operations

An impersonation token acts as the target everywhere except operations that change the target's
credentials, security, privacy, or sessions, or that would chain impersonations. These answer
`403 impersonation.forbidden_action`:

| Operation ids |
| --- |
| `me.password.update`, `me.email.request_change`, `me.email.confirm_change` |
| `me.2fa.get`, `me.2fa.setup`, `me.2fa.confirm`, `me.2fa.disable`, `me.2fa.backup_codes` |
| `me.privacy.update`, `me.activity.export`, `me.activity.erase` |
| `auth.logout`, `auth.refresh`, `auth.oauth.start`, `auth.oauth.callback` |
| `admin.impersonation.start` |

The guard (`middleware.ImpersonationGuard`) runs on every route after the route metadata is set,
through `routes.Controllers.Guard`.

## Endpoints

### `POST /api/v1/admin/impersonation/start`

Requires `impersonation.start` and a normal (non-impersonation) token.

```json
{
  "target_user_id": "uuid",
  "reason": "Ticket #4521: author cannot publish",
  "current_password": "…",
  "two_factor_code": "123456"
}
```

`201`:

```json
{
  "data": {
    "session": {
      "impersonation_session_id": "uuid",
      "impersonator_user_id": "uuid",
      "target_user_id": "uuid",
      "state": "active",
      "active": true,
      "reason": "Ticket #4521: author cannot publish",
      "started_at": "2026-09-25T10:00:00Z",
      "expires_at": "2026-09-25T11:00:00Z",
      "ended_at": null,
      "end_reason": null
    },
    "target_user": { "id": "uuid", "username": "…", "email": "…", "full_name": "…" },
    "access_token": "…",
    "token_type": "Bearer",
    "expires_in": 3600
  }
}
```

The client keeps the staff member's own tokens and sends the impersonation token instead while
impersonating.

### `POST /api/v1/admin/impersonation/stop`

With the impersonation token, ends that session; with the staff member's own token, ends their
active session. Needs no permission, so a staff member who lost it can still exit. `200` returns
the ended session; `404 impersonation.not_found` when there is none.

### `GET /api/v1/admin/impersonation/current`

Same token rules as stop. `200` with `{"active": bool, "session": {…} | null}`.

## Storage

`impersonation_sessions`:

| Column | Notes |
| --- | --- |
| `uuid` | Session id (`sid`) |
| `actor_id`, `target_id` | `users.id`, cascade on delete; must differ |
| `state` | `active`, `exited`, `expired`, `revoked` |
| `reason` | 10–255 characters |
| `ip_address`, `user_agent` | Of the start request |
| `started_at`, `expires_at` | `expires_at > started_at` |
| `ended_at`, `end_reason` | Set exactly when the state is not `active`; `manual_exit`, `expired`, `policy` |

Indexes: one active session per actor (partial unique), expiry of active sessions, and target
history.

## Audit

- `audit_logs.impersonator_id` holds the staff member for every action taken with an impersonation
  token; `actor_id` is the target. Metadata carries `impersonation_session_id`.
- Admin activity (`GET /api/v1/admin/users/{id}/activity`) returns `impersonator_id`; the user's
  own activity (`GET /api/v1/me/activity`) shows `impersonated: true` on those entries.
- `admin.impersonation.start` (category `impersonation_start`) records the staff member as actor,
  the target as resource, and the reason and session id as metadata.
  `admin.impersonation.stop` (category `impersonation_end`) is attributed to the staff member.

## Events

Recorded through the outbox in the same transaction as the session change, with the
`ImpersonationLifecycle` payload in [asyncapi.yaml](../architecture/asyncapi.yaml):

| Event | When |
| --- | --- |
| `blog.impersonation.started` | A session starts |
| `blog.impersonation.exited_manually` | Stop |
| `blog.impersonation.expired` | Expiry noticed on a request or by the `impersonation-expire` job |
| `blog.impersonation.revoked_by_policy` | A participant is no longer active or the staff member lost `impersonation.start` |

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `IMPERSONATION_TTL` | `1h` | Session and token lifetime; `5m`–`2h` |

`impersonation.start` is seeded on the `admin` role (which also holds `*`) so it appears in the
permission catalogue and can be granted to a support role.
