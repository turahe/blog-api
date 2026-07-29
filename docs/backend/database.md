# Database

## Primary Store

PostgreSQL is the source of truth for:

- users
- roles
- permissions
- user_roles
- role_permissions
- posts
- post_media
- categories
- tags
- post_tags
- comments
- media_assets
- media_transforms
- settings
- settings_history
- impersonation_sessions
- post_revisions
- post_seo
- user_profiles
- user_privacy_settings
- user_password_history
- password_reset_tokens
- user_activity
- user_activity_daily (materialized view)
- audit_logs
- outbox_events

## Design Rules

- use UUIDs for external identifiers
- keep unique indexes on slugs, emails, role names, and permission keys
- keep indexed lookup fields for media storage key, checksum, and frequently queried tags
- use timestamps consistently
- soft delete only where it has real product value

## Migration Rules

- keep migrations explicit
- review indexes with every schema change
- avoid hidden auto-migration behavior in production
- apply migrations with `app migrate up`
- rollback with `app migrate down`
- `app migrate down` must support reversible migrations and avoid silent data loss
- seeding initial or reference data must use `app seed`, not migration files

## Redis Usage

Redis is not the source of truth. Use it for:

- cache
- rate limiting
- short-lived session or security state
- coordination for background work
- transformed media cache entries when Redis caching is enabled

## Recommended Fields

### users

- id
- full_name
- email
- email_normalized (unique index; lowercased trimmed for lookups)
- password_hash_version (tinyint: 1=bcrypt, 2=argon2id, future)
- password_hash
- password_changed_at
- require_password_change_next_login boolean default false
- avatar_id (FK -> media_assets.id, nullable, one-to-one avatar)
- avatar_url (computed/legacy fallback or sync-friendly field, optional)
- status enum: active, invited, locked, suspended, soft_deleted
- login_count int
- last_login_at
- last_login_ip (truncated)
- last_login_ua_bucket varchar(64)
- email_verified_at
- email_change_pending_new_email (nullable encrypted or hash)
- email_change_pending_token_jti (nullable)
- created_at
- updated_at
- deleted_at (nullable, for user soft-delete)

### roles

- id
- name
- slug
- description
- created_at
- updated_at

### permissions

- id
- key
- description
- resource
- action
- created_at
- updated_at

### user_roles

- id
- user_id
- role_id
- assigned_by
- created_at

### role_permissions

- id
- role_id
- permission_id
- created_at

### posts

- id
- author_id
- category_id
- title
- slug
- excerpt
- content
- cover_image_media_id (FK -> media_assets.id, nullable, primary cover image)
- cover_image_url (optional derived/fallback field)
- status
- published_at
- created_at
- updated_at
- deleted_at

### post_media

- id
- post_id (FK -> posts.id)
- media_asset_id (FK -> media_assets.id)
- kind (cover, inline_image, attachment)
- sort_order
- created_at

### categories

- id
- parent_id
- name
- slug
- description
- image_id (FK -> media_assets.id, nullable, one-to-one category cover image)
- lft
- rgt
- depth
- sort_order
- created_at
- updated_at

### tags

- id
- name
- slug
- created_at
- updated_at

### post_tags

- id
- post_id
- tag_id
- created_at

### comments

- id
- post_id
- parent_id
- author_name
- author_email
- author_website
- content
- status
- lft
- rgt
- depth
- ip_address
- user_agent
- created_at
- updated_at

### media_assets

- id
- parent_id
- storage_key
- original_filename
- content_type
- size_bytes
- width
- height
- checksum_sha256
- disk
- tags
- metadata
- lft
- rgt
- depth
- sort_order
- uploaded_by
- created_at
- updated_at
- deleted_at

### media_transforms

- id
- media_asset_id
- cache_key
- transform_name
- width
- height
- fit
- format
- quality
- storage_key
- size_bytes
- content_type
- last_accessed_at
- expires_at
- created_at
- updated_at

### audit_logs

- id
- actor_id
- action
- resource_type
- resource_id
- request_id
- ip_address
- metadata
- created_at

### outbox_events

- id
- aggregate_type
- aggregate_id
- event_name
- payload
- status
- published_at
- retry_count
- created_at
- updated_at

### settings

