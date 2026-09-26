# Events

## Canonical Contract

The authoritative events and streaming contract lives at:

- [asyncapi.yaml](../architecture/asyncapi.yaml) (AsyncAPI 2.6)

Use `docs/backend/events.md` as a human overview; the AsyncAPI contract is the source of truth for channel names, message payloads, security requirements, SSE bindings, and job topics.

Two checks keep it honest: the `AsyncAPI` workflow (`make asyncapi-validate` locally) fails on
schema errors, and `TestEventTypesHaveAsyncAPIChannels` fails when an event type in
`internal/core/event/types.go` has no channel.

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
- `blog.comment.moderated`
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

### Emitted today

Recorded in the same transaction as the write (only when `MESSAGE_BROKER` is set):

| Event | Recorded by | Actor |
| --- | --- | --- |
| `blog.post.created` | Create draft | Author |
| `blog.post.updated` | Update; unpublish (back to draft) | Editor; none |
| `blog.post.published` | Publish | Admin who published, when known |
| `blog.post.archived` | Archive | None |
| `blog.comment.created` | Create comment (any initial status, including `pending` and `spam`) | Author; none for guests |
| `blog.comment.moderated` | Moderate, bulk moderate (one event per comment), hard delete | Moderator |
| `blog.user.created` | Admin creates a user | None |
| `auth.password.reset_requested` | A reset token is issued (forgot password or admin reset) | None |
| `blog.media.uploaded` | Direct upload, or completing a presigned upload | Uploader |
| `blog.media.deleted` | Delete media | Uploader of the asset |
| `blog.settings.updated` | Admin settings update that changed at least one key (one event per request) | Admin |
| `blog.post.revision.created` | Every post write (create, update, media replace, publish, unpublish, archive, delete, undelete, restore) | Acting user; none for system writes |
| `blog.post.revision.restored` | Restore of a revision, alongside its `revision.created` | Restoring user |
| `blog.post.seo.updated` | SEO update that changed at least one SEO field; `changed_fields` lists them | Editing user |
| `blog.post.slug_changed` | Slug change through the SEO endpoint, with `old_slug` and `new_slug` | Editing user |
| `user.privacy.updated` | A `/me/privacy` update that changed at least one flag; before/after values and `stepup_proof_present` | The user |
| `blog.impersonation.started` | A staff member starts impersonating a user | Staff member |
| `blog.impersonation.exited_manually` | Stop | Staff member |
| `blog.impersonation.expired` | Expiry noticed on a request or by the `impersonation-expire` job | Staff member |
| `blog.impersonation.revoked_by_policy` | A participant is no longer active, or the staff member lost `impersonation.start` | Staff member |
| `user.activity.export_requested` | A new `/me/activity/export` job is queued; `job_id`, `scope`, `requested_by_admin` | The user |
| `user.activity.erasure_requested` | A new `/me/activity/erase` request is queued; `erasure_id`, `scope` | The user |
| `blog.newsletter.issue.send_requested` | An issue is queued, by an editor or the `newsletter-release` job; consumed by `newsletter-dispatch` | Editor; none for the job |
| `blog.newsletter.subscriber.changed` | Confirm, list or preference change, resubscribe, unsubscribe, bounce, complaint, or erase; `change` names which. Consumed by `newsletter-provider-sync` when `NEWSLETTER_PROVIDER=custom_http` | The user or admin; none for public token and webhook changes |
| `analytics.consent.granted` / `rejected` / `withdrawn` | A consent decision that changed status, one event per purpose; payload carries the pseudonymous subject id, purpose, and policy version | None |

Payloads carry identifiers and state, never email addresses or content. The rest of the
catalogue above is planned.

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

## Envelope

Metadata travels in Watermill message metadata (broker headers), following the
`EventEnvelope` trait in [asyncapi.yaml](../architecture/asyncapi.yaml). The message body is the
JSON payload only.

