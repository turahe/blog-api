# Events

## Canonical Contract

The authoritative events and streaming contract lives at:

- [asyncapi.yaml](../architecture/asyncapi.yaml) (AsyncAPI 2.6)

Use `docs/backend/events.md` as a human overview; the AsyncAPI contract is the source of truth for channel names, message payloads, security requirements, SSE bindings, and job topics.

## Event Bus Strategy

Use Watermill for domain and integration events. Prefer transactional consistency through an outbox pattern.

Transports are selected at runtime with `MESSAGE_BROKER` (see [architecture/tech-stack.md](../architecture/tech-stack.md) and [messaging brokers design](../superpowers/specs/2026-07-30-messaging-brokers-design.md)):

| `MESSAGE_BROKER` | Broker | Local | Notes |
|------------------|--------|-------|-------|
| `kafka` | Apache Kafka | Compose profile `messaging` | `KAFKA_BROKERS`, `KAFKA_CONSUMER_GROUP` |
| `rabbitmq` | RabbitMQ | Compose profile `messaging` | `RABBITMQ_URL` |
| `googlepubsub` | Google Cloud Pub/Sub | GCP project + ADC / Workload Identity | `GOOGLE_PUBSUB_PROJECT_ID` |

Platform factory: `internal/platform/messaging`. Core domain code must not import Watermill or broker clients.

## Initial Event Catalogue

- `blog.post.created`
- `blog.post.updated`
- `blog.post.published`
- `blog.post.archived`
- `blog.comment.created`
- `blog.comment.approved`
- `blog.auth.logged_in`
- `blog.user.created`
- `blog.user.role_assigned`
- `blog.role.updated`
- `blog.media.uploaded`
- `blog.media.deleted`
- `blog.media.variant.generated`
- `blog.settings.updated`
- `blog.settings.cache.invalidated`
- `blog.settings.security.updated`
- `blog.impersonation.start_attempted`
- `blog.impersonation.started`
- `blog.impersonation.start_failed`
- `blog.impersonation.exited_manually`
- `blog.impersonation.expired`
- `blog.impersonation.revoked_by_policy`
- `blog.impersonation.action_performed`
- `blog.post.revision.created`
- `blog.post.revision.restored`
- `blog.post.seo.updated`
- `blog.post.slug_changed`
- `user.profile.updated` — any self-service or admin edit to display/bio/contact/social/locale/timezone/marketing-consent fields; payload contains redacted before/after diff
- `user.profile.avatar_updated` — avatar attach, detach, or replace; old_media_asset_id/new_media_asset_id/variants URLs
- `user.password.changed` — authenticated update flow success; includes revoke_all_sessions flag and invalidated_family boolean
- `user.password.admin_reset_triggered` — admin sent reset email or set force-password-on-next-login
- `auth.password.reset_requested` — `/auth/password/forgot` timing-safe attempted payload: user_id may be null; email_address_hash peppered HMAC; status=attempted
- `user.password.reset_completed` — `/auth/password/reset` success: invalidates token jti, rotates session family
- `user.email.change_requested` — user submitted new_email + password proof; tokens issued
- `user.email.changed` — confirm-change success; old/new HMAC hashes, revoked_sessions_count
- `user.email.change_cancelled` — cancel token or old email user cancelled the pending change via warning email link
- `user.privacy.updated` — any privacy flag change; before/after diff; includes direction-aware step-up proof presence flag
- `user.activity.export_requested` — GDPR export job queued; job_id, requested_by (self or admin with user.activity.read_all)
- `user.activity.erasure_requested` — GDPR/CCPA erasure; scope and retention window; erase_marker uuid
- `user.session.invalidated_family` — emitted when password/email/avatar-security changes invalidate all refresh tokens
- `user.consent.marketing_granted` / `user.consent.marketing_withdrawn` — mirror ConsentService events when marketing_consent changes via profile endpoint; drives email unsubscribe lists

## Event Consumers

- cache invalidation
- notifications
  - email delivery
  - fan-out to SSE notification streams per user
- audit enrichment
  - impersonation metadata propagation to business action audit entries
- analytics hooks
- future search indexing
- media cleanup and cache invalidation hooks
- settings cache invalidation
- SEO/media re-warmup hooks when site-wide settings change
- impersonation session expirer and parent-session revocation hooks
- realtime admin activity streams for impersonation governance
- post revision events:
  - cache invalidation for post detail and revisions list
  - audit log enrichment (especially restores)
  - notification fanout to watching editors when revisions are created/restored
- post SEO events:
  - `blog.post.seo.updated` → invalidates `/posts/{slug}/seo-meta` cache and post detail cache
  - `blog.post.slug_changed` → redirect rule creation consumer, sitemap regeneration, related internal-link rewriters, analytics URL mapping updates
  - SEO update consumer that warms public SEO meta cache after updates
