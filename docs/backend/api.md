# API

## Canonical Contract

The authoritative REST API contract lives at:

- [contracts/openapi.yaml](../../contracts/openapi.yaml) (OpenAPI 3.1)

Use `docs/backend/api.md` as a human-readable summary, but `contracts/openapi.yaml` is the source of truth for path definitions, schema, security, and status codes. Validation and code generation should reference the contract file.

## Conventions

- version routes under `/api/v1`
- use JSON request and response bodies
- use pagination for list endpoints
- return a consistent error envelope

## Main API Areas

- auth
- users
- user profile management (self-service + public)
- roles and permissions
- posts (including revisions and SEO)
- media
- categories
- tags
- comments
- notifications
- analytics
- settings
- impersonation
- health

## Example Self-Service Endpoints

- `GET /api/v1/me`
- `PATCH /api/v1/me/profile`
- `POST /api/v1/me/avatar` (multipart image upload; 5 MB max; returns media_asset URLs for 64/128/256/512px variants)
- `DELETE /api/v1/me/avatar` (detach avatar FK; optional media_asset archive via media lifecycle)
- `PUT /api/v1/me/password` (update password with current password proof + optional session family revocation)
- `POST /api/v1/me/email/request-change` (initiate email change flow with verification token)
- `POST /api/v1/me/email/confirm-change` (confirm pending email change)
- `GET /api/v1/me/privacy`
- `PUT /api/v1/me/privacy` (partial patch with allowlisted privacy flags; visibility changes require reverify)
- `GET /api/v1/me/activity` (paginated; category/date filters; CSV export via `?export=csv`)
- `GET /api/v1/me/activity/export` (async GDPR export job)
- `POST /api/v1/me/activity/erase` (GDPR/CCPA erasure request with password reverify)
- `GET /api/v1/me/notifications`
- `GET /api/v1/me/notifications/stream` (SSE)
- `POST /api/v1/me/notifications/:id/read`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `POST /api/v1/auth/2fa/challenge`
- `POST /api/v1/auth/oauth/:provider/callback`
- `POST /api/v1/auth/password/forgot` (timing-safe, no user enumeration; 5/hour 15/day per email + IP)
- `GET /api/v1/auth/password/reset/{token}` (validity check for UI)
- `POST /api/v1/auth/password/reset` (consume token + apply new password; single-use Redis jti guard)

## Entity ↔ Media Response Conventions

Entities referencing media_assets should include associated media objects via `include` when requested, but keep compact responses by default.

Examples:

- `GET /api/v1/me?include=avatar` returns `avatar` object or avatar media ref
- `GET /api/v1/posts/:slug?include=cover,media` returns `cover` and ordered `media[]` from post_media
- `GET /api/v1/categories/:slug?include=image` returns category `image` cover object

All associations must be server-resolved from FKs and join tables rather than trusting client-supplied URLs.

## Notification SSE Endpoint

