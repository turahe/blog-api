# Changelog

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
