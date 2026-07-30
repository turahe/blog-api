# Changelog

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