- user profile events:
  - `user.profile.updated` → invalidates `user:me:*` cache, public cache for username, admin search user cache; optionally triggers search index update if public profile search enabled
  - `user.profile.avatar_updated` → invalidates user caches; invokes MediaService usage counter + orphaned-variant cleanup; notifies email user with "your avatar has changed" (optional)
  - `user.password.changed`, `user.password.reset_completed` → `UserSessionRepository` invalidates refresh family; emits notification to user's recovery/warning email; audit enrichment
  - `auth.password.reset_requested` → queue email delivery worker via SMTP adapter per settings smtp config; rate limit monitor; no email leaks via response
  - `user.email.change_requested` → send verification email to new address + cancellation notice to old address
  - `user.email.changed` → invalidate sessions; send "email changed" to both old/new; trigger email list sync workers
  - `user.privacy.updated` → immediate public profile cache invalidation; updates SSR robots headers source of truth; propagates tracking_personalize_ads to ConsentService
  - `user.activity.export_requested` → async job: materialize user_activity CSV for 395 day retention window, sign a short-lived download URL, deliver via in-app notification + email
  - `user.activity.erasure_requested` → async erasure worker: zero PII fields in user_activity (ip_address, geo*, ua bucket, PII in detail_jsonb), erase-mark rows; invoke ConsentService erasure hooks if scope=full; deliver confirmation to user + audit
  - `user.session.invalidated_family` → refresh-token store bulk rotate key; realtime SSE to connected clients to re-login

## Event Payload Rules

- include event ID
- include event name
- include occurred-at timestamp
- include aggregate ID
- include actor ID when available
- keep payload versionable

## Streaming Channels and SSE Fan-Out

In addition to the point-to-point Watermill channels listed above, the backend exposes
parameterised streaming channels that are consumed by inbound SSE adapters. The authoritative
binding for these channels lives in [asyncapi.yaml](../architecture/asyncapi.yaml)
(server `sse-local` and `sse-prod`), but the human-facing catalogue is:

| Channel | SSE endpoint | Consumer identity | Purpose |
|---------|--------------|-------------------|---------|
| `notifications.user.{user_id}.stream` | `GET /api/v1/me/notifications/stream` | authenticated `user_id == sub` claim | Realtime per-user notification events (created/read/dismissed) plus stream lifecycle frames |
| `analytics.admin.realtime.stream` | `GET /api/v1/admin/analytics/realtime/stream` | authenticated admin with `analytics.read_all` | Live dashboards, impersonation audit trail, system health |

### SSE Event Catalogue

Each SSE frame is serialized according to the WHATWG Server-Sent Events specification using
`event:`, `id:`, `data:`, and optional `retry:` lines. Data payloads are compact single-line
JSON. All streams deliver the following control frames in addition to domain events:

- `event: stream.opened` — emitted exactly once per successful connection, `data` contains
  `{ "stream_id": <uuid>, "user_id": <uuid>, "server_ts": <ISO-8601>, "retry_ms": 5000 }`.
- `event: stream.closed` — emitted on server-initiated shutdown. The `data.code` field is one
  of: `auth.expired`, `session.revoked`, `too_many_connections`, `shutdown`,
  `maintenance`. Clients should not reconnect until `retry_ms` has elapsed.
- `event: ping` — keep-alive every `SSE_PING_INTERVAL_SECONDS` (default 15 seconds). `data` is
  `{ "ts": <ISO-8601 server wall clock> }`.
- `event: error` — transient server-side error that does not close the stream, for example
  a single fan-out write that was dropped because the consumer buffer was full. `data.code`
  is a stable machine-readable enum.

Notification stream frames on `notifications.user.{user_id}.stream`:

- `event: notification.created` — a new in-app notification has been stored for the user.
  `data` mirrors the REST `Notification` shape: `{ notification_id, type, title, preview,
  read: false, created_at, channel: "sse" }`.
- `event: notification.read` — one or more notifications have been marked read (via REST
  `POST /api/v1/me/notifications/{id}/read` or bulk). `data` is
  `{ "ids": [<uuid>, ...], "read_at": <ISO-8601> }`.
- `event: notification.dismissed` — user dismissed a single notification from the UI.
  `data` is `{ "id": <uuid> }`.
- `event: session.invalidated_family` — password/email/security change caused the refresh
  family to be rotated. Clients MUST tear down local session state and navigate to login.
  Emitted from the Watermill consumer handling `user.session.invalidated_family`.

### SSE Delivery Guarantees

- At-least-once delivery; clients deduplicate by `id:` or `data.notification_id`.
- On reconnect, `Last-Event-ID` is passed to the stream endpoint. The server replays missed
  events from a bounded in-memory or Redis-backed replay ring (default last 1000 events per
  user with TTL 10 minutes). Events older than the replay window are fetched by calling the
  REST `GET /api/v1/me/notifications` endpoint from the client after reconnect.
- Idle sockets are kept alive by `event: ping` frames; clients that miss two consecutive pings
  SHOULD initiate a reconnect before the upstream proxy idle timeout (configured separately;
  recommended Nginx `proxy_read_timeout` is 120s or higher).
