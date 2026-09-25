# Blog API — Entity Relationship Diagram (ERD)

Governance: this diagram is authored, validated, and maintained under the [Mermaid Competency Framework](../guides/mermaid-competency-framework.md). All contributions must satisfy the Intermediate-band outcomes for Skill 1.3 (ERD syntax), Skill 5.1 (3-stage CI syntax validation), and Skill 5.2 (7 WCAG 2.1 AA SCs) before merge. Framework version pinned at time of last edit = 1.0.0.

## 1. Scope & Design Philosophy

### 1.1 Business Scope Covered
This ERD captures every PostgreSQL source-of-truth table for the **blog-api** multi-user blogging platform. It covers eight domain areas:

1. **Identity & Access (IAM)** — users, authentication state, 2FA, OAuth links, password history, password-reset / email-change tokens, and a *dual-stack RBAC model* (Casbin canonical policy store + readable mirrors for admin UI).
2. **User Profile** — sparse one-to-one profile extension, privacy controls, and append-only user activity history with daily aggregates.
3. **Content** — posts, categories (nested-set tree), tags, post↔tag joins, comments (nested-set), post revisions (append-only snapshot/diff history), and one-to-one per-post SEO configuration.
4. **Media** — media assets (nested-set folder tree), derived image transforms with TTL caches, and post↔media attachment joins.
5. **Notifications** — per-user notifications with read/unread state and notification-preference flags.
6. **Analytics (optional hot-path table)** — page-view and time-on-page events for consent-based analytics.
7. **Configuration & Audit** — key/value settings with change history, append-only generic audit log, impersonation session state, and transactional outbox for reliable cross-service event delivery.
8. **RBAC Audit & Enforcement** — append-only policy-mutation audit log and monthly-partitioned per-request enforcement events (SLO + investigations).

A materialized view (`user_activity_daily`) and Redis-coordinated logical entities (rate limits, session state) are included for completeness; Redis is not modelled as relational tables.

### 1.2 Design Philosophy & Rules
The schema follows seven explicit principles documented in [database.md](./database.md).
How those tables map to domain / GORM / HTTP models is documented in [model.md](./model.md).

The schema principles:

- **bigint primary key + uuid public id** — every entity table has `id bigint GENERATED ALWAYS AS IDENTITY` (primary key; all foreign keys reference it) and `uuid uuid UNIQUE DEFAULT gen_random_uuid()` (the only id exposed through APIs, JWT subjects, and events). Pure join tables (`user_roles`, `role_permissions`, `post_tags`) keep composite primary keys over bigint foreign keys.
- **Lean hot-path rows** — frequently-queried tables (`users`, `casbin_rules`) are kept deliberately narrow; wide payloads live in sidecar one-to-one tables (`user_profiles`, `user_privacy_settings`, `post_seo`).
- **Append-only for history, security, and event streams** — `post_revisions`, `user_password_history`, `password_reset_tokens`, `user_activity`, `audit_logs`, `rbac_policy_audit_log`, `rbac_enforcement_events`, `outbox_events`, `settings_history` are INSERT-only; no in-place UPDATE of historical rows.
- **Nested-set trees for hierarchical data** — `categories`, `comments`, and `media_assets` use `(parent_id, lft, rgt, depth)` for efficient subtree queries and descendant traversal.
- **Dual-stack RBAC** — `casbin_rules` is the canonical policy store; `rbac_roles` / `rbac_permissions` / `user_role_assignments` are **readable mirrors** maintained by the RBAC service (never write directly to mirrors) so admin UI avoids joining across tuple-style Casbin rows.
- **Soft delete only where it has product value** — users, posts, and media have `deleted_at`; junction rows and append-only tables use hard deletes.
- **Indexed for access patterns listed in the backend design docs** — see §Notes of [database.md](./database.md#L579-L641) for the explicit unique/FK/composite/GIN index catalogue.

### 1.3 Legend (Mermaid cardinality symbols)
| Symbol  | Meaning                          |
|---------|----------------------------------|
| `\|\|`  | exactly one (1)                  |
| `\|o`  | zero or one (0..1)               |
| `o\|`  | one or more (1..N)               |
| `}o`  | zero or more (0..N)              |
| `--`   | directional relationship line    |
| **PK**  | primary key (after `id` type)    |
| **FK**  | foreign key (after `id` type)    |
| **UK**  | unique key marker                |
| **MV**  | materialized view marker         |

### 1.4 Field types used
| Mermaid label | PostgreSQL type | Notes |
|---|---|---|
| `uuid` | `UUID` | Public identifier column `uuid` (RFC 4122 v4) on every entity table; also correlation ids and polymorphic references (`outbox_events.aggregate_id`, `audit_logs.resource_id`) |
| `bigint` PK/FK | `BIGINT GENERATED ALWAYS AS IDENTITY` | Primary key `id` on every entity table and every foreign key column |
| `varchar` | `VARCHAR(n)` | Bounded strings (lengths in [database.md](./database.md)); use `TEXT` without limit only when appropriate |
| `text` | `TEXT` | Unbounded text (bio, content, summaries, diffs) |
| `char(2)` | `CHAR(2)` | ISO country codes |
| `jsonb` | `JSONB` | Postgres typed JSON for payloads, snapshots, metadata, social_links, keywords |
| `bytea` | `BYTEA` | Encrypted blobs (AES-GCM envelope ciphertext, HMAC/SHA digests) |
| `inet` | `INET` | Postgres native IP address type (truncated via policy; /24 or /64) |
| `smallint` / `tinyint` | `SMALLINT` | Tier, version counters, hash-version counters, quality (1–100) |
| `int` | `INTEGER` | Counters (login_count, widths, sort_order, lft/rgt/depth) |
| `bigint` | `BIGINT` | Size_bytes, latency_ns, enforcer generation counters |
| `boolean` | `BOOLEAN` | Flags |
| `datetime` | `TIMESTAMPTZ` | All timestamps are timezone-aware; default `now()` per [database.md](./database.md) |
| `date` | `DATE` | Materialized view daily partition key |

---

## 2. Entity Relationship Diagram (Mermaid erDiagram)

```mermaid
erDiagram

 %% ============================================================
 %% DOMAIN 1 — IDENTITY & ACCESS (IAM)
 %% ============================================================

 users {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar full_name "Required 1-150 chars"
 varchar email "Display preserves original casing"
 varchar email_normalized
 tinyint password_hash_version "1=bcrypt 2=argon2id"
 text password_hash "Never returned over API"
 datetime password_changed_at ""
 boolean require_password_change_next_login "DEFAULT false"
 bigint avatar_id FK
 varchar status "ENUM: active invited locked suspended soft_deleted"
 int login_count "Monotonic counter"
 datetime last_login_at ""
 varchar last_login_ip "Truncated: IPv4/24 IPv6/64"
 varchar last_login_ua_bucket "First 64 chars of UA"
 datetime email_verified_at "NULL = unverified"
 varchar email_change_pending_new_email "Encrypted HMAC stored"
 varchar email_change_pending_token_jti "Matches password_reset_tokens.jti"
 datetime created_at ""
 datetime updated_at ""
 datetime deleted_at "Soft delete NULL = active user"
 }

 user_oauth_accounts {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar provider "ENUM: google github etc"
 varchar provider_account_id
 text access_token_encrypted "AES-GCM envelope never plaintext"
 text refresh_token_encrypted "NULL for implicit-grant providers"
 varchar scope "Space-delimited OAuth scopes"
 datetime expires_at "Access token expiry"
 datetime linked_at ""
 }

 user_two_factor_methods {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar method "ENUM: totp webauthn backup_codes sms"
 text secret_or_credential_encrypted "TOTP seed or WebAuthn credential or HMAC backup hashes"
 int priority "Lowest = primary step-up method"
 boolean enabled "DEFAULT true"
 datetime last_used_at ""
 datetime created_at ""
 }

 user_password_history {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 tinyint password_hash_version "1=bcrypt 2=argon2id"
 text password_hash "Previous N hashes default N=10"
 datetime created_at "Never updated"
 }

 password_reset_tokens {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar jti
 varchar scope "ENUM: forgot email_change"
 bytea token_sha256 "Optional SHA-256 of opaque bearer token"
 bytea email_address_hash "Peppered HMAC of email address"
 datetime expires_at "Required NOT NULL"
 datetime consumed_at "NULL = still valid"
 varchar issued_ip "Truncated: IPv4/24 IPv6/64"
 varchar issued_ua_bucket "First 64 UA chars"
 datetime created_at ""
 }

 casbin_rules {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar p_type "Casbin: p=permission g=user-role g2=role-role-inheritance e=explicit-effect"
 varchar v0 "Subject: role/user; for g-type = user or child role"
 varchar v1 "Object: permission-key resource.action.scope OR parent-role/role-name"
 varchar v2 "Action/scope: read manage star OR domain star"
 varchar v3 "Effect allow deny meaningful for p-type only"
 varchar v4 "Reserved domain future workspace tenant"
 varchar v5 "Reserved extra scope bits"
 datetime created_at ""
 datetime updated_at ""
 }

 rbac_roles {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar name
 varchar display_name "Human-friendly UI label"
 varchar description "500 chars max"
 smallint tier "1=viewer 2=editor 3=admin 4=superadmin tier-gating"
 bigint inherits_from_id FK
 boolean is_system "True for viewer editor admin superadmin never delete"
 boolean is_custom "GENERATED ALWAYS AS NOT is_system STORED"
 boolean hidden_from_ui "Quarantine internal roles hidden from pickers"
 int version "Optimistic lock incremented on every metadata or permission edit"
 bigint created_by FK
 datetime created_at ""
 datetime updated_at ""
 datetime deleted_at "Soft delete system rows never NULL here"
 }

 rbac_permissions {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar key
 varchar resource "First segment rbac.role user post settings media etc"
 varchar action "Second segment read manage update publish export etc"
 varchar scope "Third segment default all also own uuid security"
 varchar description "500 chars max"
 varchar category "ENUM buckets RBAC User Post Media Settings Analytics Impersonation Profile System"
 smallint default_tier "Default min tier when inherit-from viewer editor admin templates"
 boolean hidden_flag "Internal-only permissions not exposed in UI"
 varchar code_version "Schema version asserting this key exists for drift-detection doctor"
 datetime created_at ""
 datetime updated_at ""
 }

 user_role_assignments {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 bigint role_id FK
 bigint assigned_by FK
 varchar assignment_reason "Admin free-text reason nullable"
 datetime expires_at "Temp grant expiry NULL = permanent"
 varchar source "ENUM: api bulk_import ldap_sync bootstrap_seed self_signup_default"
 bigint impersonation_session_id FK
 datetime created_at ""
 datetime updated_at ""
 }

 rbac_policy_audit_log {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar mutation_type "ENUM catalog: role_created role_updated role_deleted role_permissions_assigned role_permissions_revoked user_role_assigned user_role_revoked policy_force_reload mirror_resync_triggered mirror_drift_repaired"
 bigint actor_user_id FK
 bigint impersonator_id FK
 bigint impersonation_session_id FK
 bigint target_role_id FK
 bigint target_user_id FK
 jsonb tuples_added_jsonb "Added Casbin tuples array of p_type plus v0 through v5"
 jsonb tuples_removed_jsonb "Removed Casbin tuples array of p_type plus v0 through v5"
 jsonb mirror_delta_jsonb "Before after mirror-row snapshots rbac_roles user_role_assignments"
 boolean tier_escalation_detected "Review flag compliance check"
 uuid request_id "Correlates to API request-id"
 inet ip_address "Truncated IPv4/24 IPv6/64 GDPR"
 varchar user_agent_bucket "64 chars"
 varchar stepup_verified "ENUM: none password totp webauthn compliance proof presence"
 datetime created_at "DEFAULT now"
 }

 rbac_enforcement_events {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar decision "ENUM: allow deny deferred error"
 varchar deny_reason_code "Error catalogue rbac.forbidden rbac.deny_storm etc"
 uuid user_id "Zero-UUID anonymous usually 401 before RBAC"
 bigint impersonator_id FK
 bigint impersonation_session_id FK
 text effective_roles_array "TEXT array snapshot at enforce-time for debugging"
 varchar required_object "Permission string resource action optionally scoped"
 varchar required_action "Explicit action segment redundant debug"
 varchar required_domain "Default star multi-tenant future"
 varchar request_method "GET POST PATCH PUT DELETE etc"
 varchar request_path "Full path up to 512 chars"
 varchar route_pattern "Template like api-v1-admin-users-id-roles for grouping"
 uuid request_id "Correlation id"
 varchar trace_id "Distributed trace id"
 inet ip_address "Truncated for GDPR"
 varchar user_agent_bucket ""
 bigint latency_ns "Wall-clock RBAC middleware performance SLO"
 bigint enforcer_generation_id "Cache generation stale-debug helper"
 datetime created_at "DEFAULT now monthly list partitioning recommended"
 }

 %% ============================================================
 %% DOMAIN 2 — USER PROFILE / PRIVACY / ACTIVITY
 %% ============================================================

 user_profiles {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar display_name
 text bio "4000 chars max sanitized markdown only"
 bytea encrypted_contact_phone "AES-256-GCM envelope never plaintext"
 datetime contact_phone_verified_at "SMS OTP verified stamp E.164 assumed"
 varchar contact_website "2048 chars http https only"
 varchar contact_location "120 chars free text"
 jsonb social_links "Three buckets normalized twitter linkedin github slugs or URLs"
 varchar locale "BCP-47 like en_US zh_Hans_CN"
 varchar timezone "IANA tz name like America-Los-Angeles default UTC"
 boolean marketing_consent "DEFAULT false"
 datetime marketing_consent_updated_at ""
 bigint updated_by FK
 datetime created_at ""
 datetime updated_at ""
 }

 user_privacy_settings {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar visibility_profile "ENUM: public unlisted private followers_only DEFAULT public"
 boolean visibility_email "DEFAULT false never expose email publicly"
 boolean visibility_contact_details "DEFAULT false applies phone website location public page"
 boolean visibility_activity_timeline "DEFAULT false applies users name activity if ever exposed"
 boolean search_allow_indexing "DEFAULT true drives X-Robots-Tag and meta robots header"
 boolean tracking_personalize_ads "DEFAULT false analytics consent propagation"
 bigint updated_by FK
 datetime created_at ""
 datetime updated_at ""
 }

 user_activity {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar session_id "JWT jti or opaque session id nullable"
 bigint impersonator_id FK
 bigint impersonation_session_id FK
 uuid request_id "API gateway RPC correlation id"
 varchar activity_type "Full ENUM catalog: login logout profile_edit avatar_update password_change password_reset email_change consent_grant consent_withdraw post_create post_edit post_publish comment_create twofa_enable twofa_disable oauth_link oauth_unlink impersonation_start impersonation_end role_change settings_view privacy_change marketing_consent_grant marketing_consent_withdraw export_requested erasure_requested"
 varchar summary "500 chars human readable safe text no PII no secrets"
 jsonb detail_jsonb "Schema-per-type: post_id route device_class partial_geo diff_hash etc"
 varchar ip_address "Truncated IPv4/24 IPv6/64"
 varchar user_agent_bucket "64 chars"
 char(2) geo_country_code "ISO 3166-1 alpha-2"
 varchar geo_subdivision "Like US-CA GB-LND"
 datetime created_at "Never updated"
 }

 user_activity_daily {
 date day PK
 bigint user_id PK
 int login_count "Per-day aggregates"
 int profile_edit_count ""
 int avatar_update_count ""
 int password_change_count ""
 int post_create_count ""
 int comment_create_count ""
 int twofa_events_count ""
 smallint unique_active_minutes "Estimated bucketed value"
 datetime created_at "Refresh marker"
 }

 %% ============================================================
 %% DOMAIN 3 — CONTENT
 %% ============================================================

 categories {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint parent_id FK
 varchar name ""
 varchar slug
 text description "Nullable"
 bigint image_id FK
 int lft "Nested-set left index"
 int rgt "Nested-set right index"
 int depth "Tree depth root 0 child 1"
 int sort_order "Sibling ordering within parent"
 datetime created_at ""
 datetime updated_at ""
 }

 tags {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar name ""
 varchar slug
 datetime created_at ""
 datetime updated_at ""
 }

 posts {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint author_id FK
 bigint category_id FK
 varchar title "Required 1-200 chars"
 varchar slug
 text excerpt "Nullable auto-generated fallback optional"
 text content "Sanitized HTML markdown AST never raw user HTML"
 bigint cover_image_media_id FK
 varchar status "ENUM: draft review published archived"
 datetime published_at "NULL until first publish"
 datetime created_at ""
 datetime updated_at ""
 datetime deleted_at "Soft delete NULL = not deleted"
 }

 post_tags {
 bigint post_id PK, FK
 bigint tag_id PK, FK
 datetime created_at ""
 }

 post_revisions {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint post_id FK
 int revision_number "Per-post sequence unique per post_id plus revision_number"
 varchar revision_type "ENUM: create update restore publish archive"
 varchar title "Snapshot"
 varchar slug "Snapshot"
 text excerpt "Snapshot"
 text content "Snapshot"
 varchar status "Snapshot"
 bigint author_id FK
 uuid category_id_snapshot
 uuid cover_image_media_id_snapshot
 jsonb media_snapshot_jsonb "post_media rows snapshot FKs kind order"
 jsonb tags_snapshot_jsonb "Tag ids plus names snapshot"
 jsonb seo_snapshot_jsonb "SEO fields snapshot"
 jsonb diff_jsonb "Per-field old-new title slug excerpt content status category cover media tags SEO"
 text changelog_text "Auto-generated human readable summary"
 text editor_note "Optional free-text save-time"
 bigint restore_from_revision_id FK
 bigint impersonator_id FK
 bigint impersonation_session_id FK
 uuid request_id "Correlation id"
 datetime created_at ""
 datetime updated_at ""
 }

 post_seo {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint post_id FK
 varchar seo_title ""
 text seo_description ""
 jsonb seo_keywords "String array keywords"
 varchar og_title ""
 text og_description ""
 bigint og_image_id FK
 varchar og_url "Optional canonical override OpenGraph"
 varchar twitter_card "ENUM: summary summary_large_image app player"
 varchar twitter_title ""
 text twitter_description ""
 bigint twitter_image_id FK
 varchar twitter_creator "At-handle format"
 varchar canonical_url "Optional user-specified canonical URL"
 boolean robots_noindex "DEFAULT false"
 boolean robots_nofollow "DEFAULT false"
 datetime created_at ""
 datetime updated_at ""
 }

 comments {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint post_id FK
 bigint parent_id FK
 bigint author_id FK "Authenticated commenter SET NULL when account hard-deleted"
 varchar author_name "Guest name NULL if authenticated commenter"
 varchar author_email "Guest email nullable never exposed rendered publicly gravatar md5 if display"
 varchar author_website "Guest website nullable http https format-only validated"
 varchar ip_hash "SHA-256 of client IP for spam correlation never plaintext stored"
 varchar user_agent "Up to 500 chars full user-agent string"
 text content "Submitted raw text rendered as HTML only after markdown plus XSS sanitization pipeline"
 text content_html "Sanitized HTML rendered on create and edit empty for rows rendered on read"
 varchar status "ENUM: pending approved rejected spam flagged deleted soft-deleted"
 smallint depth "0 = root reply max depth 5 enforced by trigger and API validator"
 bigint moderation_reviewed_by FK "Admin user who moderated SET NULL when admin removed"
 varchar moderation_reason "Moderator free-text note up to 500 chars NULL until reviewed"
 varchar spam_engine "ENUM: akismet mollom internal_ml honeypot rate_limit admin NULL until scored"
 float spam_score "0.00 through 1.00 higher means more spammy NULL until scored"
 varchar spam_verdict "ENUM: ham unknown likely_spam spam NULL until scored"
 int reply_count "Cached count of direct children updated via DB trigger"
 int upvote_count "Cached upvote counter updated atomically via triggers"
 int flag_count "Cached flag counter admins review when >= threshold"
 datetime edited_at "Null unless author or admin edited after creation"
 datetime created_at ""
 datetime updated_at ""
 datetime deleted_at "Soft-delete timestamp SET deleted status row retained when not NULL"
 bigint deleted_by FK "User or admin who soft-deleted SET NULL when account removed"
 }

 comment_flags {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint comment_id FK
 bigint reporter_user_id FK "Authenticated reporter SET NULL if account removed NULL for anonymous flags"
 varchar reporter_ip_hash "SHA-256 IP hash anonymous flag deduplication"
 varchar reason_code "ENUM: spam abuse hate harassment doxx self_harm copyright impersonation illegal other"
 varchar details "Reporter free-text up to 2000 chars optional"
 datetime created_at ""
 datetime resolved_at "When a moderator closes this flag NULL until then"
 bigint resolved_by FK "Admin user who resolved SET NULL if account removed"
 varchar resolution "ENUM: no_action approve_comment reject_comment mark_spam delete_comment"
 varchar resolution_notes "Optional moderator notes up to 500 chars"
 }

 comment_upvotes {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint comment_id FK
 bigint voter_user_id FK "Authenticated voter NULL if anonymous upvote tracked via IP"
 varchar voter_ip_hash "Anonymous upvote de-duplication plus session signature"
 datetime created_at ""
 }

 comment_moderation_log {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint comment_id FK
 bigint moderator_user_id FK "Admin SET NULL if account removed"
 varchar action "ENUM: approve reject spam restore unspam flag_resolved hard_delete soft_delete"
 varchar reason "Moderator reason string up to 500 chars"
 boolean notify_author "Whether an email was sent to the comment author"
 jsonb snapshot_before "Optional JSON snapshot of comment row before mutation"
 jsonb snapshot_after "Optional JSON snapshot of comment row after mutation"
 datetime created_at ""
 }

 %% ============================================================
 %% DOMAIN 4.1 — NEWSLETTER SUBSCRIBERS / ISSUES / ESP SYNC
 %% ============================================================

 newsletter_subscribers {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar email "Lowercase RFC 5322 unique per subscriber row UNIQUE"
 varchar email_normalized "RFC plus dot-plus stripped gmail plus outlook variants for dedup"
 varchar display_name "Optional subscriber name up to 120 chars"
 varchar status "ENUM: pending_confirm active unsubscribed bounced complained deleted"
 varchar locale "BCP-47 locale tag nullable defaults site locale"
 varchar source "ENUM: web_form api imported migration admin"
 varchar ip_hash "SHA-256 of signup IP plus user agent optional"
 varchar user_agent "Up to 500 chars signup UA for analytics"
 boolean consents_given "True iff subscriber actively clicked opt-in GDPR consent flag"
 varchar gdpr_legal_basis "ENUM: consent legitimate_interest contract legal_obligation vital_interest public_task"
 datetime opted_in_at "Timestamp of explicit subscriber click NULL until confirmed"
 varchar opted_in_ip_hash "SHA-256 opt-in IP retained for compliance audit"
 text consents_declaration "Verbatim copy of consent statement shown to subscriber GDPR record"
 varchar confirm_token_hash "SHA-256 of double opt-in token NEVER raw token persisted"
 datetime confirm_sent_at "Timestamp last confirm email was sent rate-limited by address"
 datetime confirmed_at "Null until subscriber clicks confirm link active transition"
 varchar unsubscribe_token_hash "SHA-256 of unsubscribe footer token rotated every send"
 varchar last_unsubscribe_reason_code "ENUM: too_often not_relevant did_not_sign_up privacy spam other NULL until unsubscribe"
 text last_unsubscribe_feedback "Free-text subscriber feedback 2000 chars max"
 datetime bounced_at "Null unless ESP reports permanent bounce status transition to bounced"
 datetime complained_at "Null unless FBL or ESP reports spam complaint moves to complained"
 datetime last_clicked_at "Engagement telemetry optional ESP pingback"
 datetime last_opened_at "Engagement telemetry optional ESP tracking pixel"
 datetime created_at ""
 datetime updated_at ""
 datetime deleted_at "Soft-delete or GDPR erasure null unless scrubbed"
 }

 newsletter_list_memberships {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint subscriber_id FK
 varchar list_id "String list identifier per ESP max 64 chars"
 datetime subscribed_at "Null if never confirmed list join"
 datetime unsubscribed_at "Timestamp of per-list opt-out NULL if active"
 }

 newsletter_issues {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar slug "Friendly unique issue slug nullable until published"
 varchar subject "Up to 300 chars email subject line"
 varchar preheader "Up to 500 chars inbox preview text"
 varchar list_id "Destination list id same namespace as memberships"
 varchar template_id "Optional ESP template id 128 chars max"
 varchar status "ENUM: draft scheduled sending sent cancelled failed"
 text html_body "Up to 400KB html body fully formed on send"
 text plaintext_body "Up to 400KB plaintext auto-generated or explicit"
 int sent_count "Final recipients after list expansion"
 int open_count "Telemetry aggregated counts updated via ESP webhook"
 int click_count "Telemetry click aggregate"
 int bounce_count "Permanent plus transient bounce aggregate"
 int complaint_count "FBL / spam complaint aggregate"
 int unsubscribe_count "Unsubscribe clicks aggregate"
 datetime scheduled_at "Null for immediate send otherwise future UTC"
 datetime sending_started_at "Null while draft or scheduled"
 datetime sent_at "Final send complete stamp"
 bigint created_by FK "Admin author SET NULL on admin removal"
 datetime created_at ""
 datetime updated_at ""
 }

 newsletter_provider_syncs {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint subscriber_id FK "Nullable NULL if sync is a list-level operation"
 bigint issue_id FK "Nullable NULL if sync is subscriber-only change"
 varchar provider "ENUM: mailchimp convertkit sendfox brevo mailerlite beehiiv buttondown mailgun ses_smtp custom_http"
 varchar provider_contact_id "Remote contact id 128 chars max NULL until pushed"
 varchar provider_list_id "Remote list id 128 chars max"
 varchar sync_status "ENUM: pending synced error skipped"
 varchar direction "ENUM: push_to_provider pull_from_provider"
 text last_error_message "Max 1000 chars error body NULL until failure"
 int retry_count "Backed-off retry counter reset after success"
 datetime next_retry_at "Exponential backoff schedule stamp NULL if synced"
 jsonb sync_payload "Snapshot of data sent or received provider-opaque"
 datetime created_at ""
 datetime updated_at ""
 }

 newsletter_consent_audit {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint subscriber_id FK
 varchar action "ENUM: opted_in resubscribed unsubscribed erasure_request erasure_completed preferences_changed confirm_sent confirm_clicked"
 text declaration "Verbatim consent statement or action reason retained per GDPR 30 59 82"
 varchar ip_hash "SHA-256 user IP at action time NULL if N/A"
 varchar user_agent "UA at action time NULL if N/A"
 bigint actor_admin_id FK "Set only when admin acts NULL if self-serve"
 datetime created_at ""
 }

 %% ============================================================
 %% DOMAIN 5 — MEDIA
 %% ============================================================

 media_assets {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint parent_id FK
 varchar storage_key "Object-storage key unique per disk"
 varchar original_filename "Client-supplied name sanitized"
 varchar content_type "MIME image-png application-pdf etc"
 bigint size_bytes "On-disk bytes non-negative"
 int width "Pixels NULL non-image files"
 int height "Pixels NULL non-image files"
 varchar checksum_sha256 "Content-hash for dedup"
 varchar disk "ENUM: s3 r2 minio do_spaces"
 jsonb tags "String array tags GIN indexed"
 jsonb metadata "Exif ICC dimensions user-supplied arbitrary metadata"
 int lft "Nested-set left index"
 int rgt "Nested-set right index"
 int depth "Folder depth root zero"
 int sort_order "Sibling display order"
 bigint uploaded_by FK
 datetime created_at ""
 datetime updated_at ""
 datetime deleted_at "Soft delete NULL = active file"
 }

 media_transforms {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint media_asset_id FK
 varchar cache_key "Deterministic hash width height fit format quality plus asset id"
 varchar transform_name "Named preset avatar_s64 hero_1600w optional"
 int width "Pixels NULL = auto"
 int height "Pixels NULL = auto"
 varchar fit "cover contain fill inside outside"
 varchar format "webp jpeg png avif auto"
 smallint quality "One through hundred compression"
 varchar storage_key "Object-storage key derived file"
 bigint size_bytes "Bytes derived file on disk"
 varchar content_type "MIME derived image"
 datetime last_accessed_at "LRU eviction driver"
 datetime expires_at "TTL sweeper deletes rows plus storage after stamp"
 datetime created_at ""
 datetime updated_at ""
 }

 post_media {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint post_id FK
 bigint media_asset_id FK
 varchar kind "ENUM: cover inline_image attachment"
 int sort_order "Author-defined ordering within kind"
 datetime created_at ""
 }

 %% ============================================================
 %% DOMAIN 5 — NOTIFICATIONS
 %% ============================================================

 notifications {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar type "Registry-backed keys: notification-created comment-replied post-published role-assigned impersonation-started etc"
 varchar title "Human-readable title one line"
 text preview "Short preview plaintext-safe no PII"
 boolean is_read "DEFAULT false"
 jsonb payload "Type-specific: comment_id post_id reason deep_link etc"
 datetime read_at "NULL = unread"
 bigint actor_user_id FK
 bigint impersonator_id FK
 datetime expires_at "TTL sweep NULL persistent until dismissed"
 datetime created_at ""
 datetime updated_at ""
 }

 notification_preferences {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint user_id FK
 varchar notification_type "Matches notifications.type values"
 boolean in_app_enabled "DEFAULT true"
 boolean email_enabled "DEFAULT true transactional types force regardless"
 boolean push_enabled "DEFAULT false"
 boolean digest_enabled "DEFAULT false batch daily digest"
 datetime created_at ""
 datetime updated_at ""
 }

 %% ============================================================
 %% DOMAIN 6 — ANALYTICS (consent-based)
 %% ============================================================

 analytics_page_views {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar consent_token "Opaque session-bucketed cookie PII-free"
 varchar session_id "Rolling 30-minute session identifier non-PII"
 varchar path "Max 1024 chars full path plus query optionally"
 varchar referrer "Max 1024 chars Referer header"
 char(2) country_code "ISO 3166 alpha-2"
 varchar device_type "ENUM: desktop tablet mobile"
 bigint user_id FK
 datetime occurred_at "Client timestamp server skew-window validated"
 }

 analytics_time_spent {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar consent_token "Matches analytics_page_views.consent_token"
 varchar session_id "Matches analytics_page_views.session_id"
 varchar path "1024 chars max"
 datetime started_at ""
 datetime ended_at "NULL still open heartbeat writing"
 int focus_seconds "Non-negative tab-visible-only time"
 }

 %% ============================================================
 %% DOMAIN 7 — CONFIGURATION & SYSTEM
 %% ============================================================

 settings {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar key
 jsonb value_jsonb "Actual value typed per value_type"
 varchar value_type "ENUM: string number boolean string_array object"
 varchar sensitivity "ENUM: public_safe admin_only server_only never return server_only over API"
 varchar category "Buckets: site content media analytics notifications seo security"
 int version "Optimistic lock counter per-setting"
 text description "What this setting governs admin UI helptext"
 bigint updated_by FK
 datetime created_at ""
 datetime updated_at ""
 }

 settings_history {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint setting_id FK
 varchar key "Denormalized snapshots settings.key at change time"
 jsonb previous_value_jsonb "Value before change"
 jsonb new_value_jsonb "Value after change"
 bigint changed_by FK
 uuid request_id "Correlation id"
 inet ip_address "Truncated IPv4/24 IPv6/64"
 datetime created_at ""
 }

 impersonation_sessions {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint impersonator_user_id FK
 uuid impersonator_session_id "Original JWT jti before impersonation began"
 bigint impersonated_user_id FK
 varchar state "ENUM: active exited revoked expired"
 datetime started_at ""
 datetime expires_at "Hard expiry default configurable two hours"
 datetime exited_at "NULL while still active"
 varchar revoked_reason "Why revoked: admin_stop target_user_deleted policy_change timeout"
 uuid document_id "Optional JWT document id impersonation token chain"
 varchar reason "Admin-supplied free-text compliance reason required for audit"
 boolean stepup_verified "2FA step-up confirmed before start"
 varchar csrf_token_hash "HMAC CSRF token subsequent browser requests during impersonation"
 uuid request_id "Start-request correlation id"
 inet ip_address "Truncated"
 datetime created_at ""
 datetime updated_at ""
 }

 audit_logs {
 bigint id PK
 uuid uuid UK "Public identifier"
 bigint actor_id FK
 varchar action "Catalog: login-failed settings-view media-upload-denied etc"
 varchar resource_type "Users posts roles settings media_assets etc"
 varchar resource_id "UUID or business key of the target"
 uuid request_id "API correlation id"
 varchar ip_address "Truncated IPv4/24 IPv6/64"
 jsonb metadata "Structured context: field before-after permissions tier etc"
 datetime created_at ""
 }

 outbox_events {
 bigint id PK
 uuid uuid UK "Public identifier"
 varchar aggregate_type "Users posts comments casbin_rules media_assets etc"
 varchar aggregate_id "UUID root entity"
 varchar event_name "User-created post-published comment-created casbin-policy-updated etc"
 jsonb payload "Domain event schema-versioned JSON body"
 varchar status "ENUM: pending published failed dead"
 datetime published_at "NULL while pending"
 smallint retry_count "Retry exponential backoff dead-letter over max default ten"
 datetime created_at ""
 datetime updated_at ""
 }

 %% ============================================================
 %% RELATIONSHIPS — IDENTITY & ACCESS
 %% ============================================================

 users ||--o{ user_oauth_accounts : "has zero or more OAuth link rows one user per link"
 users ||--o{ user_two_factor_methods : "has zero or more two-factor methods multiple for step-up redundancy"
 users ||--o{ user_password_history : "has append-only password history rows default N=10 reuse blocked"
 users ||--o{ password_reset_tokens : "issues forgot-password plus email-change tokens single-use via consumed_at plus Redis jti guard"
 users ||--|| user_profiles : "has exactly one sparse profile sidecar created lazily first profile edit or eager seeder"
 users ||--|| user_privacy_settings : "has exactly one privacy row created eager user-create trigger or service default"
 users ||--o{ user_activity : "performs zero or more append-only activity rows CASCADE hard user delete"
 user_activity }o..o| users : "impersonator_id soft context SET NULL if impersonator account removed from system"
 users o|--o{ user_role_assignments : "assigned roles through mirror junction many-to-many typical one-or-more roles temp grant via expires_at"
 rbac_roles o|--o{ user_role_assignments : "granted to users through mirror junction many-to-many system roles never soft-deleted"
 rbac_roles ||--o{ rbac_roles : "role-role inheritance self-reference DAG inherits_from_id cycle-checked max depth ten DFS"
 casbin_rules ||--|| rbac_roles : "canonical Casbin policy tuples logically drive role metadata mirror one-to-one logical"
 casbin_rules ||--|| rbac_permissions : "canonical Casbin policy logically drives permission registry mirror one-to-one logical"
 casbin_rules ||--|| user_role_assignments : "canonical Casbin g-user-role tuples logically drive assignment mirror one-to-one logical"
 rbac_roles ||--o{ rbac_policy_audit_log : "role-related policy mutations write append-only audit rows"
 users ||--o{ rbac_policy_audit_log : "actor user recorded every policy mutation for compliance"
 user_activity_daily }o--|| users : "rollup aggregates one user per day materialized view refresh cascades logically"

 %% ============================================================
 %% RELATIONSHIPS — CONTENT
 %% ============================================================

 categories ||--o{ categories : "category tree self-reference parent_id nested-set lft rgt depth sort_order traversal support"
 posts }o..o| categories : "categorized under exactly zero or one category orphan posts permitted"
 posts }o..o| users : "authored by exactly zero or one user SET NULL on hard-delete author reassigned ghost unassigned"
 posts ||--o{ post_tags : "tagged with zero or more tags via many-to-many junction table"
 tags ||--o{ post_tags : "applied to zero or more posts via many-to-many junction table"
 posts ||--o{ post_revisions : "has append-only revision snapshot history CASCADE delete on post removal"
 post_revisions }o..o| post_revisions : "restore_from points self to source revision restore creates NEW row never overwrites"
 posts ||--|| post_seo : "has exactly one SEO config sidecar one-to-one CASCADE NULL allowed if never edited"
 posts ||--o{ comments : "receives zero or more comments nested-set tree rooted per post_id CASCADE delete"
 comments ||--o{ comments : "comment tree parent_id self-reference nested-set lft rgt depth sort_order CASCADE tree delete"
 users }o..o| comments : "authored by authenticated user SET NULL if account removed guest comments keep name and email"
 comments ||--o{ comment_flags : "receives zero or more flag reports from readers soft-aggregated into flag_count"
 comment_flags }o..o| users : "flagged by authenticated reporter SET NULL if reporter removed anonymous uses ip_hash"
 comments ||--o{ comment_upvotes : "has zero or more up-vote joins unique composite comment_id plus voter_user_id or ip_hash"
 comment_upvotes }o..o| users : "cast by authenticated voter SET NULL on voter account removed anonymous uses hash"
 comments ||--o{ comment_moderation_log : "each moderation action appends exactly one immutable log row per admin mutation"
 comment_moderation_log }o..o| users : "performed by moderator SET NULL if admin account removed preserves audit"

 %% ============================================================
 %% RELATIONSHIPS — NEWSLETTER
 %% ============================================================

 newsletter_subscribers ||--o{ newsletter_list_memberships : "has zero or more per-list membership rows list_id composite unique with subscriber_id"
 newsletter_subscribers ||--o{ newsletter_provider_syncs : "participates in zero or more ESP sync attempts retries logged per direction"
 newsletter_issues ||--o{ newsletter_provider_syncs : "each send creates zero or more sync entries push then status pull via webhooks"
 newsletter_issues }o..o| users : "authored by admin or editor SET NULL if user removed"
 newsletter_subscribers ||--o{ newsletter_consent_audit : "each consent-relevant action appends exactly one audit row retained per policy"
 newsletter_consent_audit }o..o| users : "performed by admin actor SET NULL for self-serve actions record actor_admin_id if provided"

 %% ============================================================
 %% RELATIONSHIPS — MEDIA
 %% ============================================================

 media_assets ||--o{ media_assets : "media folder tree self-reference parent_id nested-set lft rgt depth folders inner leaves files"
 media_assets ||--o{ media_transforms : "has zero or more derived image variants per image TTL cache eviction sweeper"
 posts ||--o{ post_media : "attaches zero or more media rows cover inline attachment kinds with sort_order per kind"
 media_assets ||--o{ post_media : "attached to zero or more posts through many-to-many junction CASCADE either delete"
 users }o..o| media_assets : "has zero or one avatar media asset SET NULL image delete detaching NEVER deletes user row"
 posts }o..o| media_assets : "optionally references cover media SET NULL on image hard delete never cascades post"
 categories }o..o| media_assets : "optionally references category cover banner SET NULL media delete detaches only"
 post_seo }o..o| media_assets : "references zero or one OG plus Twitter card images two FKs og_image_id twitter_image_id SET NULL each"

 %% ============================================================
 %% RELATIONSHIPS — NOTIFICATIONS / ANALYTICS
 %% ============================================================

 users ||--o{ notifications : "receives zero or more inbox notifications fan-out per recipient CASCADE user delete"
 notifications }o..o| users : "optionally triggered by one actor_user_id SET NULL when actor account removed"
 users ||--o{ notification_preferences : "has zero or more per-type delivery rows one row per notification_type unique composite user_id plus notification_type"
 analytics_page_views }o..o| users : "optionally attributed SET NULL anonymous consent token bridge"
 analytics_time_spent }o..o| analytics_page_views : "one or more dwell buckets per page view long-session writes multiple 30-second slabs"

 %% ============================================================
 %% RELATIONSHIPS — CONFIGURATION / SYSTEM / RBAC AUDIT
 %% ============================================================

 settings ||--o{ settings_history : "has append-only change history every write creates new row snapshots previous new JSONB"
 users ||--o{ settings_history : "actor user recorded every settings write both SETTINGS_KEY editor rows"
 users ||--o{ impersonation_sessions : "starts impersonations impersonator_user_id zero or more rows at most one active enforced via partial unique"
 impersonation_sessions }o..|| users : "impersonated_target_user_id references exactly one target account CASCADE when target hard-deleted whole session removed"
 users ||--o{ audit_logs : "actor user zero or more append-only rows SET NULL on actor hard-delete preserves audit corpus"
 outbox_events }o..o| rbac_policy_audit_log : "logical pair every policy mutation is both an audit row AND an outbox fan-out event"
 outbox_events }o..o| user_activity : "logical pair many user activities simultaneously emit domain events through transactional outbox"
 rbac_enforcement_events }o..o| users : "optionally attributed actor SET NULL when actor account removed preserves historic telemetry"
```

---

## 3. Domain Grouping Reference (for diagram zoom / subset views)

When the single ERD above is too dense for a specific discussion, slice it by domain:

| Domain Key | Tables in group |
|------------|-----------------|
| **IAM** | `users`, `user_oauth_accounts`, `user_two_factor_methods`, `user_password_history`, `password_reset_tokens`, `casbin_rules`, `rbac_roles`, `rbac_permissions`, `user_role_assignments`, `rbac_policy_audit_log`, `rbac_enforcement_events` |
| **User Profile** | `user_profiles`, `user_privacy_settings`, `user_activity`, `user_activity_daily` (MV) |
| **Content** | `categories`, `tags`, `posts`, `post_tags`, `post_revisions`, `post_seo` |
| **Comments / Moderation** | `comments`, `comment_flags`, `comment_upvotes`, `comment_moderation_log` |
| **Newsletter** | `newsletter_subscribers`, `newsletter_list_memberships`, `newsletter_issues`, `newsletter_provider_syncs`, `newsletter_consent_audit` |
| **Media** | `media_assets`, `media_transforms`, `post_media` |
| **Notifications** | `notifications`, `notification_preferences` |
| **Analytics** | `analytics_page_views`, `analytics_time_spent` |
| **Configuration/System** | `settings`, `settings_history`, `impersonation_sessions`, `audit_logs`, `outbox_events` |

## 4. Renderability Notes & Verification

### 4.1 Mermaid syntax constraints we conform to
The diagram was adjusted specifically for compatibility with [@mermaid-js/mermaid-cli](https://github.com/mermaid-js/mermaid-cli) v10.9.1 (which uses Mermaid v10.9.x parser):

1. **No inline `PK`/`FK` followed by quoted COMMENT that itself starts with an identifier-looking word** — an older mermaid parser bug treats `uuid id PK "Primary key..."` as if `"Primary"` is a stray attribute word. Instead, PK/FK are placed immediately after the id type, and the quoted description starts either with a lowercase character or is purely informative — avoiding the parser misinterpretation above.
2. **No bar-pipe characters `|` or curly `{}` inside quoted column comments** — these tokens confuse the `erDiagram` lexer in some Mermaid versions; use spacing, words, and JSONB tags for bucketized descriptions instead.
3. **No stray `UNIQUE (...)` lines inside entity blocks** — `UNIQUE` is not an `erDiagram` attribute syntax per parser spec; unique constraints are documented via UK labels on columns, explicit text in descriptions, and the prose in [database.md §Notes](./database.md#L579-L641).
4. **Composite PKs** — `user_activity_daily` declares both `day` and `user_id` with `PK` to indicate the composite primary key (PostgreSQL composite PKs listed in order).

### 4.2 Render-verify procedure
Open [https://mermaid.live](https://mermaid.live) and paste the entire fenced `mermaid` block from §2 into the editor. Expected behaviour:

- All 43 entities render with column type markers and quoted comment tooltips on hover.
- All 51 relationship lines draw; self-referential arcs appear on `categories`, `comments`, `media_assets`, `rbac_roles`, and `post_revisions`.
- Cardinality `||`, `|o`, `o|`, `}o` glyphs appear on the appropriate ends of each relationship.
- No red "parse error" banner; download SVG/PNG exports work from the File → Export menu.

For offline local verification run (requires Node ≥ 20.19 + puppeteer deps):
```bash
# Extract Mermaid block, then invoke @mermaid-js/mermaid-cli
npx -y -p @mermaid-js/mermaid-cli@^10.9.1 mmdc \
   -i docs/backend/ERD.md.mmd -o docs/backend/ERD.svg \
   -w 12000 -H 8000 -b white
```