- id
- key
- value_jsonb
- value_type
- sensitivity
- category
- version
- description
- updated_by
- created_at
- updated_at

### settings_history

- id
- setting_id
- key
- previous_value_jsonb
- new_value_jsonb
- changed_by
- request_id
- ip_address
- created_at

### impersonation_sessions

- id
- impersonator_user_id
- impersonator_session_id
- impersonated_user_id
- state
- started_at
- expires_at
- exited_at
- revoked_reason
- document_id
- reason
- stepup_verified
- csrf_token_hash
- request_id
- ip_address
- created_at
- updated_at

### post_revisions

Append-only history table for all post revisions (create/update/restore/publish/archive).

- id (UUID PK)
- post_id (FK -> posts.id, indexed, CASCADE on post delete)
- revision_number (integer per-post sequence; unique(post_id, revision_number))
- revision_type enum: create, update, restore, publish, archive
- title (snapshot)
- slug (snapshot)
- excerpt (snapshot)
- content (snapshot)
- status (snapshot)
- author_id (user_id of the modifier/editor, indexed)
- category_id_snapshot
- cover_image_media_id_snapshot
- media_snapshot_jsonb (post_media rows snapshot)
- tags_snapshot_jsonb (tag ids + names snapshot)
- seo_snapshot_jsonb (SEO fields snapshot)
- diff_jsonb: per-field old/new (title/slug/excerpt/content/status/category/cover/media/tags/SEO)
- changelog_text: auto-generated human-readable summary
- editor_note: optional free text note at save time
- restore_from_revision_id (nullable FK -> post_revisions.id, indexed)
- impersonator_id (nullable)
- impersonation_session_id (nullable)
- request_id (nullable for correlation)
- created_at
- updated_at

### post_seo

One-to-one SEO configuration for posts. Option A (separate table); fallback Option B is embedding these fields directly on posts.

- id (UUID PK)
- post_id (FK -> posts.id, unique, indexed, CASCADE on post delete)
- seo_title
- seo_description
- seo_keywords JSONB (string array)
- og_title
- og_description
- og_image_id (nullable FK -> media_assets.id, SET NULL on media delete)
- og_url (nullable)
- twitter_card enum: summary, summary_large_image, app, player
- twitter_title
- twitter_description
- twitter_image_id (nullable FK -> media_assets.id, SET NULL on media delete)
- twitter_creator
- canonical_url (nullable)
- robots_noindex boolean default false
- robots_nofollow boolean default false
- created_at
- updated_at

### user_profiles

Sparse one-to-one extension to `users` carrying seldom-edited contact/social/locale payload.
Kept separate from `users` so the primary auth row stays lean for login hot-path queries.

- id (UUID PK)
- user_id (FK -> users.id, UNIQUE, CASCADE on user delete; mandatory NOT NULL)
- display_name varchar(60) (nullable, application-level case-insensitive unique; non-nulls enforce DB unique partial index)
- bio text (max 4000 chars sanitized markdown)
- encrypted_contact_phone bytea (AES-256-GCM envelope ciphertext for phone number; never stored plain text)
- contact_phone_verified_at nullable timestamp (SMS/OTP verified flag for E.164)
- contact_website varchar(2048) (http/https only)
- contact_location varchar(120)
- social_links JSONB: {twitter, linkedin, github} — normalized to slugs or full URLs
- locale varchar(32) (BCP 47; e.g. en_US, zh_Hans_CN)
- timezone varchar(64) (IANA tz name; default UTC)
- marketing_consent boolean default false
- marketing_consent_updated_at
- updated_by (FK -> users.id, nullable; for admin-initiated updates or impersonation)
- created_at
- updated_at

### user_privacy_settings

One-to-one per user: controls visibility of public profile and related routes.

- id (UUID PK)
- user_id (FK -> users.id, UNIQUE, CASCADE)
- visibility_profile enum: public, unlisted, private, followers_only — default public
- visibility_email boolean default false (never show email publicly; admin and owner still see it)
- visibility_contact_details boolean default false (applies to phone/website/location on public page)
- visibility_activity_timeline boolean default false (applies to public `/users/:name/activity` if ever exposed)
- search_allow_indexing boolean default true (controls SSR `X-Robots-Tag` header and `<meta name=robots>`)
- tracking_personalize_ads boolean default false (propagates to analytics consent service)
- updated_by (FK -> users.id, nullable)
- created_at
- updated_at

