# Settings Backend

## Hexagonal Placement

Follows the same hexagonal modular structure as the rest of the backend.

### Core Package

- `internal/core/settings/domain`
  - entity: `Setting`
  - value objects: `SettingKey`, `SettingSensitivity`, `SettingValueType`, `SettingSchema`
  - domain errors: unknown key, invalid value, update rejected, version mismatch
- `internal/core/settings/ports`
  - inbound: `SettingsReaderPort`, `SettingsWriterPort`
  - outbound: `SettingsRepository`, `SettingsCache`, `AuditSink`, `EventPublisher`
- `internal/core/settings/service`
  - `SettingsService`: get, partial update, validate keys against schema

### Adapters

- inbound HTTP under `internal/adapters/inbound/http/admin/settings`
- outbound:
  - GORM repository `internal/adapters/outbound/persistence/settings`
  - Redis cache wrapper `internal/adapters/outbound/cache/settings`
  - Watermill publisher for events

## Storage Model

### Table `settings`

Recommended fields:

- id (bigint identity PK), uuid (unique public id)
- key (unique, indexed)
- value_jsonb (JSONB or TEXT depending on type; prefer JSONB for structured types)
- value_type: `string | number | boolean | string_array | object`
- sensitivity: `public_safe | admin_only | server_only`
- category: `site | content | media | analytics | notifications | seo | security | storage | smtp`
- version (integer for optimistic locking)
- description
- updated_by (user_id)
- created_at
- updated_at

Unique constraints:

- unique index on `(key)`

### Table `settings_history`

For fast retrieval of changes without scanning only audit logs:

- id (bigint identity PK), uuid (unique public id)
- setting_id
- key (denormalized copy)
- previous_value_jsonb
- new_value_jsonb
- changed_by (user_id)
- request_id
- ip_address (truncated/masked if privacy rules require)
- created_at

## Endpoints

Follow project API standards under `/api/v1/admin`.

### `GET /api/v1/admin/settings`

Purpose: return current non-sensitive settings for admins.

Headers:

- `Authorization: Bearer <access_token>` or equivalent server-managed session
- `X-Request-ID` recommended

Query params:

- `category` optional filter: `site`, `content`, `media`, `analytics`, `notifications`, `seo`, `security`, `storage`, `smtp`
- `include_sensitive_admin` boolean, default `false`; when true and caller has elevated permission, include `admin_only` keys. Must never include `server_only` keys.

Response:

```json
{
  "data": {
    "settings": [
      {
        "key": "site.name",
        "value": "Blog",
        "value_type": "string",
        "category": "site",
        "sensitivity": "public_safe",
        "updated_at": "2026-07-29T10:00:00Z"
      }
    ],
    "default_applied": [
      "content.default_post_status"
    ]
  },
  "meta": {
    "request_id": "req_123"
  },
  "error": null
}
```

Security rules:

- requires authentication
- requires permission `settings.read`
- never returns keys with `server_only` sensitivity
- 403 for missing permissions; 401 for missing auth

### `PUT /api/v1/admin/settings`

Purpose: partial update of authorized settings keys.

Headers:

- `Authorization: Bearer <access_token>`
- `Content-Type: application/json`
- `X-CSRF-Token` where the client is browser-based
- `If-Match` optional for optimistic locking with a version digest or per-key versions

Request body:

```json
{
  "updates": [
    { "key": "site.name", "value": "New Blog Name" },
    { "key": "media.max_upload_size_bytes", "value": 5242880 },
    { "key": "content.comments_enabled", "value": true }
  ]
}
```

Validation rules:

- `updates` is required, non-empty array, max length per request (e.g. 100)
- every `key` must be present in the schema allowlist
- `value` must match the declared type and any range/enum constraints
- unknown keys cause the **entire request to fail** with `validation_error`
- string values: length bounds, allowed charset, regex or enum enforcement where applicable
- numeric values: min/max bounds, safe integer checks
- boolean values: strict boolean (reject `"true"`/`1`) unless the schema explicitly declares coercion rules
- array values: item allowlist, max duplicates, max length
- object values: validated against JSON-schema-like rules for the declared key

Response on success (200):

```json
{
  "data": {
    "applied": [
      { "key": "site.name", "previous_value": "Blog", "new_value": "New Blog Name" }
    ],
    "unchanged": []
  },
  "meta": { "request_id": "req_123" },
  "error": null
}
```

Response on validation failure (422):

```json
{
  "data": null,
  "meta": { "request_id": "req_123" },
  "error": {
    "code": "validation_error",
    "message": "one or more settings are invalid",
    "details": {
      "violations": [
        {
          "key": "media.max_upload_size_bytes",
          "reason": "out_of_range",
          "message": "value must be between 1048576 and 52428800"
        }
      ]
    }
  }
}
```

