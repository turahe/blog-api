# Models

How this project represents data across layers, and which entities exist in the
PostgreSQL source of truth. Column-level detail lives in [database.md](./database.md).
Relationships and Mermaid ERD live in [ERD.md](./ERD.md).

## Purpose

| Layer | Role | Location (target) |
| --- | --- | --- |
| Domain model | Entities, value objects, invariants, domain errors | `internal/core/<module>/domain` |
| Persistence model | GORM structs, table tags, soft-delete hooks | `internal/adapters/outbound/persistence` |
| Transport DTO | Request/response shapes bound to OpenAPI | `internal/adapters/inbound/http` (+ contract schemas) |

Rules of thumb:

- domain models never import Gin, GORM, Redis, or Watermill
- persistence models never leak into HTTP handlers or domain packages
- repositories map persistence ↔ domain at the adapter boundary
- OpenAPI schemas are the source of truth for public JSON shapes — not GORM tags

## Package Layout

```text
internal/core/<module>/
  domain/     # User, Post, Status enums, invariants
  ports/      # UserRepository, PostPublisher, …
  service/    # use cases; depends on ports only

internal/adapters/outbound/persistence/
  models/     # gorm.Model-style structs + table names (optional subpkg)
  <module>_repository.go

internal/adapters/inbound/http/
  # bind DTOs / map envelope data; no GORM types
```

Cross-module reads go through ports or domain events — not by importing another
module’s persistence package. See [architecture.md](../architecture/architecture.md).

## Mapping Conventions

```text
HTTP DTO  --validate-->  service command/query
service                --> domain entity (invariants)
repository             --> persistence model  <-->  domain entity
```

| Concern | Domain | Persistence | HTTP |
| --- | --- | --- | --- |
| ID | `uuid.UUID` / typed ID | `uuid` PK | string UUID in JSON |
| Timestamps | domain time / clock port | `timestamptz` | RFC3339 strings |
| Soft delete | usually hidden (`deleted_at` unset ⇒ active) | `DeletedAt` / scoped queries | never expose unless admin contract says so |
| Secrets | never on entity for API use | `password_hash`, encrypted blobs | never serialized |
| Enums | typed string/const | `text` + CHECK | OpenAPI enum |
| Nested trees | parent + depth semantics | `parent_id`, `lft`, `rgt`, `depth` | flatten or tree DTO per contract |
| JSON blobs | value objects | `jsonb` | structured objects in OpenAPI |

### Do

- map in repository constructors / private helpers (`toDomain`, `toModel`)
- keep allowlisted PATCH fields in the HTTP adapter; apply only known keys in the service
- emit domain events from services after successful persistence (outbox in same TX when required)

### Do not

- embed `gorm.Model` into domain types
- return persistence structs from ports
- trust client-supplied IDs for ownership; resolve effective user from auth context

## GORM / Persistence Conventions

- table names: snake_plural matching [database.md](./database.md) (`users`, `post_media`, …)
- primary keys: UUID (`gen_random_uuid()` / app-generated); no serial IDs in public APIs
- indexes and CHECKs belong in goose migrations, not only in GORM tags
- soft delete only where product requires restore/tombstone: typically `users`, `posts`, `media_assets`, `comments`
- append-only tables: no GORM `Updates` on historical rows (`post_revisions`, `audit_logs`, `outbox_events`, `user_activity`, `settings_history`, password history/tokens)
- Casbin: `casbin_rules` is canonical; `rbac_*` mirrors are service-maintained — do not write mirrors from ad-hoc repos
- foundation migration [00001_foundation.sql](../../internal/platform/migrations/sql/00001_foundation.sql) is a **bootstrap subset**; full target schema is [database.md](./database.md) + [ERD.md](./ERD.md)

## Lifecycle Patterns

| Pattern | Tables (examples) | Rules |
| --- | --- | --- |
| Soft delete | `users`, `posts`, `media_assets`, `comments` | unique indexes usually partial `WHERE deleted_at IS NULL` |
| Hard delete / CASCADE | junction rows (`post_tags`, `user_roles`) | cascade with parent where appropriate |
| Append-only | `post_revisions`, `audit_logs`, `outbox_events`, `user_activity` | INSERT only; restore = new revision row |
| 1:1 sidecar | `user_profiles`, `user_privacy_settings`, `post_seo` | keep hot `users`/`posts` rows lean |
| Nested set | `categories`, `comments`, `media_assets` | maintain `lft`/`rgt`/`depth` in service, not ad-hoc SQL |
| Outbox | `outbox_events` | same transaction as aggregate write |

## Entity Catalog

Authoritative columns: [database.md](./database.md). Relationships: [ERD.md](./ERD.md) §3.

### IAM

| Entity | Purpose | Notes |
| --- | --- | --- |
| `users` | Account identity, credentials, status | Soft delete; avatar FK → `media_assets` |
| `user_oauth_accounts` | Linked social providers | Unique per provider+subject |
| `user_two_factor_methods` | TOTP / factors | Secrets encrypted at rest |
| `user_password_history` | Prior hashes | Append-only; reuse checks |
| `password_reset_tokens` | Forgot/reset + email-change JTIs | Single-use; Redis may guard jti |
| `casbin_rules` | Canonical RBAC policy | Tuple store; not UI-friendly |
| `rbac_roles` / `rbac_permissions` | Readable RBAC mirrors | Maintained by RBAC service |
| `user_role_assignments` | User↔role mirror | Optional expiry metadata |
| `rbac_policy_audit_log` | Policy mutation audit | Append-only |
| `rbac_enforcement_events` | Per-request allow/deny telemetry | Partitioned / high volume |

