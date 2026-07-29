# Settings Management

## Feature Summary

Provide an admin-only settings surface for non-secret documentation/blog configuration. Settings are retrieved via `GET` and modified via `PUT`, with server-side validation, allowlist enforcement, audit logging, and strong RBAC.

This feature is about configuration that product admins can safely change without deployment or database access. It is **not** a mechanism for runtime secrets, infrastructure config, or environment-level parameters.

## In Scope

- read non-sensitive current settings via admin API
- update authorized settings keys via admin API
- validate every update against allowed keys, types, and value ranges
- enforce admin-only authentication and role-based permissions
- audit every change with actor, previous value, new value, and request metadata
- keep current and previous values queryable through change history (audit + outbox events)

## Out of Scope

- secret/credential management (secrets stay in env vars)
- environment-level infrastructure configuration (database URLs, Redis addresses, bucket credentials)
- per-user preferences that should live in user profiles
- ad-hoc schema changes or arbitrary key creation via API

## User Roles

- **admin**: can read and update settings and export setting history if enabled
- **editor/author/moderator**: read access only to explicitly public settings if exposed (default: no access)
- **public readers**: no access

## Permissions

Add to role/permission catalog:

- `settings.read` — allowed to fetch current settings
- `settings.update` — allowed to modify authorized settings keys
- `settings.history.read` — allowed to inspect historical changes

Default policy:

- `admin` role holds all three permissions
- other roles hold no default settings permissions

## Access Rules

- authentication required for all settings endpoints (no public GET/PUT)
- authorization enforced by server-side middleware + service layer checks
- even when middleware blocks access, services must re-check permissions
- high-sensitivity settings (those that affect behavior site-wide) may require 2FA step-up if configured
- CORS and CSRF protections apply to both GET and PUT where the client is browser-based

## Settings Categories

Split configurable keys into groups for validation and UI grouping:

- `site`
  - site name, tagline, default locale, timezone
  - public site URL, canonical base URL
- `content`
  - default post status
  - comments enabled globally on/off
  - moderation mode (disabled / opt-in / opt-out / strict)
- `media`
  - max upload size bytes
  - allowed content types
  - default transform quality
  - malware scanning enabled flag
- `analytics`
  - analytics enabled flag
  - consent required flag
  - default retention days for raw events
- `notifications`
  - default sender name/address for email
  - moderation notification recipients (roles or user IDs, allowed list only)
  - digest cadence where applicable
- `seo`
  - default SEO title template
  - default SEO description
  - default social share image URL or media asset ID
- `security` (admin-only subset)
  - allowed login domains for organization mode (if implemented)
  - password minimum length / complexity flags
  - session lifetime configuration within safe bounds
  - 2FA required for high-risk roles toggle
- `storage`
  - default provider (R2 / S3 / S3-compatible / filesystem)
  - bucket/container name
  - region / endpoint / path-style toggle
  - public base URL for serving assets
  - storage class / tier defaults
  - CORS allowed origins for direct browser uploads
  - lifecycle retention / object expiration rules in days
  - server-side encryption flag
  - signed-URL TTL seconds for private objects
  - multipart upload config (part size, concurrency)
- `smtp`
  - SMTP enabled global toggle
  - SMTP host
  - SMTP port (25/465/587/2525)
  - encryption mode (none / STARTTLS / SMTPS / implicit TLS)
  - authentication required toggle
  - SMTP username (for auth) — never returned in GET responses
  - SMTP password (for auth) — never returned in GET responses
  - default sender (from) name and address
  - default reply-to address
  - bounce / complaint / delivery notification email
  - connection pool size
  - timeout seconds (connect / read / write)
  - retry + backoff configuration
  - test-send recipient for UI health checks

## Sensitivity Rules

Each key declares a sensitivity level:

- `public-safe`: allowed in a future public settings endpoint if ever exposed
- `admin-only`: allowed in admin GET response only
- `server-only`: never returned by GET endpoints; used only for internal defaults and future migrations

## Update Rules

PUT behavior:

- partial update semantics (only submitted keys are validated and applied)
- rejected unknown keys outright; do not silently ignore
- each key must have:
  - data type (string, number, boolean, string[] enum, JSON object with schema)
  - allowlist for enums
  - min/max where numeric
  - string regex or length constraints where applicable