### user_password_history

Append-only password hash history used by the N-history reuse policy (default N=10).

- id (UUID PK)
- user_id (FK -> users.id CASCADE)
- password_hash_version tinyint (1=bcrypt, 2=argon2id, …)
- password_hash text (bcrypt/argon2id output; same hash policy as users.password_hash)
- created_at

### password_reset_tokens

Opaque + JWT reset token tracking table for forgot-password AND email-change tokens.
All tokens are single-use via `consumed_at` and server-side Redis SETNX guard to prevent replay within a window.

- id (UUID PK)
- user_id (FK -> users.id CASCADE; nullable when scope=email_change lookup by JTI)
- jti varchar UNIQUE NOT NULL (matches JWT jti claim)
- scope enum: forgot, email_change
- token_sha256 bytea (SHA-256 of opaque bearer token, optional)
- email_address_hash bytea (peppered HMAC of email; used for server-side salt check vs URL parameter)
- expires_at timestamp NOT NULL
- consumed_at nullable timestamp
- issued_ip (truncated; v4 /24, v6 /64 or encrypted bytea depending on policy)
- issued_ua_bucket varchar(64)
- created_at

### user_activity

Append-only engagement trail surfaced via `/me/activity`. This table serves both as the user-visible
engagement history AND provides audit-grade context for security/trust investigations at admin level.

- id (UUID PK)
- user_id (FK -> users.id CASCADE, index)
- session_id nullable (JWT jti or opaque session id)
- impersonator_id nullable FK -> users.id
- impersonation_session_id nullable UUID
- request_id nullable (correlates to API gateway/RPC request id)
- activity_type enum: login, logout, profile_edit, avatar_update, password_change, password_reset, email_change, consent_grant, consent_withdraw, post_create, post_edit, post_publish, comment_create, twofa_enable, twofa_disable, oauth_link, oauth_unlink, impersonation_start, impersonation_end, role_change, settings_view, privacy_change, marketing_consent_grant, marketing_consent_withdraw, export_requested, erasure_requested
- summary varchar(500) (human-readable, safe to display; no secrets)
- detail_jsonb (schema-per-type; contains post_id, route, device class, partial geo, diff_hash, etc.)
- ip_address (truncated; v4 /24 or v6 /64; admin see full via encrypted detail_jsonb if needed)
- user_agent_bucket varchar(64)
- geo_country_code char(2) nullable
- geo_subdivision varchar(16) nullable
- created_at timestamp (never updated)

### user_activity_daily (materialized view)

Daily-aggregate materialized view refreshed hourly by the `app scheduler` worker. Used for admin dashboards
and self-service "active days" charts.

- day DATE
- user_id UUID
- login_count int, profile_edit_count int, avatar_update_count int, password_change_count int, post_create_count int, comment_create_count int, twofa_events_count int
- unique_active_minutes smallint (estimate, bucketed)
- created_at timestamp (refresh marker)

UNIQUE PRIMARY KEY (day, user_id)

### casbin_rules

Generic Casbin policy tuple table. Source of truth for **all** RBAC decisions. Format follows the canonical Casbin database adapter (tuple-style columns v0..v5 for extensibility). The ORM/DB adapter maps this table into an in-memory Casbin Model.

- id (bigserial PK or UUID; bigserial preferred for insertion-heavy tuple writes)
- p_type varchar(8) NOT NULL — Casbin policy type: `p` (permission rule), `g` (grouping/user-role assignment), `g2` (role-role inheritance), or future `e` (explicit effect line rarely used)
- v0 varchar(255) — subject; for p_type=`p` → role or user; for p_type=`g` → user_id or `user:<id>`; for p_type=`g2` → child role
- v1 varchar(255) — object (permission key like `rbac.role:read` or `*:*`); or for grouping → role or parent role
- v2 varchar(255) — action/scope (`read` / `manage` / `*`) or for grouping → domain (always `*` unless multi-tenant mode enabled)
- v3 varchar(16)  — effect: `allow` | `deny` (only meaningful when p_type=`p`; grouping rules ignore it)
- v4 varchar(128) — reserved/domain (future workspace/team column)
- v5 varchar(128) — reserved
- created_at timestamp
- updated_at timestamp
- UNIQUE (p_type, v0, v1, v2, v3, v4, v5) — dedup identical policy rows across all columns