| Header | Always | Value |
| --- | --- | --- |
| `id` | Yes | Event UUID; also the Watermill message UUID. Consumers dedupe on it. |
| `type` | Yes | Event name, for example `blog.post.published` (the topic before `MESSAGE_TOPIC_PREFIX`). |
| `schema_version` | Yes | Payload version, starting at `1`. Additive changes keep the version; renames or removals bump it. |
| `source` | Yes | `blog-api`. |
| `timestamp` | Yes | When the change happened (RFC 3339, UTC). |
| `aggregate_type`, `aggregate_id` | When known | The entity that changed (`post`, `comment`, `user`, `media`) and its public UUID. |
| `actor_id` | When known | The user who caused the change. |
| `correlation_id` | For HTTP requests | The request's `X-Request-ID`. |

Core services build events with `internal/core/event` (`event.New`) and never import
Watermill.

## Delivery

Events go through a transactional outbox:

1. A service runs its writes and `event.Recorder.Record` inside `event.Transactor.InTx`. The
   rows in `outbox_events` commit or roll back with the change. Without `MESSAGE_BROKER`, the
   recorder discards events and nothing is stored.
2. The relay in `app worker` claims due rows (`FOR UPDATE SKIP LOCKED`, `OUTBOX_BATCH_SIZE`
   per transaction), publishes each one, and records the outcome before committing. Several
   workers can run at once.
3. A failed publish is retried after 1s, 2s, 4s, … up to 10m. After `OUTBOX_MAX_ATTEMPTS` the
   row is parked (`failed_at`); fix the cause and run `app outbox retry`. `app outbox status`
   shows the backlog.
4. Published rows are deleted after `OUTBOX_RETENTION`.

Guarantees:

- **At least once.** A crash between publishing and committing publishes the row again.
  Consumers must be idempotent: dedupe on the `id` header.
- **No global order.** One relay publishes due rows in `occurred_at` order, but retries and
  concurrent relays can reorder events, even for one aggregate. Consumers that care compare
  `timestamp` or reread current state.
- **Per broker.** Kafka keeps order within a partition (the relay does not set a partition
  key). RabbitMQ keeps order per queue while there is one consumer. Google Pub/Sub does not
  order messages (ordering keys are not used). All three redeliver unacknowledged messages.
- **First start.** `app worker` subscribes its consumers before the relay publishes, because a
  RabbitMQ fanout exchange drops messages no queue is bound for yet. A new Kafka consumer group
  starts from the oldest offset. Topics without a consumer (most `blog.*` events today) are
  still dropped by RabbitMQ until something subscribes.

Metrics (on `METRICS_ADDR` in `app worker`): `blog_outbox_published_total`,
`blog_outbox_publish_failures_total{terminal}`, `blog_outbox_pending`, `blog_outbox_failed`, and
`blog_outbox_lag_seconds` (age of the oldest pending event).

## Worker Consumers

`app worker` handlers run behind `messaging.NewRouter`, outermost first:

1. **Poison queue.** A message that still fails is published to `blog.dead_letter` (with
   `MESSAGE_TOPIC_PREFIX`) and acked, so it stops blocking the subscription. Watermill adds
   `reason_poisoned`, `topic_poisoned`, and `handler_poisoned` to the original headers, so
   handler errors must not contain secrets. Inspect or replay dead letters with broker tools.
2. **Correlation.** The `correlation_id` header becomes the context request id, so handler
   logs carry the `request_id` of the API call that produced the event.
3. **Tracing**, then **retry**: `CONSUMER_MAX_RETRIES` attempts after the first, backing off
   from `CONSUMER_RETRY_INTERVAL` and doubling up to `CONSUMER_RETRY_MAX_INTERVAL`. Errors
   wrapping `messaging.ErrPermanent` skip the retries.