- write to transaction:
  1. validate request
  2. permissions check
  3. apply updates atomically
  4. write audit entries
  5. emit `settings.updated` outbox event(s)
- return updated settings subset plus a change summary in the response envelope

## Audit Requirements

For every PUT request, record in the audit log at minimum:

- actor_id
- request_id
- ip_address
- user_agent bucket
- keys changed
- per-key previous value (redacted if sensitivity rules require)
- per-key new value (redacted if sensitivity rules require)
- timestamp
- status of the change (success / validation_failed / permission_denied)

Audit retention and access rules follow the platform’s existing audit log policy.

## Consistency Rules

- settings apply globally unless explicitly introduced as per-scope overrides later
- if a key is known but missing in storage, application uses its coded default
- storage is the source of truth; cache is invalidated on every successful change
- deployment or environment-level config always wins over stored settings for sensitive infra values

## Testing Requirements

Feature-level tests:

- GET returns non-sensitive keys for users with `settings.read`
- GET returns 401/403 for missing auth or missing permissions
- PUT with valid keys updates values correctly
- PUT with unknown keys returns validation error
- PUT with out-of-range values returns validation error
- PUT with no `settings.update` permission returns 403
- audit log entries are created for every PUT attempt (success and failure)
- concurrent updates for the same key remain consistent (optimistic locking or explicit version check)
- cached settings are invalidated on successful updates

## Category Specification: Storage Configuration (`storage.*`)

### Purpose

Admin-controlled storage behavior for the media module (R2 / S3 / S3-compatible providers / filesystem) while keeping *credentials* (access key / secret key / account ID) in environment variables or secret managers (never in settings). Storage category settings are the non-secret levers that a product admin can safely tune: provider selection, bucket, public URL, lifecycle, encryption, and upload tuning.

### Data Structure and Key Catalog