### rbac_roles

Role metadata catalog (mirror). Writes are driven by RBACService after Casbin adapter commits; never write to this table directly.

- id (UUID PK)
- name varchar(60) UNIQUE — snake_case role name; DB unique index
- display_name varchar(120) — human-friendly name for UI
- description varchar(500)
- tier int2 NOT NULL CHECK (tier BETWEEN 1 AND 4) — 1=viewer tier, 4=superadmin; used for tier gating enforcement
- inherits_from_id UUID nullable FK → rbac_roles.id (self-referential for Casbin `g2` role-role inheritance rules; when a role is saved its inheritance row in casbin_rules is upserted)
- is_system boolean NOT NULL DEFAULT false — true for `viewer|editor|admin|superadmin`; cannot be deleted
- is_custom boolean GENERATED ALWAYS AS (NOT is_system) STORED
- hidden_from_ui boolean DEFAULT false — quarantine/internal roles not exposed in default role pickers
- version int NOT NULL DEFAULT 0 — optimistic lock counter; incremented on every permission or metadata edit
- created_by UUID FK → users.id
- created_at timestamp
- updated_at timestamp
- deleted_at timestamp nullable (soft delete only for custom roles; system rows never soft delete)

### rbac_permissions

Permission registry catalog (mirror). Source of truth is the code-side typed registry + seed. This table is the readable/searchable mirror used by Admin UI list/search.

- id (UUID PK)
- key varchar(180) UNIQUE NOT NULL — `resource:action[:scope]`. Max ~180 chars to accommodate 3 colon segments comfortably
- resource varchar(64) NOT NULL — first segment (rbac.role, user, post, settings, impersonation, analytics, …)
- action varchar(64) NOT NULL — second segment (read, manage, update, publish, export, …)
- scope varchar(64) NOT NULL DEFAULT 'all' — optional third segment (all, own, uuid, security, …)
- description varchar(500)
- category varchar(64) NOT NULL (RBAC, User, Post, Media, Settings, Analytics, Impersonation, Profile, System)
- default_tier int2 — default minimum tier a role must have to be granted this when a new role uses "inherit from viewer/editor/admin" template
- hidden_flag boolean NOT NULL DEFAULT false — hide from UI (internal only permissions)
- code_version varchar(32) nullable — last code registry schema version that asserted this key exists (drift detection via doctor)
- created_at timestamp
- updated_at timestamp

### user_role_assignments

Mirror of Casbin `g(user_id, role_id)` grouping tuples. UI-searchable join table with admin metadata (who assigned, when, optional temp grant expiry).

- id (UUID PK)
- user_id UUID NOT NULL FK → users.id ON DELETE CASCADE
- role_id UUID NOT NULL FK → rbac_roles.id ON DELETE CASCADE
- assigned_by UUID NOT NULL FK → users.id (required; system assignments = bootstrap superadmin id)
- assignment_reason varchar(500) nullable — free text note from admin
- expires_at timestamp nullable — temporary grant; scheduler sweeper revokes expired rows (sweeper also removes matching Casbin g-row)
- source enum: api, bulk_import, ldap_sync, bootstrap_seed, self_signup_default
- impersonation_session_id UUID nullable — only used when the assignment was made *during* an impersonation session for audit trail (never used for logic)
- created_at timestamp NOT NULL
- updated_at timestamp NOT NULL
- UNIQUE(user_id, role_id) — one role assignment per user+role pair (expires_at changes still use the same pair; update)

### rbac_policy_audit_log

Append-only log of policy mutations. PostgreSQL `RULE` or trigger blocks any UPDATE/DELETE on this table (except superuser curator role).

- id (bigserial PK)
- mutation_type enum: role_created, role_updated, role_deleted,
  role_permissions_assigned, role_permissions_revoked,
  user_role_assigned, user_role_revoked,
  policy_force_reload, mirror_resync_triggered, mirror_drift_repaired