Authoritative contract: [paths/notifications.yaml](../../paths/notifications.yaml#L41-L153)
(OpenAPI 3.1) + AsyncAPI channel `notifications.user.{user_id}.stream` in
[contracts/asyncapi.yaml](../../contracts/asyncapi.yaml).
Full wire format, client integration, security, and scaling guidance is in the feature chapter
[realtime-notifications-sse.md](../features/realtime-notifications-sse.md).

- `GET /api/v1/me/notifications/stream` must require authentication (JWT bearer token OR valid
  http-only session cookie issued by `/api/v1/auth/login`). Browser-origin callers using
  cookies must send `X-CSRF-Token`; non-browser API tokens in the allowlist are CSRF-exempt.
- Each connected client must subscribe **only** to that user's notification fan-out keyed by
  JWT `sub`; server-side there is no path to upgrade a connection to another user.
- Response headers on 200 OK: `Content-Type: text/event-stream; charset=utf-8` (always),
  `Cache-Control: no-cache` (always), `Connection: keep-alive` on HTTP/1.1,
  `X-Accel-Buffering: no` for Nginx. Optional `Retry-After` + `X-RateLimit-Limit/Remaining`
  rate limit headers present when the endpoint is under load.
- Connection lifecycle includes:
  - periodic `event: ping` frames every `SSE_PING_INTERVAL_SECONDS` (default 15s) to keep
    intermediate proxy sockets alive
  - opening `retry: 5000` line for EventSource reconnect delay hint + `event: stream.opened`
    frame with `stream_id`, `user_id`, `retry_ms`, `replay_applied`, `replay_count`
  - graceful close on client disconnect (request context done), auth expiration
    (`event: stream.closed { code: "auth.expired" }`), session revocation, or server SIGTERM
    shutdown
  - watchdog-based cleanup of half-open TCP sockets after 2 missed pings
  - bounded per-connection write buffer (default 512); slow consumers overflow produces a
    single `event: error { code: "fanout.buffer_full", dropped_count: N }` frame and drops
    oldest events rather than blocking the global fan-out
- Event format on the wire (WHATWG Server-Sent Events):
  - `event: notification.created` / `notification.read` / `notification.dismissed` /
    `stream.opened` / `stream.closed` / `error` / `ping` / `session.invalidated_family`
  - `id: <monotonic opaque id>` present on every frame except `ping`
  - `data: <single-line compact JSON>`; payloads are standardised
  - `retry: <milliseconds>` exactly once during the handshake
- REST history companion endpoint `GET /api/v1/me/notifications` is the fallback catchment for
  events older than the bounded Redis replay ring (`SSE_REPLAY_MAX_EVENTS_PER_USER = 1000`,
  TTL `SSE_REPLAY_TTL_SECONDS = 600`) after `Last-Event-ID` reconnects.
- Fan-out happens through an internal Watermill → Redis pub/sub bridge. Single-process deployments
  can use Watermill in-process; multi-process deployments MUST fan out via Redis bus so any
  process can publish and every process's connected clients receive the frame.
- Delivery semantics are at-least-once; clients MUST deduplicate by `id:` or
  `data.notification_id`. `Last-Event-ID` (or query `?replay_after=`) replay restores gaps
  within the replay ring; gaps outside the ring are filled by the client calling
  `GET /api/v1/me/notifications` immediately after the reconnect.
- Rate limiting on SSE (all Redis-backed, keys documented in
  [realtime-notifications-sse.md](../features/realtime-notifications-sse.md) §4):
  - per-user concurrent streams: `SSE_MAX_CONCURRENT_PER_USER=3` (4th → 429
    `sse.stream.too_many_connections` + `Retry-After: 30`)
  - reconnect attempts per `ip:user` 60s window: 10 (excess → 429
    `sse.stream.reconnect_rate`)
  - aggressive reconnect jail: ≥ 5 failed connects inside 60s → 120s jail 429
    `sse.stream.jailed`

## Example Admin Endpoints

- `POST /api/v1/admin/auth/login`
- `GET /api/v1/admin/users`
- `POST /api/v1/admin/users`
- `GET /api/v1/admin/users/:id/profile` (requires `user.profile.read`; returns full profile + privacy + summary activity)
- `PATCH /api/v1/admin/users/:id/profile` (requires `user.profile.edit` + 2FA step-up)
- `POST /api/v1/admin/users/:id/password/admin-reset` (requires `user.password.admin_reset`; sends reset email or force-password-on-next-login)
- `GET /api/v1/admin/users/:id/activity` (requires `user.activity.read_all`; ignores target user privacy)
- `GET /api/v1/admin/posts`
- `POST /api/v1/admin/posts`
- `POST /api/v1/admin/posts/:id/publish`
- `PATCH /api/v1/admin/posts/:id/media` (replace post attachments join rows)
- `GET /api/v1/admin/posts/:id/revisions` (list revisions, paginated + filters)
- `GET /api/v1/admin/posts/:id/revisions/:revision_id_or_number` (get single revision with snapshot + diff)
- `POST /api/v1/admin/posts/:id/revisions/:revision_id_or_number/restore` (restore revision as new revision; body: restore_note optional)
- `GET /api/v1/admin/posts/:id/seo` (get SEO config + soft warnings)
- `PUT /api/v1/admin/posts/:id/seo` (update SEO and/or slug; allowlisted fields + soft warnings)
- `POST /api/v1/admin/posts/:id/seo/preview` (live SERP / OG / Twitter preview from draft SEO)
- `POST /api/v1/admin/media`
- `GET /api/v1/admin/media`
- `PATCH /api/v1/admin/media/:id/tags`
- `DELETE /api/v1/admin/media/:id`
- `GET /api/v1/admin/settings`
- `PUT /api/v1/admin/settings`
- `GET /api/v1/admin/settings/history` (optional)
- `POST /api/v1/admin/impersonation/start`
- `POST /api/v1/admin/impersonation/stop`
- `GET /api/v1/admin/impersonation/current`

## Admin Settings Endpoint Rules

- `GET /api/v1/admin/settings` requires authentication and the `settings.read` permission
- `PUT /api/v1/admin/settings` requires authentication and the `settings.update` permission
- `PUT` uses partial update semantics with allowlisted keys and strict validation
- all access and modification attempts must produce audit logs
- never return `server_only` keys through the API
- enforce CORS whitelist, CSRF protection for browser clients, and rate limits for `PUT`

## Admin Impersonation Endpoint Rules

- `POST /api/v1/admin/impersonation/start` requires `superadmin` role + `impersonation.start` permission + 2FA step-up where configured
- `POST /api/v1/admin/impersonation/stop` requires authenticated impersonation state + `impersonation.stop` permission
- `GET /api/v1/admin/impersonation/current` returns active impersonation state for the caller; 403 if not authorized
- all state-changing impersonation requests require CSRF protection for browser clients
- the impersonated session must resolve effective user = target user for downstream RBAC
- every lifecycle event and every subsequent action performed during impersonation must be auditable

## Admin Post Revision Endpoint Rules

- `GET /api/v1/admin/posts/:id/revisions` requires auth + `post.revisions.view` or ownership permission for authors
- `GET /api/v1/admin/posts/:id/revisions/:revision_id_or_number` requires same auth as list; supports revision_number for friendly navigation
- `POST /api/v1/admin/posts/:id/revisions/:revision_id_or_number/restore` requires `post.revisions.restore` permission + CSRF protection for browser clients
- restore creates a new revision row (type=restore) with `restore_from_revision_id` set, never mutates existing revisions
- restore restores content/excerpt/slug/status/category/cover/media attachments/tags/SEO snapshot exactly
- every revision must record `author_id` (effective user) and, when applicable, impersonator metadata for audit

## Admin Post SEO Endpoint Rules

- `GET /api/v1/admin/posts/:id/seo` requires auth + `post.seo.view` or ownership
- `PUT /api/v1/admin/posts/:id/seo` requires auth + `post.seo.edit`; editing the slug additionally requires `post.slug.edit` (or combined admin permission)
- `PUT` uses partial-update semantics with an allowlisted fields set; unknown keys rejected
- `POST /api/v1/admin/posts/:id/seo/preview` accepts draft (unsaved) SEO and returns search/OG/Twitter previews plus a soft `warnings` array for length / duplicate / image issues
- slug changes trigger a `blog.post.slug_changed` event and create a new post revision with diff/changelog
- all SEO field updates are included in the next post revision snapshot + diff + changelog

## Example Public Endpoints

- `GET /api/v1/posts`
- `GET /api/v1/posts/:slug`
- `GET /api/v1/posts/:slug/seo-meta` (public structured SEO meta tags payload for SSR; published posts only, cached)
- `GET /api/v1/categories`
- `GET /api/v1/tags`
- `POST /api/v1/posts/:id/comments`
- `GET /api/v1/media/:id`
- `GET /api/v1/media/:id/transform?w=800&h=450&fit=cover&format=webp&quality=82`
- `GET /api/v1/users/:username_or_id` (public profile; respects user_privacy_settings: 404 when private; filters contact details/email/activity via privacy toggles; `include=avatar,posts_preview,roles_brief` supported)

## Media Notes

- media upload endpoints must validate content type, file size, and malware scan result
- transformed media endpoints generate variants on demand
- transformed responses may be cached in memory, Redis, object storage, or a combination
- when deleting or replacing media referenced by `users.avatar_id`, `posts.cover_image_media_id`, `post_media`, or `categories.image_id`, referential integrity rules must be respected (SET NULL or CASCADE depending on FK policy)
- when exposing media associations on users, posts, and categories, prefer server-resolved media objects over raw URLs; use `include=` to opt in.

## User Profile Endpoint Rules

### Self-Service Rules (all `/me/*`)

- all `/me/*` endpoints require valid JWT access token; effective user = JWT `sub` (or effective user during impersonation)
- every state-changing request (`PATCH PUT POST DELETE`) requires CSRF token for browser clients (non-browser API tokens exempted only when configured via admin security policy)
- allowlisted patch keys only; unknown keys → `400 validation_error` with `code=profile.unknown_field`
- high-risk actions require step-up re-authentication:
  - password change: `X-Re-Verify-Password` header with current password OR `X-2FA-Verified` within 5 minutes
  - email change: `X-Re-Verify-Password` or recent 2FA
  - avatar delete: same
  - privacy visibility direction `public → private`: same
  - activity erase: same
- password/email change MUST rotate the user's refresh-token family unless caller explicitly opts in to keep the current session only via `keep_current_session=true` header; `revoke_all_sessions=true` overrides any opt-in
- forgot/reset password endpoints (unauthenticated) return timing-safe identical responses regardless of user existence to prevent email/username enumeration
- contact fields marked `encrypted` (phone) are never serialized to logs/audit metadata; only HMAC hashes are allowed
- rate limits apply per endpoint family; see `docs/backend/user-profile.md` for exact tiers and Retry-After behavior

### Public User Rules

- `GET /api/v1/users/:username_or_id` applies user_privacy_settings exactly:
  - `visibility_profile=private` → `404` for non-owner / non-admin callers
  - `visibility_profile=followers_only` → 404 unless caller is confirmed follower relationship or admin
  - `visibility_contact_details=false` → phone/website/location fields omitted even for owner (no-op; owner always sees via `/me`; public never sees)
  - `visibility_email=false` → email omitted on public payload (default: always false; email is admin-only + owner only)
  - `search_allow_indexing=false` → injects `X-Robots-Tag: noindex` response header in SSR + frontend HTML
- public profile response does NOT include oauth links, sessions, private activity, hashed passwords, encrypted phone, full IPs, or internal ids like jti/request_id

## Response Shape

```json
{
  "data": {},
  "meta": {},
  "error": null
}
```

## Error Shape

```json
{
  "data": null,
  "meta": {
    "request_id": "..."
  },
  "error": {
    "code": "validation_error",
    "message": "title is required",
    "details": {}
  }
}
```