4. **Recoverer.** A panic becomes an error, so it is retried and dead-lettered like any other.
5. **Circuit breaker** (per consumer). After `CONSUMER_BREAKER_FAILURES` consecutive transient
   failures the breaker opens for `CONSUMER_BREAKER_TIMEOUT`. Messages that arrive meanwhile
   return `messaging.ErrCircuitOpen` after a short pause: they skip the retries and the dead
   letter topic and are nacked, so the broker redelivers them once the downstream recovers.

`CONSUMER_CONCURRENCY` registers that many copies of each consumer (`email-dispatch-1`, …),
sharing one breaker and one dedupe key. Worker probes and incident steps:
[deployment.md](../architecture/deployment.md#process-roles), [runbook.md](../deployment/runbook.md).

Handlers wrapped with `messaging.Idempotent` claim `(consumer, message id)` in
`processed_messages` in the same transaction as their work. A redelivered message is acked
without running the handler; a failed attempt rolls the claim back. Claims are pruned after
`CONSUMER_DEDUPE_RETENTION`.

| Consumer | Topic | Does |
| --- | --- | --- |
| `email-dispatch` | `notification.email.requested` | Decrypts the command and sends it through the mail driver |
| `notification-dispatch` | `notification.requested` | Renders and stores in-app notifications, then announces them on `notifications.created` |
| `audit-writer` | `audit.entries.recorded` | Decrypts an audit batch and inserts it into `audit_logs` |

With a broker configured, emails, in-app notifications, and audit entries are all queued for
`app worker` instead of being handled inside API requests. Each queue falls back to the old
in-process path when it cannot hand its work over, so a broker outage slows these features
down but does not lose their data. In production `MESSAGE_BROKER` requires
`APP_ENCRYPTION_KEY`, because the email and audit queues are encrypted with it.

### Email dispatch

With `MESSAGE_BROKER` and `APP_ENCRYPTION_KEY` set, the API renders each email and records a
`notification.email.requested` command in the outbox instead of calling the mail driver. The
payload is `{"ciphertext": "..."}`: recipient, subject, and body sealed with AES-GCM under the
`APP_ENCRYPTION_KEY`-derived key, because reset and verification emails contain raw tokens.
The broker, the outbox, and dead letters only hold ciphertext. The worker needs the same key
and mail settings (`MAIL_DRIVER` and its credentials); an undecryptable command is
dead-lettered without retries. When the key is unset (outside production), or the command
cannot be stored, the API sends inline.

### Notification dispatch

With `MESSAGE_BROKER` set, a comment reply, a moderation decision, or a publication records one
`notification.requested` command in the outbox, after the change itself has committed. The
command carries only what the templates render from (ids, the reply's author name and text,
the moderation outcome and reason, the post title and slug), never emails, IP hashes, or user
agents. The worker looks up the recipients, renders the web and SSE copy, and stores one
notification per recipient; publishing a post with many commenters therefore costs the API one
insert instead of one per commenter. Each stored notification keeps its dedupe key, so a
redelivered or retried command never notifies anyone twice, and a failed recipient makes the
whole command retry without repeating the ones already stored. New notifications are
announced on `notifications.created`, which every API replica relays to its SSE streams. When
the command cannot be stored, the API delivers inline.

### Audit writer

With `MESSAGE_BROKER` and `APP_ENCRYPTION_KEY` set, the API's non-blocking audit recorder
still batches entries in memory (`AUDIT_QUEUE_SIZE`), but each batch is published straight to
`audit.entries.recorded` instead of being inserted. Audit entries carry client IPs and user
agents, so the payload is `{"ciphertext": "..."}` under the same key as emails. When the
publish fails the API inserts the batch itself; only a batch lost on both paths counts as
dropped. The worker inserts batches with `ON CONFLICT (uuid) DO NOTHING`, so redeliveries are
harmless and the consumer keeps no dedupe record.

Audit batches skip the outbox, so they rely on the broker to keep them until the worker reads
them. Kafka keeps them regardless. With RabbitMQ and Google Pub/Sub, the worker's durable
queue or subscription must already exist, so start `app worker` once before API replicas get
`MESSAGE_BROKER`; until then published batches are discarded.

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