- actor_user_id UUID FK → users.id
- impersonator_id UUID nullable FK → users.id
- impersonation_session_id UUID nullable
- target_role_id UUID nullable
- target_user_id UUID nullable
- tuples_added_jsonb JSONB — array of added Casbin tuples `[{p_type, v0..v5}]`
- tuples_removed_jsonb JSONB — array of removed tuples
- mirror_delta_jsonb JSONB — before/after of rbac_roles / user_role_assignments mirror rows
- tier_escalation_detected boolean DEFAULT false — review flag when mutation attempted to raise tier to equal caller
- request_id UUID nullable
- ip_address inet nullable (truncated for GDPR: /24 or /64)
- user_agent_bucket varchar(64) nullable
- stepup_verified enum: none, password, totp, webauthn — for compliance audit of 2FA proof presence
- created_at timestamp NOT NULL DEFAULT now()

### rbac_enforcement_events

Append-only request-level enforcement audit. Written through the Watermill outbox → async bulk insert worker to avoid adding latency to hot-path requests. Retention: 90 days hot, 1 year cold archive (moved by scheduler).

- id (bigserial PK)
- decision enum: allow, deny, deferred, error
- deny_reason_code varchar(64) nullable — matches error catalogue (rbac.forbidden, rbac.deny_storm, …)
- user_id UUID NOT NULL (resolved authenticated user; 00000000-0000-0000-0000-000000000000 for anonymous → still audited but code paths usually 401 before RBAC)
- impersonator_id UUID nullable
- impersonation_session_id UUID nullable
- effective_roles_array TEXT[] — snapshot of roles at enforce time (for debugging)
- required_object varchar(180) NOT NULL — permission.object / permission string
- required_action varchar(64) NOT NULL
- required_domain varchar(64) NOT NULL DEFAULT '*'
- request_method varchar(8) NOT NULL
- request_path varchar(512) NOT NULL
- route_pattern varchar(256) NOT NULL — e.g. `/api/v1/admin/users/:id/roles` (for grouping)
- request_id UUID nullable
- trace_id varchar(64) nullable
- ip_address inet (truncated)
- user_agent_bucket varchar(64)
- latency_ns bigint NOT NULL — wall-clock time from middleware enter → exit (perf SLO tracking)
- enforcer_generation_id bigint NOT NULL — cache generation at time of decision (helps debug cache stale issues after reload)
- created_at timestamp NOT NULL DEFAULT now()
PARTITION KEY (monthly list partitioning on created_at recommended for retention drops)

## Notes

- use UUIDs for all primary keys
- add unique indexes for `users.email`, `users.email_normalized`, `roles.slug`, `permissions.key`, `posts.slug`, `categories.slug`, `tags.slug`, `settings.key`, `user_profiles.display_name` (partial unique where not null), `user_profiles.user_id`, `user_privacy_settings.user_id`, `password_reset_tokens.jti`, `rbac_roles.name`, `rbac_permissions.key`, `user_role_assignments(user_id, role_id)`, `casbin_rules(p_type, v0, v1, v2, v3, v4, v5)`
- index foreign keys and frequently filtered status columns
- use JSON or JSONB for flexible metadata, event payload, and setting value fields where appropriate
- model `categories`, `comments`, and `media_assets` as nested set trees using `parent_id`, `lft`, `rgt`, and `depth`
- add composite indexes for nested set traversal, such as `(lft, rgt)` and where useful `(parent_id, sort_order)`
- add `settings_history(setting_id, created_at desc)` index for efficient change-history lookups
- add composite indexes on `impersonation_sessions`:
  - `(impersonator_user_id, state)` partial for state='active' to enforce one active impersonation per superadmin
  - `(expires_at, state)` for expiry sweepers
  - `(impersonated_user_id, created_at desc)` for audit lookups
- add indexes and FK constraints for media associations:
  - `users.avatar_id` -> `media_assets.id` with ON DELETE SET NULL
  - `categories.image_id` -> `media_assets.id` with ON DELETE SET NULL
  - `posts.cover_image_media_id` -> `media_assets.id` with ON DELETE SET NULL
  - `post_media.post_id` + `post_media.media_asset_id` with composite unique on `(post_id, media_asset_id, kind)` where applicable, plus index `(post_id, sort_order)`
