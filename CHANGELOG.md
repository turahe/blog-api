# Changelog

## 2026-09-25 — Live analytics stream

### Added

- `GET /api/v1/admin/analytics/realtime/stream` (SSE): every 5 seconds, the page views and
  searches accepted since the last frame (at most 20 of each) and a summary with sessions active
  in the last 5 minutes, views and searches per minute for the last 30, and the top 10 pages of
  those 30 minutes.
- Accepted ingest events are published in one-second batches to the broadcast topic
  `analytics.live`; every API replica counts all of them in memory, so the stream covers every
  replica without reading the database. Without a message broker the stream answers `503
  analytics.realtime_unavailable`.

### Changed

- The notification stream and the live analytics stream share one broadcast connection to the
  broker.

### Security

- New permission `analytics.realtime.read`, seeded for admins and editors (rerun `app seed`).
  Live frames carry the path, country, and device of page views and the normalised query of
  searches; no visitor, session, or subject id reaches the client.

## 2026-09-25 — Analytics dashboard reports

### Added

- `GET /api/v1/admin/analytics/overview`, `/pages`, `/navigation`, `/retention`, and `/search`,
  read from rollups only. They share `from`/`to` (inclusive dates in `site.timezone`, default
  the last 30 days, at most 731), `grain` (picked from the range when omitted), `compare`
  (`previous` by default, or `none`), and `limit`; pages also take `sort=views|time|rising`.
- Reports cover whole periods. Each series point has exact visitors; range totals are sums of
  per-period uniques labelled `visitor_days`, `visitor_weeks`, or `visitor_months`, with plain
  `visitors` only for a one-period window. Rates are `null` without a denominator.

### Security

- New permissions `analytics.read` (overview, pages, navigation, retention) and
  `analytics.search.read` (search), seeded for admins and editors. Rerun `app seed` on existing
  databases. Export stays admin-only.

## 2026-09-25 — Analytics aggregation pipeline

### Added

- Migration 00033: daily, weekly (ISO), and monthly analytics rollups bucketed in the
  `site.timezone` setting, covering the site, pages, referrer hosts, country/device/browser,
  navigation, searches, search positions, and clicked results, plus retention cohorts of
  consented visitors (day 1, 7, and 30). Unique visitors are exact per grain. Paths, referrers,
  transitions, and queries keep the top 1000 per period and fold the rest into `(other)`.
- The `analytics-rollup` job in `app scheduler` (every 15 minutes) recomputes the periods
  containing today or yesterday and the last 31 days of cohorts. It rebuilds everything the raw
  events cover on its first run or after `site.timezone` changes.
- `app analytics rollup --from YYYY-MM-DD [--to YYYY-MM-DD]` recomputes a range of days.

### Security

- Rollups hold counts only. The first-seen record of each consented visitor, kept for new versus
  returning counts, is deleted with the account on erasure.

## 2026-09-25 — Analytics telemetry ingest

### Added

- The five `/api/v1/analytics/ingest/*` routes (page view, time spent, navigation, search,
  search click) store raw events and answer `202 {"id": ...}`. The search id is the `search_id`
  for clicks, and the page view id is the `view_id` for time-spent heartbeats.
- Migration 00032: `analytics_page_views`, `analytics_time_spent`, `analytics_navigation`,
  `analytics_searches`, `analytics_search_clicks`. Events are deduplicated by client id; time
  spent keeps one row per page view with the highest focus time.
- Events are written by a background batch writer from a bounded queue
  (`ANALYTICS_QUEUE_SIZE`, default 10000). Losses are counted in
  `blog_analytics_events_dropped_total`.
- Ingest is limited to `ANALYTICS_INGEST_PER_MINUTE` requests per client IP (default 300) and
  8 KiB per body (`413 analytics.payload_too_large`).
- `ANALYTICS_COUNTRY_HEADER` records the country from an edge proxy header, only from
  `APP_TRUSTED_PROXIES`.

### Changed

- With `analytics.consent_required` off, events without granted consent are stored with no
  subject link, and events from a subject that rejected or withdrew analytics are dropped.
- Account erasure also deletes the raw analytics events linked to the account's consent subjects.

### Security

- No IP address or user agent is stored. The visitor hash is an HMAC of the consent subject, or
  for anyone else of the day, IP, and user agent, so anonymous visits cannot be linked across
  days. Paths lose their query string and referrers keep only scheme, host, and path.
- Bots, clients without a user agent, and prefetches are dropped. Ingest answers the same way
  whether or not an event is kept.

## 2026-09-25 — Phase 5 security review: impersonation, privacy, and tokens

### Added

- Impersonation is bound to the staff member's own sign-in. Ordinary access tokens carry a `fam`
  claim (their refresh-session family, kept across rotation); `start` records it (migration
  00031, `impersonation_sessions.base_family_id`), and the impersonation token stops working once
  that sign-in is logged out, revoked, or expired (`end_reason: parent_session_expired`). A token
  without `fam`, such as one issued before this release, gets
  `401 impersonation.sign_in_required` from `start` until it is refreshed.
- Every request made with an impersonation token is audited, reads included. Refused actions are
  recorded as failures with `failure_reason: impersonation.forbidden_action`, and refused starts
  with the reason (`step_up_required`, `target_ineligible`, …).
- Responses to impersonated requests carry `X-Impersonation-Session`, and an open notification
  stream sends `stream.closed` (`code: impersonation_ended`) and closes when the session ends.
- Account erasure also erases the newsletter subscriber linked to the account, in the same
  transaction, with a consent-history entry (source `privacy_request`) and a
  `blog.newsletter.subscriber.changed` event for provider sync.
- Authorization tests for every Phase 5 admin operation, and a test that the seeded role grants
  match the handlers' fallback roles.
