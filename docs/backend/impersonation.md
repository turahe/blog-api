# Impersonation Backend

Staff with `impersonation.start` can act as another user to reproduce a problem. Starting a
session returns a separate, short-lived access token whose subject is the target and whose RFC 8693
`act` claim names the staff member. Every request with that token runs as the target, is checked
against the live session, and is audited with both identities, reads included. The session lives
only as long as the staff member's own sign-in.

Product spec: [impersonation.md](../features/impersonation.md).

## Placement

| Layer | Package | Role |
| --- | --- | --- |
| Domain | `internal/core/impersonation/domain` | `Session`, states and end reasons, `Grants.CanImpersonate` (eligibility), errors |
| Ports | `internal/core/impersonation/ports` | `Repository`, `Users`, `Permissions`, `StepUp`, `Tokens` |
| Service | `internal/core/impersonation/service` | `Start`, `Verify`, `Stop`, `Current`, `ExpireStale`; records lifecycle events |
| Persistence | `persistence.ImpersonationRepository` | `impersonation_sessions` (migrations 00029, 00031); reads the base sign-in from `refresh_sessions` |
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
the session is never renewed: the token dies at `expiresAt` (`IMPERSONATION_TTL`, default `1h`,
allowed `5m`–`2h`).

Ordinary access tokens carry a `fam` claim: the refresh-session family (the sign-in) they were
issued for, kept across refresh rotation. An impersonation token never carries `fam`; a token with
both `act` and `fam`, or a `fam` that is not a uuid, is rejected when parsed.

Responses to requests made with an impersonation token carry `X-Impersonation-Session: <sid>`, so
a client can tell it is acting as someone else.

## Session check on every request

`BearerAuth` and `OptionalBearerAuth` pass impersonation tokens to `Service.Verify`, which accepts
the token only while all of these hold:

1. the session exists, is `active`, and has the token's actor and target;
2. `expiresAt` has not passed — otherwise the session is closed as `expired`;
3. the staff member's base sign-in is live: the refresh-session family recorded at start still has
   an unrevoked, unexpired session — otherwise the session is closed as `revoked`
   (`parent_session_expired`). Logout, a password change or reset, an admin password reset that revokes
   sessions, refresh-token reuse, and the refresh session expiring all end it;
4. both accounts are active and the staff member still holds `impersonation.start` — otherwise the
   session is closed as `revoked` (`policy`).

A failed check answers `401 auth.impersonation_ended` (optional-auth routes treat the caller as
anonymous). Stopping the session therefore kills its token immediately. Without an impersonation
service wired, impersonation tokens are always refused.

The notification stream (`GET /api/v1/me/notifications/stream`) repeats the check before every event
and heartbeat. Once the session ends it sends `stream.closed` with `{"code":"impersonation_ended"}`
and closes, so an open stream does not outlive the session.

## Eligibility and step-up

`Start` checks, in order:

1. `reason` is 10–255 characters (trimmed) — `400 validation_error`;
2. the caller holds `impersonation.start` — `403 rbac.forbidden`;
3. `currentPassword`, plus `twoFactorCode` (TOTP or unused backup code, which is consumed) when
   the caller has 2FA enabled — `403 impersonation.step_up_required`. An enrolled account whose
   codes cannot be checked (no encryption key) fails closed;
4. the caller's token names its sign-in (`fam`) — `401 impersonation.sign_in_required`; refreshing
   the token supplies it;
5. the target exists, is active, and is not the caller — `422 impersonation.target_ineligible`;
6. the target does not have the `admin` role or `impersonation.start`, and every permission the
   target has is one the caller has (`*` covers everything) — `422 impersonation.target_ineligible`.
   Impersonation never adds rights;
7. the caller has no other active session — `409 impersonation.already_active` (a partial unique
   index enforces one active session per staff member). A session that is past `expiresAt` or
   whose base sign-in ended, but was not closed yet, is closed first and does not block.

The endpoint is rate limited to 10 requests per minute per caller (`impersonation.start`).

## Blocked operations

An impersonation token acts as the target everywhere except operations that change the target's
credentials, security, privacy, consent, or sessions, or that would chain impersonations. Consent
must come from the data subject, so staff cannot give or withdraw it on the user's behalf. These
answer `403 impersonation.forbidden_action`:

| Operation ids |
| --- |
| `me.password.update`, `me.email.request_change`, `me.email.confirm_change` |
| `me.2fa.get`, `me.2fa.setup`, `me.2fa.confirm`, `me.2fa.disable`, `me.2fa.backup_codes` |
| `me.privacy.update`, `me.activity.export`, `me.activity.erase` |
| `me.newsletter.subscribe`, `me.newsletter.unsubscribe`, `analytics.consent.store`, `analytics.consent.withdraw` |
| `auth.logout`, `auth.refresh`, `auth.oauth.start`, `auth.oauth.callback` |
| `admin.impersonation.start` |

The guard (`middleware.ImpersonationGuard`) runs on every route after the route metadata is set,
through `routes.Controllers.Guard`.

## Endpoints

### `POST /api/v1/admin/impersonation/start`

Requires `impersonation.start` and a normal (non-impersonation) token.

```json
{
  "targetUserId": "uuid",
  "reason": "Ticket #4521: author cannot publish",
  "currentPassword": "…",
  "twoFactorCode": "123456"
}
```

`201`:

```json
{
  "data": {
    "session": {
      "impersonationSessionId": "uuid",
      "impersonatorUserId": "uuid",
      "targetUserId": "uuid",
      "state": "active",
      "active": true,
      "reason": "Ticket #4521: author cannot publish",
      "startedAt": "2026-09-25T10:00:00Z",
      "expiresAt": "2026-09-25T11:00:00Z",
      "endedAt": null,
      "endReason": null
    },
    "targetUser": { "id": "uuid", "username": "…", "email": "…", "fullName": "…" },
    "accessToken": "…",
    "tokenType": "Bearer",
    "expiresIn": 3600
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
| `ended_at`, `end_reason` | Set exactly when the state is not `active`; `manual_exit`, `expired`, `policy`, `parent_session_expired` |
| `base_family_id` | Refresh-session family of the staff member's sign-in at start. Null only on sessions opened before migration 00031; those end on their next use |

Indexes: one active session per actor (partial unique), expiry of active sessions, and target
history.

## Audit

- Every request made with an impersonation token is recorded: reads as well as changes, failures
  and server errors included, and regardless of the lists that skip routine operations for normal
  callers. `actor_id` is the target, `audit_logs.impersonator_id` the staff member, and metadata
  carries `impersonation_session_id`. Reads have no activity category, so they appear in admin
  activity but not in the user's own feed.
- A request the guard refused is recorded as a failure with `failure_reason:
  impersonation.forbidden_action`.
- Admin activity (`GET /api/v1/admin/users/{id}/activity`) returns `impersonatorId`; the user's
  own activity (`GET /api/v1/me/activity`) shows `impersonated: true` on those entries.
- `admin.impersonation.start` (category `impersonation_start`) records the staff member as actor,
  the target as resource, and the reason and session id as metadata. Refused starts are recorded
  too, with `failure_reason`: `validation`, `forbidden`, `step_up_required`, `sign_in_required`,
  `target_ineligible`, `already_active`, or `error`.
  `admin.impersonation.stop` (category `impersonation_end`) is attributed to the staff member.
- Ends that happen without a request (expiry, policy revocation, base sign-in ending) are recorded
  on the session row (`state`, `end_reason`, `ended_at`) and as lifecycle events, not in
  `audit_logs`. A request with a token whose session already ended answers `401` before identity is
  set and is not audited.

## Events

Recorded through the outbox in the same transaction as the session change, with the
`ImpersonationLifecycle` payload in [asyncapi.yaml](../architecture/asyncapi.yaml):

| Event | When |
| --- | --- |
| `blog.impersonation.started` | A session starts |
| `blog.impersonation.exited_manually` | Stop |
| `blog.impersonation.expired` | Expiry noticed on a request or by the `impersonation-expire` job |
| `blog.impersonation.revoked_by_policy` | A participant is no longer active, the staff member lost `impersonation.start` (`reason_code: policy`), or their sign-in ended (`reason_code: parent_session_expired`) |

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `IMPERSONATION_TTL` | `1h` | Session and token lifetime; `5m`–`2h` |

`impersonation.start` is seeded on the `admin` role (which also holds `*`) so it appears in the
permission catalogue and can be granted to a support role.