- relational mapping summary:
  - `users (1) -- (0..1) media_assets` via `avatar_id` (one-to-one avatar)
  - `posts (1) -- (0..N) post_media -- (1) media_assets` via join table (one-to-many attached images with kind + order)
  - `categories (1) -- (0..1) media_assets` via `image_id` (one-to-one category cover)
  - `posts (1) -- (0..N) post_revisions` via `post_id` (append-only revisions; CASCADE delete on post delete)
  - `posts (1) -- (0..1) post_seo` via `post_id` (one-to-one SEO config; CASCADE delete on post delete)
  - `post_seo.og_image_id / twitter_image_id` SET NULL on referenced `media_assets` delete
  - `users (1) -- (0..1) user_profiles` via `user_id` (1:1 extension; created lazily on first profile edit or eagerly by seeder; CASCADE delete)
  - `users (1) -- (0..1) user_privacy_settings` via `user_id` (1:1; default row created by user-create trigger or service; CASCADE delete)
  - `users (1) -- (0..N) user_password_history` via `user_id` (append only; CASCADE delete)
  - `users (1) -- (0..N) password_reset_tokens` via `user_id` (append-only; CASCADE delete; email_change tokens still carry user_id too)
  - `users (1) -- (0..N) user_activity` via `user_id` (append-only engagement log; CASCADE delete on hard user deletes activity)
  - `users (1) -- (0..N) user_activity` via `impersonator_id` SET NULL when the impersonator account is removed from the system (soft delete or CASCADE via impersonator_id FK)
  - `users.avatar_id` SET NULL on referenced `media_assets` delete; detaching an avatar never deletes the user row
- add composite indexes on `post_revisions`:
  - `(post_id, revision_number)` unique for friendly per-post numbering
  - `(post_id, created_at desc)` for timeline queries
  - `(author_id)` for editor-centric audit views
  - `(restore_from_revision_id)` partial where not null for restore-audit chains
- add indexes on `post_seo`:
  - `unique(post_id)` to enforce one-to-one
  - `(og_image_id)` and `(twitter_image_id)` for FK joins when resolving social card images
- `post_revisions` rows are append-only and never updated after insert (except for `updated_at` as an implementation detail of ORM defaults); consider triggers or application logic to enforce immutability
- on slug change via SEO endpoints, emit a `blog.post.slug_changed` event so redirect/routing systems can record the old → new mapping
- add indexes on `user_profiles`:
  - `unique(user_id)` to enforce one-to-one
  - partial `unique(display_name)` where `display_name is not null`
  - `(locale)` and `(timezone)` for cohort/analytics queries
- add indexes on `user_privacy_settings`:
  - `unique(user_id)` to enforce one-to-one
  - `(visibility_profile)` to support fast "find all public users" admin searches
- add indexes on `user_password_history`:
  - `(user_id, created_at desc)` — last N passwords scan for the reuse policy
- add indexes on `password_reset_tokens`:
  - `unique(jti)` — JWT/opaque single-use
  - `(user_id, created_at desc)` — recent tokens
  - `(scope, expires_at)` partial where `consumed_at is null` — cleanup sweepers
- add indexes on `user_activity`:
  - `(user_id, created_at desc)` — owner timeline
  - `(activity_type, created_at desc)` — audit by type
  - `(impersonation_session_id, created_at)` — impersonation audit trails
  - GIN index on `detail_jsonb` — admin post_id/route lookups
- `user_activity_daily` materialized view refresh concurrently where supported, coordinated by the `app scheduler` worker with advisory locks so horizontal deployments don't double-refresh
- `user_activity` rows are append-only; never update a row once created. Erasure requests use a separate erase marker and zero out PII-bearing fields (ip_address, detail_jsonb with partial geo, user_agent_bucket) in place or move to a redacted archive table for the retention window — never fully delete while an active investigation may require the record
- encrypted contact fields (user_profiles.encrypted_contact_phone) use envelope encryption with per-row data keys; the KMS/ENV master key MUST NOT appear in migrations, seeds, or code comments; rotation strategy documented in `docs/architecture/security.md`
- user emails are case-insensitive unique via `users.email_normalized` (lowercased, trimmed, punycode-IDNA encoded); the `email` display field preserves original casing