Readable mirrors in early migrations may appear as `roles`, `permissions`, `user_roles`, `role_permissions` — evolve toward the dual-stack names in [ERD.md](./ERD.md) / [rbac-casbin.md](./rbac-casbin.md).

### User profile

| Entity | Purpose | Notes |
| --- | --- | --- |
| `user_profiles` | Contact, bio, social | 1:1 with `users` |
| `user_privacy_settings` | Visibility / indexing flags | Drives public profile 404/noindex |
| `user_activity` | Engagement timeline | Append-only; PII erasable under GDPR flow |
| `user_activity_daily` | Daily aggregates | Materialized view |

### Content

| Entity | Purpose | Notes |
| --- | --- | --- |
| `posts` | Articles / drafts | Soft delete; version; author + category FKs |
| `categories` | Taxonomy tree | Nested set + optional cover `image_id` |
| `tags` | Flat labels | Unique slug |
| `post_tags` | Post↔tag | Composite uniqueness |
| `post_revisions` | Snapshot/diff history | Append-only; restore creates new row |
| `post_seo` | Per-post SEO / OG | 1:1 with post; slug rules in SEO docs |

### Comments / moderation

| Entity | Purpose | Notes |
| --- | --- | --- |
| `comments` | Threaded comments | Nested set; anonymous or authed author |
| `comment_flags` | User reports | Dedupe per identity |
| `comment_upvotes` | Votes | Auth required |
| `comment_moderation_log` | Moderator actions | Append-only audit |

### Media

| Entity | Purpose | Notes |
| --- | --- | --- |
| `media_assets` | Stored originals / folders | Nested set; soft delete; checksum |
| `media_transforms` | Derived variants + TTL | Cache key + last_accessed |
| `post_media` | Ordered post attachments | Kind + sort_order allowlist |

### Notifications

| Entity | Purpose | Notes |
| --- | --- | --- |
| `notifications` | Inbox rows | Fan-out per user; SSE companion |
| `notification_preferences` | Per-type delivery flags | Unique `(user_id, type)` |

### Newsletter

| Entity | Purpose | Notes |
| --- | --- | --- |
| `newsletter_subscribers` | Subscriber identity | Double opt-in |
| `newsletter_list_memberships` | List membership | |
| `newsletter_issues` | Sent issues | Admin lifecycle |
| `newsletter_provider_syncs` | External ESP sync | |
| `newsletter_consent_audit` | Consent changes | Append-only |

### Analytics

| Entity | Purpose | Notes |
| --- | --- | --- |
| `analytics_page_views` | Consent-gated views | Hot path; may truncate |
| `analytics_time_spent` | Dwell slabs | Linked to page views |

### Configuration / system

| Entity | Purpose | Notes |
| --- | --- | --- |
| `settings` | Key/value config | Sensitivity tiers; never return `server_only` |
| `settings_history` | Change snapshots | Append-only |
| `impersonation_sessions` | Active impersonation | Short TTL; audit metadata |
| `audit_logs` | Generic security/product audit | Append-only; keep on actor delete |
| `outbox_events` | Reliable domain event publish | Same TX as aggregate |

## Domain Module Ownership

| Core module | Primary entities |
| --- | --- |
| `auth` | users credentials, oauth, 2FA, password reset/history, sessions (Redis) |
| `user` | profiles, privacy, activity |
| `rbac` | casbin_rules, mirrors, enforcement/audit |
| `post` | posts, revisions, seo, post_tags, post_media |
| `media` | media_assets, media_transforms |
| `comment` | comments, flags, upvotes, moderation log |
| `notification` | notifications, preferences |
| `settings` | settings, settings_history |
| `analytics` | page_views, time_spent (+ Redis ingest buffers) |

## Checklist: Adding a Model

1. Specify columns and indexes in [database.md](./database.md); update [ERD.md](./ERD.md) if relationships change.
2. Add a goose migration under `internal/platform/migrations/sql/`.
3. Add domain type + ports in `internal/core/<module>/`.
4. Add persistence struct + repository mapper (no GORM in core).
5. Expose only via OpenAPI-backed HTTP DTOs; committed swag docs under `docs/` (`make swagger`)  / `make routes-check` as needed.
6. Cover mapping and lifecycle in tests ([testing strategy](../testing/strategy.md)).

## Related Docs

- [database.md](./database.md) — schema, indexes, Cloud SQL
- [ERD.md](./ERD.md) — Mermaid ERD and domain groupings
- [services.md](./services.md) — use-case orchestration
- [api.md](./api.md) — HTTP surface and route groups
- [architecture.md](../architecture/architecture.md) — hexagonal rules
- [coding-standards.md](../architecture/coding-standards.md) — mapping rule summary