Authorization rules:

- requires authentication
- requires permission `settings.update`
- server service layer re-checks permissions
- some `security.*` keys may require additional 2FA step-up auth header or proof

## HTTP Middleware

The endpoint pipeline should include, in order:

- request ID injection
- CORS enforcement (whitelist only)
- CSRF protection for browser-based PUT requests
- authentication middleware resolving actor identity
- rate limiting (stricter for PUT than GET; e.g. 60 PUT/minute per admin user)
- RBAC middleware emitting 403 early for missing permissions
- audit context middleware attaching request ID, IP, user agent bucket
- request body size limit for PUT

## Events

Transactional outbox events emitted for each successful PUT:

- `settings.updated` with:
  - aggregate_id = system-settings or key-level ID
  - list of changed keys
  - per-key previous/new values (redacted per sensitivity rules)
  - actor_id
  - request_id
  - occurred_at timestamp

In addition, handlers may optionally emit:

- `settings.cache.invalidated` for dependent cache layers
  - `settings.security.updated` for `security.*` keys
  - `settings.storage.updated` for `storage.*` keys
  - `settings.smtp.updated` for `smtp.*` keys

Consumers of these events:

- cache invalidation
- audit log consolidation
- realtime admin activity streams if subscribed by authorized users
- search/SEO re-warmup jobs for site-wide SEO setting changes
- `media.*` consumers for storage category changes: re-apply CORS, lifecycle, drop storage caches (see `docs/features/settings-management.md`)
- `email.*` consumers for SMTP category changes: drop connection pools, refresh workers, emit SMTP test results (see `docs/features/settings-management.md`)

## Caching Strategy

- read path:
  1. check Redis for `settings:snapshot:v1` and `settings:by_category:{category}`
  2. cache miss -> repository read -> assemble snapshot -> cache write
  3. return filtered response per sensitivity and permissions
- write path:
  1. transactional update on `settings` and `settings_history`
  2. append outbox events
  3. delete/invalidate cache snapshots on commit
  4. async optional: worker rebuilds cache warm entries

TTL guidance:

- GET snapshot TTL 5-15 minutes with strong invalidation
- never cache responses containing `admin_only` keys by default unless cached per-permissions scope
- do not cache `server_only` results at all (they are not returned)

## Audit Logging

Write one audit row per PUT attempt (success/failure):

- actor_id
- action: `settings.update_attempt` / `settings.update_success` / `settings.get_access`
- resource_type: `settings`
- resource_id: can be `system-settings` or aggregated key list
- request_id
- ip_address
- metadata JSONB:
  - keys attempted
  - per-key validation failures if any
  - per-key previous/new redacted values for successes
  - result: `success | validation_failed | permission_denied | stepup_required`

Never log full raw secrets or raw server-only settings in any audit/debug stream.

## Unit Tests

### Endpoint / Handler Tests

- `GET /api/v1/admin/settings`
  - 401 without auth
  - 403 authenticated without `settings.read`
  - 200 returns filtered keys for authorized admin
  - category filter works correctly
  - never leaks `server_only` keys
- `PUT /api/v1/admin/settings`
  - 401 without auth
  - 403 without `settings.update`
  - 422 for unknown keys
  - 422 for type mismatch (string sent for numeric, etc.)
  - 422 for out-of-range numbers
  - 422 for disallowed enum strings
  - 200 applies valid partial updates
  - 200 empty unchanged list when submitted values equal stored values
  - optimistic locking conflict returns proper error when `If-Match` is stale
  - audit rows created for both success and validation failures

### Service Tests

- schema validation rejects unknown keys
- partial update only mutates submitted keys
- default values used when storage rows missing
- sensitivity filtering correct for each caller permission level
- outbox events produced for changed keys
- cache invalidation triggered on successful writes

### Repository Tests

- upsert correctly updates existing rows for known keys
- `settings_history` captures previous/new and changed_by metadata
- optimistic locking increments version correctly
- category filter produces correct subset
- transactional rollback prevents partial update on event/store failure

## Integration Tests

- end-to-end auth -> permissions -> update -> audit event pipeline
- concurrency tests for two PUTs racing on the same key
- CORS whitelist rejection tests
- CSRF protection tests for browser-origin PUT
- rate limiting enforcement tests for PUT endpoint

## Compatibility With Existing Architecture

- follows `/api/v1/admin` prefix and envelope response standards
- uses project RBAC permission keys and 2FA step-up rules from security/architecture docs
- uses Watermill outbox for durable event publishing
- uses Redis cache invalidation patterns aligned with media and post caches
- uses audit log rules aligned with auth/RBAC/media audit requirements
- test strategy and environment setup follow backend testing doc