| Key                                      | Value Type        | Default                          | Sensitivity   |
|------------------------------------------|-------------------|----------------------------------|---------------|
| `storage.default_provider`               | string enum       | `r2`                             | admin-only    |
| `storage.r2.bucket`                      | string            | blog-media                       | admin-only    |
| `storage.r2.account_id_domain_alias`     | string hostname   | (derived from env if unset)      | admin-only    |
| `storage.r2.public_base_url`             | string URL        | https://assets.example.com       | public-safe   |
| `storage.s3.provider_name`               | string            | aws_s3                           | admin-only    |
| `storage.s3.bucket`                      | string            | blog-media                       | admin-only    |
| `storage.s3.region`                      | string            | us-east-1                        | admin-only    |
| `storage.s3.endpoint_url`                | string URL        | null (official S3)               | admin-only    |
| `storage.s3.force_path_style`            | boolean           | false                            | admin-only    |
| `storage.s3_compatible.provider_name`    | string            | minio                            | admin-only    |
| `storage.s3_compatible.endpoint_url`     | string URL        | https://minio.internal:9000      | admin-only    |
| `storage.s3_compatible.bucket`           | string            | blog-media                       | admin-only    |
| `storage.s3_compatible.region`           | string            | us-east-1                        | admin-only    |
| `storage.s3_compatible.force_path_style` | boolean           | true                             | admin-only    |
| `storage.filesystem.root_dir`            | string path       | /var/lib/blog/media              | admin-only    |
| `storage.filesystem.public_base_url`     | string URL        | /media                           | public-safe   |
| `storage.default_storage_class`          | string enum       | standard                         | admin-only    |
| `storage.cors.allowed_origins`           | string[] URL      | [https://admin.example.com]      | admin-only    |
| `storage.cors.allowed_methods`           | string[] HTTP     | [GET,PUT,POST,DELETE]            | admin-only    |
| `storage.cors.allowed_headers`           | string[]          | [Authorization,Content-Type]     | admin-only    |
| `storage.cors.max_age_seconds`           | number int        | 3600                             | admin-only    |
| `storage.lifecycle.uploaded_expire_days` | number int        | 0 (never expire)                 | admin-only    |
| `storage.lifecycle.trash_expire_days`    | number int        | 30                               | admin-only    |
| `storage.encryption.server_side_enabled` | boolean           | true                             | server-only   |
| `storage.encryption.default_algorithm`   | string enum       | AES256                           | server-only   |
| `storage.presigned_url.ttl_seconds`      | number int        | 900 (15 min)                     | admin-only    |
| `storage.presigned_url.ip_restricted`    | boolean           | false                            | admin-only    |
| `storage.multipart.min_part_size_bytes`  | number int        | 8388608 (8 MiB)                  | admin-only    |
| `storage.multipart.max_parts`            | number int        | 10000                            | admin-only    |
| `storage.multipart.concurrency`          | number int        | 4                                | admin-only    |
| `storage.upload.max_concurrent_per_user` | number int        | 8                                | admin-only    |
| `storage.cache.variant_ttl_seconds`      | number int        | 15552000 (180 days)              | public-safe   |

Notes:

- `storage.*.bucket` regex: `^[a-z0-9][a-z0-9\-_\.]{1,61}[a-z0-9]$` (AWS bucket naming safe, also works for R2).
- Hostname regex for `endpoint_url`, `public_base_url`: RFC 3986 URL validation with permitted schemes https (or http for local minio).
- `filesystem.root_dir` must be absolute path; reject traversal sequences `..`.
- `storage.default_provider` enum: `r2`, `s3`, `s3_compatible`, `filesystem`.
- `storage.default_storage_class` enum: `standard`, `infrequent_access`, `archive`, `reduced_redundancy`.

### Validation Rules

Per `PUT /api/v1/admin/settings`:

- **Provider switching:** If caller changes `storage.default_provider`, require the selected provider's `bucket` + endpoint/region keys to also be present and valid. Return a grouped validation error if required keys for the new provider are missing.
- **Cross-field:**
  - When `encryption.server_side_enabled` is true, `default_algorithm` is required and must be in the enum set.
  - `filesystem` provider requires `filesystem.root_dir` to be set and to pass `filepath.IsAbs()` + no-traversal check.
  - `presigned_url.ttl_seconds` 60 ≤ TTL ≤ 604800 (1 min to 7 days).
  - `multipart.min_part_size_bytes` `5*1024*1024` ≤ value ≤ `5*1024*1024*1024`.
  - `cors.allowed_origins` entries must be valid URLs with no trailing slash (or `*` explicitly allowed only when a security override flag is true in server-only defaults).
- **Type coercion disabled:** booleans, numeric, arrays must match declared types exactly. Do not accept `"true"` as boolean or `["https://x"]` length validation by string length alone.
- **Array max lengths:** `cors.allowed_*` arrays capped at 50 items.
- **Default application:** If a key is missing but has a coded default, the PUT validator accepts that keys are absent and does not treat missing as an error.

### UI Elements

Admin Settings → Storage tab:

1. **Provider selector:** Radio group: R2 / AWS S3 / S3 Compatible / Local filesystem.
2. **Provider card (conditional):** Form fields change based on radio. Fields with `server-only` sensitivity must **never** appear in client-side form definitions rendered to the browser. Server-only keys are only configured via environment/deployment.
   - Bucket (text, regex helper + placeholder).
   - Region (text, select for known AWS/Cloudflare regions when applicable).
   - Endpoint URL (text, validates on blur).
   - Force path-style (toggle, with toolip for MinIO users).
   - Public base URL (text, pre-populated if derivable).
   - Root directory (for filesystem; absolute path validator).
3. **Storage class** select, disabled for providers that do not support classes.
4. **CORS panel:** origins/methods/headers/max_age tag editors with validation badges.
5. **Lifecycle section:** two numeric fields for uploaded expire days and trash expire days, with tooltip explaining 0 = never.
6. **Presigned URL section:** TTL slider from 60s to 7d + ip-restricted toggle.
7. **Multipart upload tuning:** min part size, max parts, concurrency (3 number fields with min/max helper text).
8. **Actions row:**
   - `Test Connection` button → calls POST `/api/v1/admin/settings/storage/test-connect` (returns OK if provider ping works; 422 with structured validation otherwise).
   - `Save Changes` → standard PUT with CSRF header.
9. **Responsive layout:** Single column on mobile; two-column grid on ≥ md (provider card + tuning side by side, CORS and lifecycle stacked below).
10. **Field grouping labels:** All inputs show sensitivity badges: `Public-safe`, `Admin-only`, `Server-only (disabled, set via env)`.

### Permission Controls

- All `storage.*` reads require base permission `settings.read`.
- All `storage.*` writes require base permission `settings.update`.
- Additional granular permissions if admins want to delegate storage separately:
  - `settings.storage.read` — allows fetching only storage-category keys (useful for a storage operator role).
  - `settings.storage.update` — allows modifying only storage-category keys; superseded by `settings.update` if the caller holds both.
- `2FA step-up` is required for writes to:
  - `storage.default_provider`
  - `storage.*.bucket`
  - `storage.lifecycle.uploaded_expire_days` (non-zero change)
  - `storage.encryption.server_side_enabled`
  - Reasoning: these keys have potential to make data unavailable or expose existing assets publicly.

### Persistence Mechanisms

- Written through standard settings transaction:
  1. `settings` upsert by key, version incremented;
  2. `settings_history` row captures per-key previous/new JSONB (for server-only keys the `previous_value` / `new_value` are set to JSONB `null` and `changed_by` is still recorded for audit even though env is the final source of truth).
- Secrets (R2/S3 access key IDs and secret access keys) are **never** stored in `settings` table — services read them from env/config. The settings module only affects *behavior* knobs and name/URL configuration.
- On successful PUT, cache invalidation keys:
  - `settings:snapshot:v1`
  - `settings:by_category:storage`
  - `media:storage_config:v1` (custom downstream cache key used by media module)

### Architecture Integration

- **Hexagonal mapping:** Storage category schema lives in `internal/core/settings/domain.SettingSchema` with an allowlist of `storage.*` keys. The media outbound port (`MediaStoragePort`) reads current settings via `SettingsReaderPort` during adapter initialization and on every `GetConfig()` call; the HTTP inbound handler never constructs a storage client directly.
- **Provider client factory:** `internal/adapters/outbound/storage` factory selects implementation based on `storage.default_provider` value combined with env credentials. Settings values are merged at runtime with environment overrides; env always wins for credentials (access key, secret key, account IDs), settings win for bucket, endpoint, URL, and tuning.
- **Events:** On `settings.updated` containing any `storage.*` key, emit:
  - `media.storage_config.invalidated` → invalidates variant cache, forces refresh of storage adapter in next request pool checkout;
  - `media.cors_rules.updated` → async re-apply bucket CORS if the provider SDK supports it (R2/S3/MinIO bucket CORS PATCH);
  - `media.lifecycle_updated` → async re-apply lifecycle rules if applicable.
- **Transactional test boundary:** All `storage.*` writes participate in the same DB transaction as the settings update + history + outbox; if event publishing fails, the write rolls back.

## Category Specification: SMTP Email Service (`smtp.*`)

### Purpose

Admin-controlled SMTP client behavior: SMTP host, port, encryption, authentication toggles, tuning/timeouts, retry policy, and default sender/reply-to addresses. The SMTP *username* and *password* are accepted via PUT for convenience but are persisted with `server-only` sensitivity (never returned in GET responses; never cached outside isolated SMTP worker memory).

### Data Structure and Key Catalog

| Key                                          | Value Type      | Default                       | Sensitivity   |
|----------------------------------------------|-----------------|-------------------------------|---------------|
| `smtp.enabled`                               | boolean         | true                          | admin-only    |
| `smtp.host`                                  | string hostname | smtp.example.com              | admin-only    |
| `smtp.port`                                  | number int enum | 587                           | admin-only    |
| `smtp.encryption`                            | string enum     | starttls                      | admin-only    |
| `smtp.auth.required`                         | boolean         | true                          | admin-only    |
| `smtp.auth.username`                         | string          | (empty)                       | **server-only** |
| `smtp.auth.password`                         | string          | (empty)                       | **server-only** |
| `smtp.defaults.from_name`                    | string          | Blog                          | admin-only    |
| `smtp.defaults.from_address`                 | string email    | no-reply@example.com          | admin-only    |
| `smtp.defaults.reply_to_address`             | string email    | support@example.com           | admin-only    |
| `smtp.defaults.subject_prefix`               | string          | [Blog]                        | admin-only    |
| `smtp.bounce.address`                        | string email    | bounces@example.com           | admin-only    |
| `smtp.complaints.address`                    | string email    | complaints@example.com        | admin-only    |
| `smtp.delivery_reports.address`              | string email    | delivery@example.com          | admin-only    |
| `smtp.pool.size`                             | number int      | 5                             | admin-only    |
| `smtp.pool.idle_timeout_seconds`             | number int      | 60                            | admin-only    |
| `smtp.timeout.connect_seconds`               | number int      | 10                            | admin-only    |
| `smtp.timeout.read_seconds`                  | number int      | 30                            | admin-only    |
| `smtp.timeout.write_seconds`                 | number int      | 30                            | admin-only    |
| `smtp.retry.attempts`                        | number int      | 3                             | admin-only    |
| `smtp.retry.initial_backoff_seconds`         | number int      | 1                             | admin-only    |
| `smtp.retry.max_backoff_seconds`             | number int      | 300                           | admin-only    |
| `smtp.retry.backoff_multiplier`              | number float    | 2.0                           | admin-only    |
| `smtp.send.rate_per_minute_per_worker`       | number int      | 120                           | admin-only    |
| `smtp.tls.insecure_skip_verify`              | boolean         | false                         | server-only   |
| `smtp.tls.min_version`                       | string enum     | TLSv1.2                       | server-only   |
| `smtp.headers.custom`                        | JSON object     | {}                            | admin-only    |
| `smtp.test.recipient_address`                | string email    | admin-test@example.com        | admin-only    |
| `smtp.tracking.open_enabled`                 | boolean         | false                         | admin-only    |
| `smtp.tracking.click_enabled`                | boolean         | false                         | admin-only    |
| `smtp.tracking.include_message_id_header`    | boolean         | true                          | admin-only    |

Notes:

- `smtp.port` allowed values: `{25, 465, 587, 2525}`. Validate in cross-field with `smtp.encryption` (see below).
- `smtp.encryption` enum: `none`, `starttls`, `smtps`, `implicit_tls`. Port + encryption combos checked as cross-field validation.
- Email fields use RFC-5322 validation (length, `local-part@domain` structure, TLD allowlist optional).
- `smtp.auth.username` / `smtp.auth.password` are accepted in PUT bodies but **never** echoed back in GET response payloads. History and audit rows for these keys MUST redact the actual value (replace with `{ "redacted": true }` JSONB marker). Do not log in any debug logs.

### Validation Rules

- **Required fields when `smtp.enabled=true`:** `host`, `port`, `encryption`, `defaults.from_address`.
- **Cross-field port / encryption matrix:**
  - `encryption=none` → allowed ports: 25, 2525. Warning emitted in response `meta.warnings` if used.
  - `encryption=starttls` → allowed ports: 587, 2525.
  - `encryption=smtps` / `implicit_tls` → allowed ports: 465.
- **Authentication required:**
  - When `auth.required=true`, `auth.username` must be a non-empty string. `auth.password` must be non-empty at save time (PUT). If the caller submits PUT with all other smtp keys and omits password, and the stored password already exists, accept the update without requiring password re-entry using an allowlist rule keyed by `{username_set, password_set}` flags.
- **Sender addresses:** from + reply-to must be valid email addresses; local parts ≤ 64, domains with valid TLDs.
- **Retry fields:** 0 ≤ `attempts` ≤ 10; `initial_backoff` ≤ `max_backoff`; `backoff_multiplier` 1.0 to 4.0.
- **Rate fields:** `rate_per_minute_per_worker` 1–10000.
- **Timeouts:** connect 1–30s, read 5–300s, write 5–300s. Pool size 0 (disable pool) to 50.
- **Custom headers JSON object:** keys must match `^[A-Za-z][A-Za-z0-9\-]*$`, non-empty string values. Max 20 headers.
- **Tracking toggles:** warn in `meta.warnings` if open/click tracking is enabled but no `public_base_url` SEO/site config is set to render tracking pixel URLs.
- **Server-only coercion:** Never allow GET to return `smtp.auth.username`, `smtp.auth.password`, `smtp.tls.insecure_skip_verify`, `smtp.tls.min_version`. Treat them as server-only: read-only via service layer by outbound adapters, never in HTTP responses.

### UI Elements

Admin Settings → Email / SMTP tab:

1. **Enabled toggle** + connection card:
   - Hostname (text with DNS-lookup badge on blur).
   - Port selector (25/465/587/2525) with encryption radio auto-suggesting the recommended combo (587 + STARTTLS by default).
   - Encryption mode radio group with help text.
2. **Authentication panel (collapsible):**
   - Auth required toggle.
   - Username (text).
   - Password (password field with show/hide toggle, plus "Leave unchanged if blank" helper when updating).
   - Note "Server-only — never re-displayed" under the password input.
3. **Default addresses panel:**
   - From name, From address, Reply-to, Subject prefix.
4. **Bounces / complaints / DSN addresses** (3 email fields).
5. **Tuning panel:**
   - Pool size, idle timeout, connect/read/write timeout sliders with min/max.
   - Retry attempts, initial/max backoff, multiplier with live-preview retry timeline visualization.
   - Rate per minute per worker number input with graph preview.
6. **Advanced (collapsed by default):**
   - Custom headers key-value editor.
   - TLS min version (visible but disabled badge with "Configured via env" if server-only).
   - Insecure skip verify: disabled in browser client UI with locked badge (server-override only); never renders as editable.
7. **Tracking section:**
   - Open / click tracking toggles, message-id header toggle.
8. **Test section:**
   - Test-send recipient email pre-populated from `smtp.test.recipient_address`.
   - `Send Test Email` → calls POST `/api/v1/admin/settings/smtp/test-send` (enqueues test job; returns request ID + 202). SSE or polling endpoint reports the attempt result.
9. **Actions row:**
   - `Send Test Email`
   - `Save Changes` (CSRF required)
   - `Revert to Defaults` (restores coded defaults)
10. **Responsive layout:** Single column on mobile; two columns (connection + tuning) ≥ md, advanced/tracking stacked below. Server-only fields render as read-only locked info cards with lock icons and `Env-managed` label.

### Permission Controls

- Base: `settings.read` for GET of non-server-only `smtp.*` keys; `settings.update` for PUT.
- Granular delegation permissions for an email operator role:
  - `settings.smtp.read` — list only SMTP category keys.
  - `settings.smtp.update` — modify only SMTP category keys.
  - `settings.smtp.test_send` — needed to trigger the test-send endpoint (separate from general update so read-only users can still validate config).
- 2FA step-up required on PUT when changing:
  - `smtp.host`, `smtp.port`, `smtp.encryption`
  - `smtp.auth.required` toggle (true→false or false→true)
  - `smtp.defaults.from_address`
  - `smtp.tls.insecure_skip_verify` (if env allows changes)
- PUT attempts to change `smtp.tls.insecure_skip_verify` or `smtp.tls.min_version` via admin API are rejected unless the caller also submits a signed admin-override JWT issued via special server-to-server CLI (this is an exceptional flow; default behavior is treat these as immutable via HTTP).

### Persistence Mechanisms

- Standard `settings` + `settings_history` tables; additional rules:
  - For `smtp.auth.username` and `smtp.auth.password` (sensitivity server-only):
    - Upsert to DB **as encrypted JSONB using env-managed encryption key** (KMS env key id per deployment).
    - History rows capture redacted markers for `previous_value` / `new_value` instead of the raw secret.
  - `smtp.tls.*` server-only keys: stored only if the deployment accepts server-override via PUT; otherwise, values are read directly from env and settings rows are ignored.
- Cache policy:
  - Redis key `settings:by_category:smtp` is **never cached at the HTTP response layer** because server-only values must not leak.
  - Internal SMTP adapter reads via `SettingsReaderPort` with an in-memory cache (TTL 60s) bounded to the worker process; never shared.
- On `settings.updated` with `smtp.*` keys:
  - Drop the SMTP connection pool; force fresh connections to pick up new host/port/encryption.
  - Queue async health probe (if enabled) to confirm the new config works.

### Architecture Integration

- **Hexagonal mapping:** The Email Service inbound port (`internal/core/email/ports/inbound/EmailServicePort`) declares a `ReloadConfig()` method triggered when settings change. The SMTP outbound adapter under `internal/adapters/outbound/email/smtp` reads the current `smtp.*` values through `SettingsReaderPort` on reload; HTTP handlers never build SMTP clients.
- **Factory:** SMTP client pool factory selects plain / STARTTLS / SMTPS transports per `smtp.encryption`; env vars override credentials (`SMTP_AUTH_USERNAME`, `SMTP_AUTH_PASSWORD`) if set. Settings values apply for non-credential parameters, and env wins for auth only when non-empty.
- **Worker integration:** Job queue `email.send.*` workers refresh their SMTP client from settings on every `settings.updated` event using the settings cache invalidation hook.
- **Events:** On `settings.updated` containing `smtp.*` keys:
  - `email.smtp_config.updated` — consumed by connection pool manager;
  - `email.smtp.test.completed` — emitted after `/smtp/test-send` job finishes (linked via request ID for admin UI SSE).
- **Audit trail:** PUT attempts for `smtp.auth.password` / username emit audit rows with redacted values; the attempt success/failure status, IP, and actor are still tracked. `email.send.*` delivery attempts reference current `smtp.config.version` for issue triage.

## Additional Permissions Reference

Consolidated new permissions keys to add to `internal/core/rbac` catalog (in addition to existing `settings.read/update/history.read`):

| Permission Key                 | Default Holder | Purpose                                                        |
|--------------------------------|----------------|----------------------------------------------------------------|
| `settings.storage.read`        | admin          | Read-only storage category delegation                          |
| `settings.storage.update`      | admin          | Modify storage keys delegation                                 |
| `settings.smtp.read`           | admin          | Read-only SMTP category delegation                             |
| `settings.smtp.update`         | admin          | Modify SMTP keys delegation                                    |
| `settings.smtp.test_send`      | admin          | Allowed to trigger `/smtp/test-send` endpoint                  |

## Cross-Reference to Backend Architecture Norms

- Follows `docs/backend/settings.md` for endpoints, partial PUT rules, optimistic locking, audit row layout, Watermill outbox events, and Redis caching strategy.
- 2FA step-up follows `docs/architecture/security.md` TOTP step-up patterns for `security.*` keys; the same middleware applies to flagged storage/SMTP keys.
- Redis cache invalidation key patterns and Watermill event topics follow the conventions in `docs/backend/events.md` (`settings.updated` with sub-topic consumer routing).
- Server-only sensitivity rules align with `backend/settings.md` sensitivity tiers (public-safe / admin-only / server-only), ensuring SMTP auth and storage encryption algorithm settings never leak via admin GET or cache keys.
- Settings `version` optimistic locking per row ensures concurrent edits to the same storage/SMTP key return a semantic conflict error, matching the test requirement for concurrency safety.

## Extended Testing Requirements (Storage + SMTP)

Append the following feature-level tests to the existing list:

### Storage
- PUT successfully toggles provider and validates required provider subkeys are present.
- PUT rejects `filesystem.root_dir` with `..` sequences and non-absolute paths.
- PUT rejects invalid bucket names (uppercase, trailing dot, underscore outside allowed ranges).
- PUT rejects presigned TTL outside 60s–7d range.
- Provider switch emits `media.storage_config.invalidated` outbox event.
- Server-only encryption keys are not returned in GET responses (even for admins with `include_sensitive_admin=true`).
- CORS arrays with length > 50 return validation error with structured violation count.

### SMTP
- PUT accepts auth fields in request body but GET never echoes them, history redacts them.
- Port + encryption mismatches produce cross-field validation errors.
- Enabling `smtp.enabled` without `from_address` returns a required-field violation.
- Emails with `from_address` lacking valid TLD fail validation.
- Retry `initial_backoff` > `max_backoff` returns cross-field violation.
- `smtp.tls.insecure_skip_verify` returns 403 via HTTP even when user has `settings.update` unless override JWT present (deployments that disable this feature return "immutable via HTTP" 400).
- `/smtp/test-send` without `settings.smtp.test_send` returns 403; with permission enqueues job with 202 + request_id.
- SMTP password change events are written to audit with redacted previous/new values.