- Privacy stance in [overview.md](docs/security/overview.md#privacy-stance).

### Changed

- Impersonation tokens can no longer give or withdraw analytics consent or change newsletter
  subscriptions (`me.newsletter.subscribe`, `me.newsletter.unsubscribe`,
  `analytics.consent.store`, `analytics.consent.withdraw`).
- Stored client-IP hashes (comments, guest flags, newsletter consent evidence) are HMAC-SHA256
  under a key derived from `APP_ENCRYPTION_KEY`. Without the key they stay plain SHA-256 and
  startup warns. Existing rows keep their old hashes.
- Analytics consent responses are `Cache-Control: no-store`.
- A session that expired or lost its base sign-in without being closed no longer blocks starting
  a new one.

## 2026-09-25 — Media variants, orphan cleanup, and storage usage

### Added

- Site settings `media.variants` (named `name:width[:format]` presets, default thumbnail 320,
  card 640, and hero 1280 in WebP) and `media.default_transform_format`. When imgproxy is
  configured, ready raster assets on admin media, `public.media.get`, and post media responses
  carry a `variants` map of signed URLs. Nothing is stored; the CDN caches the renders.
- Transform URLs now carry the `media.default_transform_quality` setting.
- `GET /api/v1/admin/media/usage` (`media.usage.read`, admin and editor): asset counts and
  bytes by status, content type, and top uploaders, optionally for one `user_id`.
- `GET /api/v1/admin/media?unused=true` lists ready assets that no avatar, category, post
  cover, post media row, or SEO image references. They are never deleted automatically.
- Hourly scheduler job `media-orphans` deletes uploads never completed 24 hours after their
  presign expired, and soft-deleted assets after `MEDIA_PURGE_AFTER` (default 30 days, `0`
  keeps them), object first and row second.

## 2026-09-25 — Newsletter subscriptions

### Added

- Public double opt-in: `POST /api/v1/newsletter/subscribe`, `/confirm`, `/confirm/resend`,
  `/unsubscribe` (also the RFC 8058 one-click target), and `GET|PATCH
  /api/v1/newsletter/preferences/{token}`. Subscribe never reveals whether an address is known;
  it checks a honeypot and, when configured, Turnstile, and is rate limited per IP and per
  address. See [newsletter.md](docs/backend/newsletter.md).
- `GET /api/v1/me/newsletter/subscriptions`, `POST /api/v1/me/newsletter/subscribe`, and
  `POST /api/v1/me/newsletter/unsubscribe` for the account email.
- Admin subscribers (list, search, CSV export, get with consent history, unsubscribe or
  `hard_delete` erasure), issues (Markdown drafts, scheduling, send, cancel, preview), and
  provider config (sender identity, postal address, confirm TTL, double opt-in, and lists).
- Issues are sent by the `newsletter-dispatch` consumer in `app worker`, one message per
  recipient with unsubscribe and preferences links, the postal address, and `List-Unsubscribe`
  headers. Each recipient is claimed once, so retries never send twice.
- `NEWSLETTER_PROVIDER=smtp|custom_http`: the SMTP mailer, or HMAC-signed JSON deliveries and
  contact syncs to `NEWSLETTER_HTTP_ENDPOINT`. `NEWSLETTER_HTTP_SECRET` also enables the signed
  bounce and complaint webhook `POST /api/v1/newsletter/webhooks/provider`.
  `NEWSLETTER_SEND_BATCH` sets the dispatch batch size.
- Migration 00030 with lists, subscribers, memberships, hashed tokens, consent audit, issues,
  and deliveries. Erasure keeps the suppressed row and consent history without the address.
- Events `blog.newsletter.issue.send_requested` and `blog.newsletter.subscriber.changed`;
  scheduler jobs `newsletter-release` (every minute) and `newsletter-tokens-prune` (hourly).
- `newsletter.confirm` and `newsletter.welcome` notification templates.
- Eight `newsletter.*` permissions: all on `admin`; subscriber read/export and issue
  read/edit/send on `editor`.

## 2026-09-25 — Staff impersonation

### Added

- `POST /api/v1/admin/impersonation/start`, `POST /api/v1/admin/impersonation/stop`, and
  `GET /api/v1/admin/impersonation/current`. Start needs `impersonation.start`, a reason, the
  current password, and a TOTP or backup code when 2FA is enabled, and returns a Bearer token with
  `sub` = target and an RFC 8693 `act` claim for the staff member (no refresh token). Targets are
  active users whose permissions are a subset of the caller's, never admins or other
  impersonators. See [impersonation.md](docs/backend/impersonation.md).
- `impersonation_sessions` table (migration 00029); one active session per staff member.
- Config `IMPERSONATION_TTL` (default `1h`, `5m`–`2h`): a hard ceiling, never renewed.
- Impersonation tokens are checked against their session on every request
  (`401 auth.impersonation_ended` once stopped, expired, or revoked) and refused on credential,
  2FA, OAuth, privacy, export, erase, logout, refresh, and chained impersonation operations
  (`403 impersonation.forbidden_action`).
- `audit_logs.impersonator_id` is filled for every action taken with an impersonation token;
  admin activity returns `impersonator_id` and `/me/activity` shows `impersonated`.
- Events `blog.impersonation.started`, `exited_manually`, `expired`, and `revoked_by_policy`;
  scheduler job `impersonation-expire` (every minute).
- `impersonation.start` permission, seeded on the `admin` role.

## 2026-09-25 — Post search

### Added

- `GET /api/v1/posts?q=…` searches published posts with PostgreSQL full-text search (web search
  syntax, best match first) and returns `search.rank`, `search.title`, and `search.snippet` with
  matches in `<mark>`. See [search.md](docs/backend/search.md).
- `posts.search_vector` generated column with a GIN index (migration 00028): title, excerpt, and
  the first 100,000 characters of content, weighted in that order.
- Config `SEARCH_LANGUAGE` (default `simple`) and `app search reindex` to rebuild the index after
  changing it.

### Changed

- Post reads list their columns explicitly instead of `posts.*`, so they never load the search
  vector.

## 2026-09-25 — Data export and erasure

### Added

- `GET /api/v1/me/activity/export` queues a JSON export of the caller's data and, once ready,
  returns a short-lived presigned download link. `POST /api/v1/me/activity/erase` (with
  `current_password`) queues the anonymization of the account. See
  [api.md](docs/backend/api.md#profiles-and-email-change).
- `privacy_requests` table (migration 00027) and a `privacy-requests` job in `app scheduler`
  that runs queued requests every minute and deletes expired export archives.
- Config `PRIVACY_EXPORT_RETENTION` (default `72h`) and `PRIVACY_EXPORT_URL_TTL` (default `15m`).
- Events `user.activity.export_requested` and `user.activity.erasure_requested`.

### Changed

- Comments by an erased account show `Deleted user` as the author username.

## 2026-09-25 — Privacy settings

### Added

- `GET /api/v1/me/privacy` and `PUT /api/v1/me/privacy` read and update profile visibility,
  email and contact visibility, and search indexing. See
  [api.md](docs/backend/api.md#profiles-and-email-change).
- Narrowing profile visibility requires `current_password`
  (`403 privacy.level_change_requires_reauth` otherwise). Rate limited to 10 updates per minute.
- Event `user.privacy.updated` with before/after values.

## 2026-09-25 — Analytics consent

### Added

- Pseudonymous analytics consent: `consent_subjects` (only the SHA-256 hash of the consent token is
  stored) and `analytics_consents` (purpose, status, policy version, decision and withdrawal time).
  See [analytics.md](docs/backend/analytics.md#implementation).
- `POST /api/v1/analytics/consent` stores decisions for `analytics` and `authenticated_analytics`.
  The first call issues a consent token (`201`), which clients send back in `X-Consent-Token`.
  Rate limited to 20 requests per minute.
- `GET /api/v1/analytics/consent` returns the current decisions for the token.
- `DELETE /api/v1/analytics/consent/{id}` withdraws a consent; withdrawing `analytics` also
  withdraws `authenticated_analytics` and unlinks the user.
- Consent is enforced at the ingest boundary: `/api/v1/analytics/ingest/*` returns
  `404 analytics.disabled` when `analytics.enabled` is off and `403 analytics.consent_required`
  when consent is required but not granted.
- Events `analytics.consent.granted`, `analytics.consent.rejected`, and `analytics.consent.withdrawn`.

## 2026-09-25 — Post SEO

### Added

- Per-post SEO: search title, description, and keywords; Open Graph and Twitter card overrides;
  canonical URL; and noindex/nofollow. Stored in the new `post_seo` table. See
  [post-seo.md](docs/backend/post-seo.md#implementation).
- `GET` and `PUT /api/v1/admin/posts/{id}/seo`:
  - `PUT` is a partial update that also renames the post when `slug` changes (requires `post.slug.edit`);
  - invalid fields are all reported at once as `422` with per-field codes;
  - text containing markup is rejected.
- `POST /api/v1/admin/posts/{id}/seo/preview` renders the search, Open Graph, and Twitter previews
  for unsaved changes.
- `GET /api/v1/posts/{slug}/seo-meta` returns rendered meta for published posts:
  - every value falls back to one derived from the post (title, excerpt, content, cover) and then
    to the site settings;
  - `X-Robots-Tag` is sent for noindex/nofollow posts.
- Settings `seo.default_twitter_card` and `seo.canonical_allowed_hosts`.
- Events `blog.post.seo.updated` and `blog.post.slug_changed`.
- Permissions `post.seo.view`, `post.seo.edit`, and `post.slug.edit`, seeded for admin, editor, and author.

### Changed

- Post revisions now snapshot SEO and diff it per field. Restoring a revision restores its SEO,
  skipping images that no longer exist.

## 2026-09-25 — Post versioning

### Added

- Post revision history. Every post write records a full snapshot in the same transaction:
  - the write types are create, update, media replace, publish, unpublish, archive, delete, and
    undelete;
  - each snapshot holds content, meta, tags, and media, plus a per-field diff against the
    previous revision and a generated changelog.
  See [post-versions.md](docs/backend/post-versions.md#implementation).
- The following endpoints replace the `501` stubs:
  - `GET /api/v1/admin/posts/{id}/revisions`, with filters `author_id`, `from_date`,
    `to_date`, and `include_diff`;
  - `GET /api/v1/admin/posts/{id}/revisions/{revision UUID or number}`;
  - `POST /api/v1/admin/posts/{id}/revisions/{revision}/restore`.
- Restore copies content, meta, tags, and media back as a new `restore` revision and keeps the
  post's status. Missing categories, tags, and media, or a slug now held by another post, are
  skipped and reported.
- Permissions:
  - `post.revisions.view` and `post.revisions.restore` for admin, editor, and author;
  - `post.revisions.view_all` for admin and editor. Authors only reach their own posts' history.
- `blog.post.revision.created` and `blog.post.revision.restored` outbox events.
- `app revisions prune --keep N` keeps the newest N revisions of every post.
- Migration `00024_post_revisions.sql`.

### Changed

- The AsyncAPI `PostRevisionCreated` type enum adds `unpublish`, `delete`, and `undelete`.
  `author_id` (and `actor_id` on `PostRevisionRestored`) is nullable, for writes with no user.
- Post delete, trash restore, and media replace now run in one transaction with their revision.

### Upgrade notes

- Run `app migrate up` and `app seed` to add the revision permissions. Existing posts get
  revision 1 on their next write.

## 2026-09-25 — Admin settings

### Added

- `GET /api/v1/admin/settings`, `PUT /api/v1/admin/settings`, and
  `GET /api/v1/admin/settings/history` (permissions `settings.read`, `settings.update`,
  `settings.history.read`), replacing the `501` stubs. See
  [settings.md](docs/backend/settings.md#implementation).
- A typed settings catalogue in code (site, media, analytics, notifications, SEO keys) with
  strict validation: no type coercion, ranges, enums, patterns, and URL and time zone checks.
  Any invalid key rejects the whole update with `422` and per-key violations.
- Optimistic locking: optional per-key `version` on updates; stale versions and concurrent
  writes return `409 settings.version_conflict`.
- Migration `00023_settings.sql`: `settings` (changed values only) and `settings_history`.
- `blog.settings.updated` outbox event per applied update.
- `settings` read-cache family and `CACHE_TTL_SETTINGS` (default `10m`).
- Rejected settings updates by signed-in admins are audited with the submitted keys and
  violations.
- Phase 5 design decisions recorded in
  [phase-5-product-expansion.md](docs/tasks/phase-5-product-expansion.md#decisions).

### Upgrade notes

- Run `app seed` to add the `settings.history.read` permission row. Admins already pass
  through the `*` grant.

## 2026-09-25 — Event pipeline secrets review

### Added

- Kafka TLS and SASL: `KAFKA_TLS`, `KAFKA_TLS_CA_PATH`, `KAFKA_SASL_MECHANISM` (`PLAIN`,
  `SCRAM-SHA-256`, `SCRAM-SHA-512`), `KAFKA_SASL_USERNAME`, `KAFKA_SASL_PASSWORD`. Production
  refuses SASL without TLS.
- Per-process secrets table and event-pipeline notes in
  [secrets-and-headers.md](docs/security/secrets-and-headers.md).

### Changed

- `app worker`, `scheduler`, `migrate`, `outbox`, and `audit` load config without the JWT
  keys (`config.LoadBackground`), so the signing key no longer has to be deployed with them.
- In production `IMGPROXY_KEY` must decode to at least 32 bytes and `IMGPROXY_SALT` to 16;
  both must be hex everywhere.

### Fixed

- The email consumer no longer copies SMTP errors, which can echo the recipient address, into
  its error and so into plaintext dead-letter headers. The detail is logged instead.

## 2026-09-25 — AsyncAPI validation in CI

### Added

- `AsyncAPI` workflow and `make asyncapi-validate` validate
  `docs/architecture/asyncapi.yaml` with `@asyncapi/cli` 6.2.0; schema errors fail.
- `TestEventTypesHaveAsyncAPIChannels` fails when an event type has no channel in the spec.

### Fixed

- `asyncapi.yaml` was not valid AsyncAPI 2.6 (131 errors): channel-level `message` moved into
  the `publish`/`subscribe` operations, channel `tags` moved onto operations, `info.summary`
  folded into the description, the payload removed from the `EventEnvelope` trait, HTTP
  bearer scopes and parameter `in`/`required` dropped, a duplicate `operationId` renamed, and
  the missing `SettingsUpdated` schema added as `SettingsUpdatedPayload`.

## 2026-09-25 — Broker integration tests

### Added

- Round-trip and dead-letter tests against real Kafka and RabbitMQ, skipped unless
  `TEST_KAFKA_BROKERS` / `TEST_RABBITMQ_URL` are set. `make test-brokers` runs them against
  Compose; CI runs both brokers as service containers.

### Fixed

- A new Kafka consumer group starts from the oldest offset instead of the newest, so a first
  worker deploy no longer skips events published before its consumers joined.
- `app worker` starts the outbox relay only after its consumers subscribe, so RabbitMQ does
  not drop the first commands on a fresh broker.

## 2026-09-25 — Event pipeline hardening

### Added

- `app worker` serves `GET /healthz` and `GET /readyz` on `METRICS_ADDR`; readiness fails
  while the database or the broker is down.
- Consumer circuit breaker: after `CONSUMER_BREAKER_FAILURES` consecutive transient failures
  a consumer pauses for `CONSUMER_BREAKER_TIMEOUT`, nacking messages for redelivery instead
  of dead-lettering them.
- `CONSUMER_CONCURRENCY` runs several handler copies per consumer in one worker.
- `HTTP_MAX_INFLIGHT` caps concurrent API requests per process; extra requests get
  `503 server.overloaded` with `Retry-After: 1`. Off by default.
- [Runbook](docs/deployment/runbook.md) and process-role deployment notes
  ([deployment.md](docs/architecture/deployment.md#process-roles)).

### Changed

- Readiness dependencies report `critical`. The broker is non-critical for `app serve`, so a
  broker outage no longer fails API readiness; events wait in the outbox.

## 2026-09-25 — Image transforms via imgproxy

### Added

- `public.media.transform` (`GET /api/v1/media/{id}/transform?w=256&format=webp`) redirects to
  a signed, expiring imgproxy URL. Widths come from `MEDIA_TRANSFORM_WIDTHS`; formats are
  webp, avif, jpeg, and png; images are never enlarged; SVG is not transformed.
- Config: `IMGPROXY_URL`, `IMGPROXY_KEY`, `IMGPROXY_SALT`, `MEDIA_TRANSFORM_WIDTHS`,
  `MEDIA_TRANSFORM_URL_TTL`. Without `IMGPROXY_URL` the endpoint answers
  `501 media.transform_disabled`.
- `imgproxy` service in `compose.yaml` under the `imgproxy` profile, reading originals from
  RustFS. See [media.md](docs/backend/media.md#image-transforms-imgproxy).

## 2026-09-25 — Scheduled jobs

### Added

- `app scheduler` runs recurring jobs: `audit-prune`, `processed-messages-prune`, and
  `auth-tokens-prune` (refresh sessions 30 days past expiry, reset tokens 7 days past
  expiry). Each job runs once per interval across replicas, guarded by a PostgreSQL advisory
  lock and `scheduled_job_runs` (migration 00022). See [jobs.md](docs/backend/jobs.md#scheduled-jobs).
- `app scheduler status` and `app scheduler run <job>`.

### Changed

- Audit pruning moved from `app worker` to `app scheduler`; run the scheduler to keep
  `AUDIT_RETENTION_DAYS` enforced.

## 2026-09-25 — Worker consumers and email dispatch

### Added

- Worker consumers retry with exponential backoff (`CONSUMER_MAX_RETRIES`,
  `CONSUMER_RETRY_INTERVAL`, `CONSUMER_RETRY_MAX_INTERVAL`), recover panics, carry the
  originating `request_id` in logs, and move messages that still fail to `blog.dead_letter`.
- `processed_messages` table (migration 00021): consumers skip redelivered messages. Rows are
  pruned after `CONSUMER_DEDUPE_RETENTION`.
- Email dispatch: with `MESSAGE_BROKER` and `APP_ENCRYPTION_KEY` set, the API stores each
  email as an encrypted `notification.email.requested` command and `app worker` sends it.
  The worker needs the same key and `SMTP_*` settings. See
  [events.md](docs/backend/events.md#email-dispatch).

### Changed

- The worker no longer subscribes to `worker.heartbeat`.
- Without `APP_ENCRYPTION_KEY`, or when a command cannot be stored, emails are still sent
  inline by the API.

## 2026-09-25 — Domain events

### Added

- Services record domain events in the same transaction as the write (when `MESSAGE_BROKER`
  is set): `blog.post.created`, `blog.post.updated`, `blog.post.published`,
  `blog.post.archived`, `blog.comment.created`, `blog.comment.moderated`, `blog.user.created`,
  `auth.password.reset_requested`, `blog.media.uploaded`, and `blog.media.deleted`. See
  [events.md](docs/backend/events.md#emitted-today).

### Changed

- Post, comment, account, and media writes that touch several rows (for example media delete
  clearing references, or a post and its tags) now commit or roll back together.
- Nested transactions use savepoints, so a slug conflict retried inside a transaction works.
- AsyncAPI: `blog.comment.approved` is replaced by `blog.comment.moderated`; the comment,
  user, post, and password-reset payloads match what is emitted (no email addresses).

## 2026-09-25 — Transactional outbox and relay

### Added

- `internal/core/event`: domain events (`event.New`) with `Recorder` and `Transactor` ports, so
  services record events in the same transaction as their writes without importing Watermill.
- `persistence.Transactor` carries the transaction in the context; every repository joins it.
- `persistence.OutboxRepository` stores events in `outbox_events` with envelope headers (`id`,
  `type`, `schema_version`, `source`, `timestamp`, aggregate, `actor_id`, `correlation_id`).
- `app worker` relays due rows to the broker (`FOR UPDATE SKIP LOCKED`, safe with several
  workers), retries with exponential backoff, parks rows after `OUTBOX_MAX_ATTEMPTS`, and
  deletes published rows after `OUTBOX_RETENTION`. It serves `blog_outbox_*` metrics on
  `METRICS_ADDR`.
- `app outbox status` and `app outbox retry`.
- Migration `00020_outbox_relay.sql` (`next_attempt_at`, `failed_at`).
- `OUTBOX_BATCH_SIZE`, `OUTBOX_POLL_INTERVAL`, `OUTBOX_MAX_ATTEMPTS`, `OUTBOX_RETENTION`.

## 2026-09-25 — Comment abuse controls documented; phase 3 done

### Changed

- `docs/security/overview.md` gains an "Abuse and spam" section covering rate limits, post
  policies, the guest gate, Turnstile, the honeypot, content limits, community flags,
  moderation, IP hashing, and the known gaps.
- Phase 3 (collaboration and moderation) is marked done.

## 2026-09-25 — Comment repository tests

### Added

- PostgreSQL tests for the comment repository: one flag per user or guest identity and the
  flag threshold (approved comments only), upvote toggling and counts, reply counts that
  include only approved replies and deleted placeholders, a moderation batch rolled back
  when one comment has changed status, and hard delete (a leaf is removed but its log entry
  stays; a parent with replies is scrubbed and its flags and upvotes are cleared).

## 2026-09-25 — Live notification stream

### Added

- `GET /api/v1/me/notifications/stream` (server-sent events): `retry` and `stream.opened` on
  connect, `notification.created` for each new notice, `ping` every `SSE_PING_INTERVAL`
  (default `15s`), `error` when a slow client dropped events, and `stream.closed` on shutdown.
  `SSE_MAX_CONCURRENT_PER_USER` (default 3) streams per user per process; the next gets `429`.
- Notices reach streams on every API replica through `MESSAGE_BROKER`: each process
  publishes to `notifications.created` and reads it through its own subscription
  (`messaging.OpenBroadcast`). Without a broker the stream returns
  `503 notifications.stream_unavailable`; the inbox endpoints are unaffected.

## 2026-09-25 — Notification inbox

### Added

- In-app notifications (migration `00019_notifications.sql`). A visible reply notifies the
  parent comment's author, a moderation decision with `notify_author` notifies the comment's
  author, and publishing a post notifies its author (when someone else publishes) and the
  users who commented on it. Notices are best effort and deduplicated per user.
- `GET /api/v1/me/notifications` (`unread`, `page`, `per_page`; unread total in
  `X-Unread-Count`) and `POST /api/v1/me/notifications/{id}/read`.
- Notification template types `comment.reply`, `comment.moderated`, and
  `publication.republished` (web and SSE only), and the `{{.Excerpt}}` and `{{.Outcome}}`
  variables.

## 2026-09-25 — Guest comment captcha

### Added

- Cloudflare Turnstile on guest comments. With `TURNSTILE_SECRET_KEY` set, guests must send
  `turnstile_response`; a missing or rejected token returns `400 comments.spam.challenge_invalid`
  and an unreachable provider `503 comments.spam.challenge_unavailable`. Signed-in users and
  honeypot hits skip the check.

## 2026-09-25 — Per-post comment policy

### Added

- Posts have a `comment_policy` (migration `00018_post_comment_policy.sql`), returned on post
  responses and set with `PATCH /api/v1/admin/posts/{id}`: `open` (default), `authenticated`
  (guests get `401`), `read_only` (comments shown; create, edit, and upvote return
  `403 comment.closed`), or `disabled` (list and get also return `403 comment.disabled`).
  Flagging stays available on read-only posts, and authors can always delete their own comments.

### Changed

- Editing or upvoting a comment now requires its post to be published, like reading and
  creating already did.

## 2026-09-25 — Sanitized comment HTML

### Added

- Comments now return `content_html` next to `content`: the markdown is rendered with goldmark
  and sanitized with bluemonday to paragraphs, bold, italic, code, blockquotes, lists, and
  `http`/`https`/`mailto` links (`rel="nofollow noreferrer"`). Raw HTML, images, and headings are
  never emitted. It is rendered on create and edit and stored in `comments.content_html`
  (migration `00017_comment_content_html.sql`); older rows are rendered on read. Public
  responses blank it for deleted comments, and a moderator hard delete clears it.

## 2026-09-25 — Audit logging and account activity

### Added

- Audit log. Every successful admin and self-service change, sign-in and sign-out, password
  reset, and signed-in public write (comments) is recorded with the actor, the resource, IP, user
  agent, and request id. Rejected sign-ins (wrong password or code, lockout) are recorded too,
  attributed to the account when it exists
- Before/after changes for role assignment, post status, comment moderation, and marketing
  consent. Profile edits record which fields changed, not their values
- `GET /api/v1/me/activity`: the caller's account activity, filterable by category and date.
  IP addresses are reduced to their /24 or /48 network and user agents to "browser on OS"
- `GET /api/v1/admin/users/{id}/activity`: every entry by or about a user, with full detail.
  Requires the new `user.activity.read_all` permission (seeded for `admin`)
- `app audit prune [--older-than-days N]` and an hourly prune in `app worker`
- Config `AUDIT_RETENTION_DAYS` (default 395) and `AUDIT_QUEUE_SIZE` (default 1024)
- Metric `blog_audit_entries_dropped_total`
- Migration `00016_audit_log_columns.sql`: `category`, `result`, `ip_address`, `user_agent`,
  `request_id`, and indexes for activity lookups

### Changed

- Audit entries are written from a bounded in-memory queue by a background batch writer, so
  auditing never delays or fails a request. When the queue is full or the insert fails, entries
  are logged and dropped; shutdown flushes the queue before closing the database

## 2026-09-25 — Auth threat model

### Added

- Threat model for authentication in `docs/security/authn-authz.md`: assets, trust boundaries,
  threats with their mitigations and residual risk, and open items

### Security

- A new forgot-password request revokes the account's older reset links, and completing a reset
  spends every pending link. Before, only admin-initiated resets revoked older links

## 2026-09-25 — Auth security checklist run

### Security

- Login no longer reveals account state. An unknown email still pays for a password hash, and
  an inactive account answers like a wrong password unless the password is correct
- Forgot-password emails are delivered in the background, so response time does not reveal
  whether the account exists
- `POST /auth/refresh`, `POST /auth/password/forgot`, `GET /auth/password/reset/{token}` and
  `POST /auth/password/reset` are now rate limited per IP (`AUTH_LOGIN_PER_MINUTE`)
- Access tokens must carry `exp` and the configured issuer

### Added

- The Swagger parity test also checks that each operation's `security` matches the route's
  auth mode

### Fixed

- `GET /api/v1/admin/categories` is documented as an admin operation with bearer auth

## 2026-09-25 — Auth cycle integration tests

### Added

- End-to-end tests of login, refresh, logout, and expired, reused and unknown refresh tokens
  through the HTTP router, against PostgreSQL with ES256 tokens and Argon2id hashing

### Fixed

- An expired refresh token answered `400 auth.password.reset_token_expired`. It now answers
  `401 unauthorized` like other dead refresh tokens (new `ErrSessionExpired`)
- Refreshing a 7-day (non-"remember me") session re-issued it for `APP_REFRESH_TOKEN_TTL`
  (30 days by default). Rotation now keeps the lifetime the session was issued with, and
  `APP_REFRESH_TOKEN_TTL` caps the 7-day lifetime

## 2026-09-25 — Route and Swagger parity test

### Added

- `routes/swagger_parity_test.go` checks that every implemented route has an operation in
  `docs/swagger.json` and that every documented operation is mounted. It runs with
  `make routes-check`

### Fixed

- `GET /api/v1/admin/categories` was missing from the Swagger spec
- The OAuth operations used `{provider}` in their Swagger paths; they now use `{param1}` like
  the mounted routes

## 2026-09-25 — Google and GitHub OAuth sign-in

### Added

- `GET /api/v1/auth/oauth/{provider}/start?redirect_uri=` returns the provider authorization
  URL. The server stores a single-use state with an S256 PKCE verifier in Redis for 10 minutes
- `POST /api/v1/auth/oauth/{provider}/callback` with `{code, state}` answers like login: tokens
  or a two-factor challenge. It signs in the account linked to the provider identity, or the
  existing account whose email the provider verified, and then saves the link (migration
  `00015_oauth_identities.sql`). Accounts are never created
- Configuration: `OAUTH_GOOGLE_CLIENT_ID`/`_SECRET`, `OAUTH_GITHUB_CLIENT_ID`/`_SECRET` and the
  exact-match `OAUTH_REDIRECT_URIS` allowlist (https required in production)

## 2026-09-25 — Admin login

### Added

- `POST /api/v1/admin/auth/login`: the regular login (lockout, rate limit, two-factor
  challenge) restricted to accounts holding the new `admin.access` permission. The seeder
  grants it to `admin`, `editor`, `author` and `moderator`; re-run `app seed` on existing
  databases. Any other account gets the same `401` as a wrong password

## 2026-09-25 — TOTP two-factor authentication

### Added

- TOTP 2FA (RFC 6238, ±1 step). Enrollment is at `GET|DELETE /api/v1/me/2fa`,
  `POST /api/v1/me/2fa/setup`, `POST /api/v1/me/2fa/confirm` (returns 10 single-use backup
  codes) and `POST /api/v1/me/2fa/backup-codes`
- For enrolled accounts, `POST /api/v1/auth/login` returns `two_factor_required` and a
  5-minute challenge token instead of tokens. `POST /api/v1/auth/2fa/challenge` exchanges it,
  with a TOTP or backup code, for the token pair. The challenge is rate limited, allows
  5 attempts and is single use
- TOTP secrets are encrypted with AES-256-GCM under the new `APP_ENCRYPTION_KEY`, and backup
  codes are stored as keyed HMACs (migration `00014_two_factor.sql`). Each time step is
  accepted only once, so codes cannot be replayed
- `AUTH_2FA_ISSUER` sets the authenticator app label

### Security

- Without `APP_ENCRYPTION_KEY`, enrollment is disabled and already-enrolled accounts fail
  closed at login (`503 auth.2fa.unavailable`) instead of skipping the second factor

## 2026-09-25 — Casbin policy reload without restart

### Added

- Role writes publish on Redis channel `rbac:policy:reload`; every instance reloads its Casbin
  policy from `casbin_rules` when a peer announces a change
- `RBAC_POLICY_RELOAD_INTERVAL` (default `30s`, `0` disables) reloads the policy on a timer,
  covering missed announcements and direct edits such as `app seed`

## 2026-09-25 — Role and permission administration

### Added

- `/api/v1/admin/roles` (list, get, create, update description, delete),
  `PUT /api/v1/admin/roles/{name}/permissions`, and `GET /api/v1/admin/permissions`,
  gated by `role.read` / `role.manage`
- `GET|POST /api/v1/admin/users/{id}/roles` and `DELETE /api/v1/admin/users/{id}/roles/{name}`
- The `admin` role is protected: it cannot be deleted or regranted, and an admin cannot revoke
  it from themselves; the `*` permission cannot be granted to other roles
- Role writes update the relational tables and `casbin_rules` in one transaction and reload the
  enforcer, so changes apply without a restart
- The seeder mirrors seeded grants into `role_permissions`

### Fixed

- `migrations.Up` takes a PostgreSQL advisory lock, so replicas or parallel test packages
  migrating the same database no longer race

## 2026-09-25 — Admin user create and admin password reset

### Added

- `POST /api/v1/admin/users` (`user.create`) creates an active account; sending `roles` also
  requires `role.manage`, and unknown roles are rejected before the account is created
- `POST /api/v1/admin/users/{id}/password/admin-reset` (`user.password.admin_reset`) emails a
  fresh reset link, invalidates earlier links, and revokes sessions unless `revoke_sessions` is false
- Seeded the admin role with `user.password.admin_reset`, `role.read`, and `role.manage`

### Changed

- The Casbin enforcer is now a `SyncedEnforcer`, safe for concurrent policy writes
- A duplicate username on insert maps to `409 user.username.taken`

## 2026-09-25 — Prometheus metrics

### Added

- `METRICS_ADDR` starts a separate listener serving Prometheus `GET /metrics` (Compose binds it to `127.0.0.1:9090`)
- Series: `blog_http_requests_total`, `blog_http_request_duration_seconds`, `blog_http_requests_in_flight` (labelled by route template), `blog_build_info`, `blog_db_*` pool stats, and Go/process collectors

## 2026-09-25 — Login throttling and lockout

### Security

- `POST /api/v1/auth/login` is rate limited per IP (`AUTH_LOGIN_PER_MINUTE`, default 10/min)
- After `AUTH_LOGIN_MAX_FAILURES` (default 5) failed logins for an email within `AUTH_LOGIN_LOCKOUT` (default 15m), that email is locked for the same period; unknown emails are tracked too so lockout does not reveal which accounts exist
- Locked logins return `429` `auth.login.locked` with `Retry-After`; a successful login clears the failure count

## 2026-09-25 — PostgreSQL only

### Removed

- MySQL and SQL Server support (`DB_DRIVER=mysql|sqlserver`, their GORM drivers, and Cloud SQL connectors); the migrations are PostgreSQL-specific, so those drivers could never run them

### Notes

- `DB_DRIVER` now accepts only `postgres` (aliases `pg`, `postgresql`); anything else fails at startup

## 2026-09-25 — Messaging readiness check

### Added

- `GET /health/ready` includes a `messaging` check when `MESSAGE_BROKER` is set; `messaging.Probe` dials the Kafka brokers, the RabbitMQ host, or the Pub/Sub endpoint without opening a publisher

## 2026-09-25 — Request ID in context

### Added

- `middleware.RequestID` stores the correlation id on the request `context.Context` (`logging.WithRequestID`)
- The application logger adds `request_id` to any record logged with that context, so core services and outbound adapters are correlated without passing the id around
- Redis read-cache warnings now log with the request context

## 2026-09-25 — Notification templates in the database

### Added

- Migration `00013_notification_templates.sql` adds `notification_templates`, one row per `(type, channel)` for `email`, `web`, and `sse`
- `app seed` inserts the built-in templates without overwriting edited rows
- Emails render from the stored template, falling back to the built-in copy when a row is missing

### Security

- Saved templates are validated; `{{.Token}}` is allowed only in email bodies and is blanked for every other field at render time

### Notes

- Run migrations and re-seed so the template rows exist

## 2026-09-25 — Mailpit and email notifications

### Added

- Mailpit in Compose (`1025` SMTP, `8025` web UI) for local email
- `NotificationService` sends password-reset, password-change, and email-change messages over SMTP
- `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, and `APP_PUBLIC_URL`

### Notes

- Leave `SMTP_HOST` empty to keep notices in the log
- Compose overrides `SMTP_HOST` to `mailpit`; the Mailpit UI is http://127.0.0.1:8025

## 2026-09-25 — ES256 access tokens and Argon2id passwords

### Changed

- Access tokens are signed and verified with ES256 (P-256 PEM) instead of RS256
- Passwords are hashed with Argon2id (t=3, m=64 MiB, p=2) instead of bcrypt
- Local-dev key paths are `configs/dev/jwt-es256-private.pem` and `configs/dev/jwt-es256-public.pem`

### Notes

- Existing RS256 access tokens are invalid after restart; clients must log in again
- Existing bcrypt password hashes no longer verify; re-seed or reset those passwords
- Regenerate local keys with `make dev-keys` if the ES256 PEMs are missing

## 2026-09-25 — Content core: post lifecycle, read cache, profiles

### Added

- `admin.posts.unpublish`, `admin.posts.archive`, `admin.posts.delete` (soft), `admin.posts.restore`; `admin.posts.list?trashed=true`
- Deterministic slug suffixes (`-2`, `-3`, …) on post create and restore; migration `00011_post_slug_live_unique.sql` makes slugs unique among live posts only
- Redis cache for public post, category, tag, and user-profile reads: generation-keyed invalidation, per-family TTLs (`CACHE_TTL_*`), `CACHE_ENABLED`, `CACHE_BYPASS_HEADER`, and a probe in `app doctor`
- `me.profile.patch`, `me.avatar.upload`, `me.avatar.delete`, `me.email.request_change`, `me.email.confirm_change`, `public.users.profile`, `admin.users.profile.get`, `admin.users.profile.patch`
- Migration `00012_user_profiles.sql` adds `user_profiles`, `user_privacy_settings`, and `password_reset_tokens.new_email`
- Server-side image upload for avatars: type from magic bytes, extension rewritten to match, header-only dimension check (max 8192 px), `AVATAR_MAX_BYTES`
- RBAC permissions `user.profile.read` and `user.profile.edit` (admin)
- Upload security review: [upload-security.md](docs/backend/upload-security.md)

### Security

- Presigned uploads are sniffed on `admin.media.complete`; content that contradicts the declared type is rejected
- Password-reset endpoints accept only password-reset tokens, so an email-change token cannot reset a password
- Confirming an email change revokes every session; the notifier never logs the token
- Rate limits: `media.presign` 60/min, `profile.update` 30/min, `profile.avatar` 10/min, `email.change.request` 3/hour, `email.change.confirm` 10/min

### Notes

- Run migrations and re-seed so the new permissions exist
- The `S3_BUCKET` bucket must exist before the first avatar upload
- `public.media.transform` stays `501` by decision; see [media.md](docs/backend/media.md#transform-decision-phase-2)
- Email change delivers through a logging notifier until the Phase 1 mailer lands

## 2026-09-24 — Comment moderation

### Added

- `admin.comments.list`, `admin.comments.get`, `admin.comments.stats`, `admin.comments.moderate`, `admin.comments.bulk_moderate`, `admin.comments.delete`
- Moderation state machine: `approve` / `reject` / `spam` / `restore`, with `409 comment.invalid_transition` for disallowed or concurrently changed comments
- Bulk moderation of up to 500 comments in one all-or-nothing transaction
- Hard delete removes the row, or scrubs content and author when the comment has replies
- Migration `00010_comment_moderation.sql` adds `moderated_by`, `moderation_reason`, `moderated_at`, and the append-only `comment_moderation_log`
- RBAC permissions `comment.moderate` (admin, editor, moderator) and `comment.delete` (admin, moderator)

### Notes

- Re-seed existing deployments so the new permissions exist; `00010` removes the unused `comments.moderate` policy

## 2026-08-02 — JWT RS256

### Changed

- Access tokens are signed and verified with RS256 (RSA PEM) instead of HS256
- `APP_SESSION_KEY` peppers opaque refresh/reset token hashes only
- JWT keys load from `APP_JWT_PRIVATE_KEY` / `APP_JWT_PUBLIC_KEY` or `*_PATH` file vars (path wins)

### Added

- Local-dev RSA keypair under `configs/dev/` for path-based configuration

## 2026-08-01 — Admin categories + nested set

### Added

- Hexagonal category admin module extensions: create, update, delete, move (reparent/reorder), and list in tree order
- `admin.categories.list`, `admin.categories.create`, `admin.categories.update`, `admin.categories.delete`, `admin.categories.move`
- Migration `00007_category_nested_set.sql` adds `lft`, `rgt`, `depth`, and `sort_order` columns
- RBAC permissions `category.read`, `category.create`, `category.update`, `category.delete` seeded for admin and editor roles
- Public category list/get expose nest metadata (`lft`, `rgt`, `depth`, `sort_order`, `image_id`); list uses envelope `{ items }`

### Notes

- After migrating, run `make migrate-up` (or restart with auto-migrate) so `CategoryService.RebuildAll` rewrites bounds from adjacency
- Re-seed or manually grant `category.*` permissions on existing deployments that skip bootstrap seed
- Delete returns `409 category_in_use` when posts reference the category or it has children
- Reparent/reorder via move only; PATCH rejects `parent_id`

### Updated

- Phase 2 content backlog marks admin category CRUD/reorder and nested-set tree as complete
- Admin categories API docs and design spec status set to implemented

## 2026-07-31 — Admin tags catalog + post attach

### Added

- Hexagonal tag module (`internal/core/tag`) with create, update, merge, delete, and resolve-or-create
- `admin.tags.create`, `admin.tags.update`, `admin.tags.merge`, `admin.tags.delete` for staff tag curation
- Post create/update accept `tags: string[]` (create-or-link by name; omit = unchanged, `[]` = clear)
- `public.tags.list` now routes through `TagService` instead of direct repository access

### Updated

- Phase 2 content backlog marks admin tag ops and post tag attach as complete
- Admin posts API docs note tag attach on create/update; design spec status set to implemented

## 2026-07-31 — Admin posts list + update

### Added

- `admin.posts.list` with status, author, category, and search filters plus envelope pagination
- `admin.posts.update` for partial post edits with ownership-aware access control and slug conflict handling

### Updated

- Phase 2 content backlog now marks admin post list and update as complete
- Admin posts API docs note the list and PATCH update endpoints

## 2026-07-31 — Media feature completion (list/delete/tags/get + post media)

### Added

- `admin.media.list`, `admin.media.delete`, `admin.media.tags.patch`, `public.media.get`
- `admin.posts.media.replace` with `post_media` join rows and `posts.cover_image_media_id` sync
- Migration `00006_media_relations.sql` (`post_media`, cover/avatar/category FKs)

### Notes

- `public.media.transform` remains deferred (501 stub)
- Soft delete clears FK references and `post_media` rows; object bytes are not removed from storage yet

## 2026-07-31 — Media upload MVP (presign + complete)

### Added

- Hexagonal media module (`internal/core/media`) with `PresignUpload` / `CompleteUpload`
- S3-compatible object storage adapter (`internal/adapters/outbound/storage`) for MinIO/S3/R2/Spaces
- `media_assets` migration (`00005_media_assets.sql`) and GORM repository
- `admin.media.create` returns a JSON presigned PUT URL; `admin.media.complete` finalizes after client upload
- Config: `S3_*`, `MEDIA_ALLOWED_MIME_TYPES`, `MEDIA_MAX_UPLOAD_BYTES`, `MEDIA_PRESIGN_TTL`

### Changed

- `admin.media.create` is no longer multipart through the API

## 2026-07-31 — Project configuration guide

### Added

- `docs/deployment/config.md` documents every environment variable consumed by the Go
  process, defaults, formats, production validation, database modes, messaging, and
  Compose-only/reserved variables

### Updated

- Deployment index, local setup, Docker guide, root README, and `.env.example` link or align
  with the configuration guide
- Local setup now sources `.env` explicitly because the Go process does not load dotenv files
- Redis application configuration is split into `REDIS_DRIVER`, `REDIS_HOST`, `REDIS_PORT`,
  `REDIS_PASSWORD`, and `REDIS_DB`; `REDIS_URL` is no longer consumed
- `REDIS_DRIVER` accepts `redis` or `valkey` (Valkey is protocol-compatible; connection URL
  scheme remains `redis://`)
- Direct database configuration is split into `DB_DRIVER`, `DB_HOST`, `DB_PORT`, `DB_USER`,
  `DB_PASSWORD`, `DB_NAME`, and `DB_SSLMODE`; `DATABASE_URL` is no longer consumed

## 2026-07-31 — CLI moved from `cmd/app` to `cmd`

### Changed

- `package main` now lives directly in `cmd/` (`main.go`, `command.go`, `worker.go`)
- Build uses an explicit output name so the binary stays `app`: `go build -o app ./cmd`
- `go run ./cmd <subcommand>` replaces `go run ./cmd/app <subcommand>` in Makefile, Dockerfile, CI, and docs

### Fixed

- `gofmt` alignment in `internal/platform/config/config.go` and `internal/platform/messaging/googlepubsub.go` that was failing `make lint`

## 2026-07-31 — Task backlog docs

### Added

- `docs/tasks/` — per-phase delivery backlog with checklists, status, dependency order, and cross-cutting work
- Index at [docs/tasks/README.md](docs/tasks/README.md) with status legend, current baseline, and maintenance rules
- Six phase files under [docs/tasks/](docs/tasks/README.md); every one of the 109 contract operation IDs is covered exactly once

### Updated

- Root README documentation list and the agent docs map link the backlog; `docs/product/roadmap.md` removed in favour of `docs/tasks/`

## 2026-07-30 — Multi-broker Watermill transports

### Added

- `internal/platform/messaging` factory for Kafka, RabbitMQ, and Google Cloud Pub/Sub (`MESSAGE_BROKER`)
- Config validation and `.env.example` vars: `KAFKA_BROKERS`, `KAFKA_CONSUMER_GROUP`, `RABBITMQ_URL`, `GOOGLE_PUBSUB_*`, `MESSAGE_TOPIC_PREFIX`
- Compose profile `messaging`: Kafka (`apache/kafka:3.9.0` KRaft) on `:9092`, RabbitMQ management on `:5672` / `:15672`
- `make infra-up-messaging` / `infra-down-messaging`; `app worker` opens the selected broker; `app doctor` pings messaging when configured

### Updated

- Architecture, deployment, local, tech-stack, and events docs describe selectable transports
- Spec [messaging brokers design](docs/superpowers/specs/2026-07-30-messaging-brokers-design.md) marked implemented

## 2026-07-30 — Multi-dialect DB + Cloud SQL connector

### Added

- `internal/platform/database` opens PostgreSQL, MySQL, or SQL Server via `DB_DRIVER`
- Google Cloud SQL path when `DB_INSTANCE_CONNECTION_NAME` is set (`cloud.google.com/go/cloudsqlconn` for all three engines)
- Config / `.env.example` for Cloud SQL IAM, private IP, credentials source, and pool knobs

### Updated

- Tech stack and database docs list MySQL / SQL Server alongside Postgres + Cloud SQL
- Bootstrap / CLI use the multi-dialect opener (`postgres` package kept as a thin shim)

## 2026-07-30 — Auth foundation + public posts + me/admin users

### Added

- Auth domain/service with login, refresh rotation, logout
- Password forgot (timing-safe) + reset token validity + reset consume + `me.password.update`
- JWT access tokens + hashed refresh sessions (`00002_auth_sessions.sql`)
- Password-reset token table (`00003_password_reset.sql`)
- Casbin RBAC enforcer with custom Postgres adapter (`00004_casbin_and_tags.sql`); seed writes role policies
- Bearer auth middleware for `AuthRequired` / `AuthOptional` routes
- `GET /api/v1/me`, `GET /api/v1/admin/users` (Casbin `user.read`)
- Public posts list/get; admin create draft + publish (Casbin `post.create` / `post.publish`)
- Public categories + tags list/get
- `app seed` for roles, permissions, Casbin policies, and initial admin
- Unit tests for auth service (including reset flow) and JWT helpers

### Updated

- Config: `APP_SESSION_KEY` (JWT signing), access/refresh TTLs
- Router wires Auth, Users, Posts, Categories, Tags, RBAC from bootstrap
- Local deploy docs include seed + login smoke
- Makefile `test`/`lint` scoped to `./cmd/... ./internal/...` (avoids Compose `data/` volume scan issues)

## 2026-07-29 — Post version history & SEO configuration

Added complete specifications for a post revision history system and per-post SEO configuration, integrated into the editor workflow, admin APIs, canonical event contracts, and backend database/service/validation layers.

### Added (feature-level docs)

- `docs/features/post-versioning.md`
  - Post revision history design: roles & permissions (`post.revisions.view`, `post.revisions.restore`, `post.revisions.view_all`), linear revision model (create/update/restore/publish/archive types), changelog per-field diff + auto-generated summary + optional editor note
  - Responsive editor UI sidebar: revisions panel with paginated list, per-revision view, side-by-side compare, restore confirmation modal, editor documentation sections

- `docs/features/post-seo.md`
  - SEO field design: `seo_title`, `seo_description`, `seo_keywords`, editable `slug`, OG fields (title/description/image/url), Twitter card (card type/title/description/image/creator), `canonical_url`, `robots_noindex/nofollow` toggles
  - Live SERP / Open Graph / Twitter card preview, responsive editor UI layout rules (desktop sidebar / tablet grid / mobile stacked), SSR rendering fallback chains and robots handling for drafts
  - Content-editor documentation outline: field meanings, recommended lengths, slug/canonical best practices, preview workflow

### Added (backend-level docs)

- `docs/backend/post-versions.md`
  - Hexagonal placement under `internal/core/post/revisions`
  - `post_revisions` storage: snapshot + diff + changelog + impersonator metadata, append-only semantics, restore creates new `type=restore` revision with `restore_from_revision_id`
  - Admin endpoints: list/get/restore revisions with filters, RBAC, audit rules, Watermill events

- `docs/backend/post-seo.md`
  - Hexagonal placement under `internal/core/post/seo`
  - `post_seo` storage (Option A separate one-to-one table, Option B embedded described), full validation rules (lengths, slug regex+uniqueness+reserved, canonical allowlist, image FK, Twitter creator handle, robots bools)
  - Endpoints: admin GET/PUT/preview SEO, public `/posts/{slug}/seo-meta`, rendering fallback chains, events, caching, integration with revision snapshots
  - New RBAC permissions: `post.seo.view`, `post.seo.edit`, `post.slug.edit`, `post.revisions.view`, `post.revisions.restore`, `post.revisions.view_all`

### Updated (central backend docs)

- `docs/backend/database.md`
  - Added `post_revisions` append-only table and `post_seo` one-to-one table with full field lists
  - Added indexes and FK semantics: CASCADE on post delete, SET NULL on `post_seo.og_image_id / twitter_image_id`, unique(post_id, revision_number), unique(post_id) on post_seo
  - Added relational mapping summary for posts ↔ revisions and posts ↔ SEO; append-only immutability note; slug change event note

- `docs/backend/api.md`
  - Added admin revisions + SEO endpoint list, public `/posts/{slug}/seo-meta` endpoint
  - Added Admin Post Revision Endpoint Rules and Admin Post SEO Endpoint Rules (RBAC, CSRF, soft warnings, restore semantics, slug event)

- `docs/backend/services.md`
  - Added `PostRevisionService` and `PostSEOService` to core services list
  - Added cross-cutting rules: `PostService` must create revisions inside same transaction; SEO updates + slug changes participate in same transaction + outbox

- `docs/backend/events.md`
  - Added events: `blog.post.revision.created`, `blog.post.revision.restored`, `blog.post.seo.updated`, `blog.post.slug_changed`
  - Added event consumers for revision cache invalidation, SEO cache invalidation, slug redirect/sitemap/internal-link rewrites, and SEO cache warmup

- `docs/backend/testing.md`
  - Added post revision history test priority area (create/update/publish revisions, unique revision_number, restore new row + content/seo/slug/status/tags/cover/media exact restore, RBAC ownership, impersonation metadata, pagination + filters)
  - Added post SEO configuration test priority area (validation matrix, allowlist partial updates, preview fallback chains, public seo-meta drafts vs published, event emission on seo update and slug change, revision diff+changelog on seo, restore reproduces seo)

### Updated (contracts)

- `contracts/openapi.yaml` (OpenAPI 3.1)
  - Paths added:
    - `GET /api/v1/admin/posts/{id}/revisions` — paginated revision list with author_id/date/include_diff filters
    - `GET /api/v1/admin/posts/{id}/revisions/{revisionIdOrNumber}` — get full snapshot/diff/changelog (accepts UUID or revision_number)
    - `POST /api/v1/admin/posts/{id}/revisions/{revisionIdOrNumber}/restore` — restore as new revision, CSRF required, optional `restore_note`
    - `GET /api/v1/admin/posts/{id}/seo`, `PUT /api/v1/admin/posts/{id}/seo` — get/update SEO + slug with allowlisted fields + soft warnings in meta
    - `POST /api/v1/admin/posts/{id}/seo/preview` — live SERP / OG / Twitter preview for draft SEO
    - `GET /api/v1/posts/{slug}/seo-meta` — public structured SEO meta tags payload for SSR
  - Schemas added: `PostRevision`, `PostSEOConfig`, `PostSEOUpdateRequest`, `PostSEOPreviewRequest`, `PostSEOPreview`, `PostSEOMetaPublic`, plus 7 Envelope response wrappers
  - Post schema: removed inline `seo_title`/`seo_description` (now in `post_seo`); PostCreateRequest accepts nested `seo: PostSEOConfig`

- `contracts/asyncapi.yaml` (AsyncAPI 2.6)
  - Channels added: `blog.post.revision.created`, `blog.post.revision.restored`, `blog.post.seo.updated`, `blog.post.slug_changed` (outbox + subscribe sides)
  - Messages added: `PostRevisionCreated`, `PostRevisionRestored`, `PostSEOUpdated`, `PostSlugChanged` (all with EventEnvelope traits and impersonator_id field)
  - Admin realtime SSE `AdminAnalyticsRealtime` enum extended with: `post.revision_created`, `post.revision_restored`, `post.seo_updated`, `post.slug_changed`
  - Final totals: 39 channels, 30 messages

### Validation performed

- YAML syntax for both contracts: parsed OK
- OpenAPI 3.x spec validation with `openapi-spec-validator`: PASSED
- AsyncAPI 2.6 structural validation: required keys (`asyncapi`, `info`, `channels`) present, version `2.6.0`, 39 channels declared, components + messages + schemas present
- Full AsyncAPI schema validation available in dev environments via `npx @asyncapi/cli validate contracts/asyncapi.yaml`

## 2026-07-29 — API contracts added

Added two new contract files under `contracts/` to document the project's synchronous and asynchronous interfaces:

### Added

- `contracts/openapi.yaml`
  - OpenAPI 3.1 specification for all project REST endpoints
  - Covers: auth, self-service profile/notifications, public content (posts, categories, tags, media, transforms, comments), admin users/posts/media/settings/impersonation/analytics, consent and analytics ingestion, health endpoints
  - Includes: request/response schemas, bearer (JWT) auth, pagination, envelope and error shapes, status codes, rate limit notes, media associations and include semantics, CSRF and step-up notes
- `contracts/asyncapi.yaml`
  - AsyncAPI 2.6 specification for all asynchronous interfaces
  - Covers: transactional outbox event channels, per-user notification SSE streams, admin analytics realtime SSE streams, internal analytics ingest and job topics, impersonation session events, settings and post-media events, media events
  - Includes: channel definitions, message payloads, security scopes, server definitions, bindings for SSE endpoints
- Developer documentation updates
  - `docs/architecture/architecture.md`: references new `contracts/` directory as the source of truth for API and event contracts
  - `docs/backend/api.md`: added pointers to `contracts/openapi.yaml` as the canonical REST API definition
  - `docs/backend/events.md`: added pointers to `contracts/asyncapi.yaml` as the canonical event/stream definition
  - `docs/backend/jobs.md`: references job channels in AsyncAPI contract
  - `docs/architecture/deployment.md`: notes on contract validation in CI

### Validation performed

- YAML syntax validation for both files: passed
- OpenAPI 3.x spec validation with `openapi-spec-validator`: passed
- AsyncAPI 2.6 structure validation: top-level required keys present (`asyncapi`, `info`, `channels`), 35 channels declared, components declared
