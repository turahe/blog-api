# User Profile Backend Module

This is the target design. What is wired today, and how it differs (the service is
`userservice.ProfileService`, email change lives in the auth service, no step-up or outbox
yet), is recorded in [api.md](api.md#profiles-and-email-change).

## Hexagonal Placement

This module sits under the existing `user` core module boundary and uses new sub-packages to avoid circular coupling with media, auth, analytics, and settings.

### Core Package

- `internal/core/user/domain`
  - entities: `UserProfile` (value objects: `ContactDetails`, `SocialLinks`, `PrivacyVisibility`, `MarketingConsent`), `UserPasswordChangeRequest`, `UserPasswordResetToken`, `UserEmailChangeRequest`, `UserActivity`
  - domain errors: `profile.validation_error`, `profile.unknown_field`, `profile.avatar.malware_detected`, `password.strength`, `password.history_conflict`, `password.token_invalid`, `email.change_unverified`, `privacy.level_change_requires_reauth`, `activity.not_found`, `encryption.decrypt_failed`
- `internal/core/user/ports` — inbound:
  - `UserProfileReaderPort`: `GetMe(userId, includes[]), GetPublicProfile(usernameOrId, callerUserId?), GetPrivacy(userId), GetActivity(userId, filterPagination)`
  - `UserProfileWriterPort`: `UpdateProfile(userId, patch), UpsertAvatar(userId, mediaAssetId), DeleteAvatar(userId), UpdatePrivacy(userId, patch)`
  - `UserPasswordPort`: `ChangePassword(userId, current, new, revokeAllSessions?), BeginForgot(emailOrUsername, ip, ua), CompleteReset(token, newPassword)`
  - `UserEmailPort`: `BeginEmailChange(userId, newEmail, passwordProof), ConfirmEmailChange(token), CancelEmailChange(token)`
  - `UserActivityPort`: `AppendActivity(activityRecord), ExportActivityCSV(userId)`
  - outbound:
  - `UserProfileRepository`, `UserPrivacyRepository`, `UserActivityRepository`
  - `AvatarMediaServicePort` (adapts to existing media module; returns MediaAsset after upload/transform)
  - `PasswordHasherPort` (Argon2id or bcrypt per security)
  - `FieldEncryptionPort` (AES-GCM envelope for sensitive contact fields)
  - `UserSessionRepository` (refresh-token family invalidation)
  - `AuditSink`, `EventPublisher`, `Cache` (Redis-aligned)
  - `EmailSenderPort` (SMTP adapter per email module)
  - `RateLimiterPort` (tiered rules)
  - `CsrfVerifierPort`
- `internal/core/user/service`
  - `UserProfileService` composes all inbound ports.

### Adapters

- Inbound HTTP:
  - `internal/adapters/inbound/http/me/profile.go` for `/api/v1/me/*` routes
  - `internal/adapters/inbound/http/public/user_profile.go` for `/api/v1/users/{username}`
  - `internal/adapters/inbound/http/admin/user_profile.go` for `/api/v1/admin/users/{id}/profile`
  - `internal/adapters/inbound/http/auth/reset_password.go` for forgot/reset
- Outbound:
  - GORM repositories for `user_profile`, `user_privacy_settings`, `user_activity`, `user_password_history`, `user_email_change_tokens`, `password_reset_tokens`
  - Media adapter delegate to existing media storage/transform
  - SMTP email delegate per settings SMTP configuration
  - Redis cache + rate limit counters
  - Watermill outbox publisher for events

## Storage Model

New tables (see also `docs/backend/database.md` for full canonical schema):

### `user_profiles` (one-to-one with users.id, extends existing users table with sparse optional contact/social)

Prefer keeping frequently-displayed fields in the users row (full_name, email, avatar_id) and rarely-edited contact/social in user_profiles for cleaner normalization, or treat as same entity 1:1 always-extended pattern per GORM modeler choice. This spec uses the 1:1 extension pattern.

- id bigint identity PK, uuid unique public id; user_id bigint FK -> users.id UNIQUE CASCADE on delete
- display_name varchar(60), nullable unique; bio text (max 4000 chars)
- encrypted_contact_phone bytea (AES-GCM ciphertext)
- contact_website varchar(2048)
- contact_location varchar(120)
- social_links JSONB { twitter, linkedin, github }
- locale varchar(32) (BCP 47); timezone varchar(64) (IANA)
- marketing_consent boolean default false; marketing_consent_updated_at
- updated_by UUID; created_at; updated_at

Indexes: UNIQUE(user_id), UNIQUE(display_name) where display_name is not null; locale, timezone for analytics.

### `user_privacy_settings` (one-to-one)

- id bigint identity PK, uuid unique public id; user_id bigint FK unique CASCADE
- visibility_profile enum ('public','unlisted','private','followers_only') default 'public'
- visibility_email boolean default false; visibility_contact_details boolean default false
- visibility_activity_timeline boolean default false
- search_allow_indexing boolean default true
- tracking_personalize_ads boolean default false
- updated_by UUID; created_at; updated_at

Index: unique(user_id)

### `user_password_history` (append only, for N-history password rule)

- id PK; user_id FK users; password_hash_version smallint; password_hash text; created_at
- Index on (user_id, created_at desc) for fast 10-history check

### `password_reset_tokens`

- id PK; user_id FK; jti varchar UNIQUE; token_hash (SHA-256 of token if opaque); scope enum ('forgot','email_change'); email_hash bytea optional; expires_at; consumed_at nullable; issued_ip; issued_ua_bucket
- Indexes: (jti), (user_id, created_at desc), (scope, expires_at) partial where consumed_at is null

### `user_activity` (append-only engagement trail)

- id bigint identity PK, uuid unique public id; user_id bigint FK users CASCADE; session_id nullable; impersonator_id nullable; impersonation_session_id nullable; request_id nullable
- activity_type enum (see feature spec)
- summary varchar(500); detail_jsonb (per-type schema: redacted IP, UA bucket, partial geo, route, referrer, post_id)
- ip_address (truncated/encrypted: IPv4 /24, IPv6 /64 OR full when `user.activity.read_all` admin needs it)
- user_agent_bucket varchar(64)
- geo_country_code char(2), geo_subdivision nullable
- created_at timestamp

Indexes: (user_id, created_at desc), (activity_type, created_at), (impersonation_session_id) GIN on detail_jsonb for admin queries.

### `user_activity_daily` (materialized view for charts)

Per-user per-day aggregates: date, user_id, counts per activity_type, unique_days_active — refreshed hourly by scheduler job (`app scheduler`).

### Referential integrity + delete behavior

- When a user is hard-deleted (rare, typically soft-delete `users.status`), CASCADE removes rows in user_profiles, user_privacy_settings, user_password_history, user_activity.
- When a media_asset referenced by `users.avatar_id` is deleted via media service delete: users.avatar_id SET NULL per `docs/backend/media-relations.md`; old avatar entry written to user_activity as `avatar_update`.

## Endpoints

Follow the canonical contract at `docs/swagger.json`.

### Self-Service `/api/v1/me` family

#### `GET /api/v1/me`

Returns user identity summary + profile + privacy + oauth_links when requested via `include=` query array.

Query params: `include` (comma separated: `avatar,profile,privacy,oauth_links,roles,permissions,email_verification_state`).

Security: authenticated (any JWT access token); 401 if missing; no extra permission needed.

Caching: Cache-Control private max-age=30; Redis TTL 30s keyed by user_id + include mask, invalidated on profile writes.

#### `PATCH /api/v1/me/profile`

Partial update of profile fields via JSON body `{patch}`.

Rules: allowlist of patch keys matches `full_name, display_name, bio, contact{phone,website,location}, social_links, locale, timezone, marketing_consent`; unknown keys fail.

Headers: `X-CSRF-Token` (browser clients), `Content-Type: application/json`, optional `X-Re-Verify-Password` or `X-2FA-Verified` for high-risk directions.

Side effects: append `profile_edit` user_activity; invalidate `/me` cache; publish `user.profile.updated` with before/after diff of changed fields; audit_logs row; send confirmation email when contact.phone or email or marketing_consent changes.

#### `POST /api/v1/me/avatar` (multipart/form-data)

Form parts: `file` (binary image), `crop` (optional JSON {x,y,width,height}).

Server: media upload via MediaServicePort → media_asset row → users.avatar_id FK upsert → old value SET NULL + archive decision by media lifecycle → publish `user.avatar.updated` → user_activity `avatar_update` → audit → return full MediaAsset URLs (64/128/256/512 WebP).

Size limit: 5 MB; accepted MIME: image/jpeg, image/png, image/webp, image/gif, image/avif. Malware scan; SVG accepted only if sanitized and disabled by default.

#### `DELETE /api/v1/me/avatar`

Detach avatar: set FK null; optionally soft-delete media_asset. Activity + audit + event. CSRF required.

#### `PUT /api/v1/me/password`

Body: `{current_password, new_password, confirm_password, revoke_all_sessions?}`.

Requires re-verify password or recent 2FA success. Rate limit 5/min; history check N=10; new password strength validator (12 char min, not common). If revoke_all_sessions true → SessionRepo rotate user's refresh family; emit `user.session.invalidated_family`. Audit `password_change`, outbox event.

#### `POST /api/v1/auth/password/forgot`

Unauthenticated, public; body `{email_or_username}`.

Response: 200 with timing-safe message "If your account exists, a reset link has been sent."

Rate limit 5/hour 15/day per email + IP. Side effects: generate JWT token with jti, consume-once via Redis SETNX jti, send email with signed URL; audit_logs row with attempted status; emit `auth.password.reset_requested`.

#### `POST /api/v1/auth/password/reset`

Body: `{token, new_password, confirm_password}`.

Validate JWT signature + jti via Redis → check email_hash salted matches → same password validation rules → upsert password_hash + append password_history row → invalidate token family → emit audit + `user.password.reset` → return success envelope with login hint.

#### `POST /api/v1/me/email/request-change`

Authenticated; body: `{new_email, password_proof}`.

Generate JWT email-change token (1 hour TTL), send confirmation email to new address AND warning notice to old email. Produce activity + audit; rate limit 2/day per user.

#### `POST /api/v1/me/email/confirm-change`

Body: `{token}` → verify → update users.email + email_verified_at = now → revoke sessions → send "your email has changed" notices to both old/new. Audit + `user.email.changed`.

#### `GET /api/v1/me/privacy` and `PUT /api/v1/me/privacy`

GET: returns current user_privacy_settings row; no cache.
PUT: partial patch, allowlisted keys only; when visibility reduces (public→private) require re-verification password. Invalidate public profile cache, emit `user.privacy.updated`.
Implemented; the password travels in the body as `current_password`, not in a header. See
[api.md](api.md#profiles-and-email-change).

#### `GET /api/v1/me/activity`

Query params: `page`, `per_page` (max 100), `category[]` (multi enum), `from_date`, `to_date`, `export=csv` optional.

Owner only. Append-only rows; CSV export via streaming writer. Rate limit export 1/hour.

#### `GET /api/v1/me/activity/export` + `POST /api/v1/me/activity/erase`

GDPR/CCPA flows: submit export job or erasure request; erasure requires password re-verify; emits analytics.consent.erasure too if user requests it.

### Public Profile

#### `GET /api/v1/users/{username_or_id}`

Path param: either UUID or unique display_name / username.

Query: `include=avatar,posts_preview,roles_brief`; caller identity anonymous when public.

Response filters fields exactly by user_privacy_settings: private profile returns 404 unless owner/admin; `X-Robots-Tag: noindex` when `search_allow_indexing=false`; SSR HTTP header + meta tag pair.

Cached heavily: 15 min public, invalidated on `user.profile.updated` and `user.privacy.updated`.

### Admin Cross-User Profile

#### `GET /api/v1/admin/users/{user_id}/profile`

Requires `user.profile.read` permission (plus auth). Returns full non-redacted profile, privacy state, summary activity counts, and linked provider list.

#### `PATCH /api/v1/admin/users/{user_id}/profile`

Requires `user.profile.edit`. Same allowlist as self-service plus support flags. 2FA step-up for all admin writes.

## Validation Rules

### Allowlisted patch keys

`full_name, display_name, bio, contact_phone, contact_website, contact_location, social_links.twitter, social_links.linkedin, social_links.github, locale, timezone, marketing_consent`.

### Strict Validation Rules

- `full_name` / `display_name` / `contact_location`: regex `^[^\p{Cc}]{1,N}$`.
- `bio` markdown; allow tags: a, strong, em, code, ul, ol, li, blockquote, p, h3-h6, pre; strip `<script,iframe,form,input,on* attributes>`; all hrefs rel=nofollow noreferrer noopener; max length 4000 chars after sanitization.
- `contact_phone` → E.164 via `libphonenumber`-style regex + region validation.
- `contact_website` → RFC 3986 with scheme http/https; 2048 length; URL-encode unwise chars; reject file:/data: schemes.
- `social_links.twitter` → regex `^@?[A-Za-z0-9_]{1,15}$` normalized to `@handle` when not full URL.
- `locale` → BCP 47 allowlist (application configured); default `en_US`.
- `timezone` → in IANA tz database provided by `time` package tzdata; default `UTC`.
- `marketing_consent` → strict boolean. If `true`, confirm ConsentService has recorded the opt-in with timestamp before persisting (use outbox pattern).
- Password update: 12 chars minimum, NIST-800-63B guidance — reject passwords appearing in breach corpuses via bloom/hash lookup; compare to hashed last N (default 10) password_history records.
- CSRF verification for all state-changing browser requests.
- Avatar: max 5 MB bytes; magic bytes match declared MIME (e.g., JPEG SOI marker); image dimensions ≤ 8192×8192; malware scan pass; return client-safe media asset URLs on different domain than app.

## Events

Transactional outbox events:

| Event Name                              | Emitted When                                                         | Payload                                                                                      |
|-----------------------------------------|----------------------------------------------------------------------|----------------------------------------------------------------------------------------------|
| `user.profile.updated`                  | PATCH /me/profile success or admin profile edit                     | {user_id, actor_id, impersonator_id?, diff: {before, after} redacted for encrypted fields}  |
| `user.avatar.updated`                   | POST or DELETE /me/avatar success                                   | {user_id, old_media_asset_id, new_media_asset_id?, size_variants[]}                          |
| `user.password.changed`                 | PUT /me/password success                                            | {user_id, session_invalidated_family: bool, admin_initiated: bool}                           |
| `user.password.reset`                   | POST /auth/password/reset success                                   | {user_id}                                                                                    |
| `auth.password.reset_requested`         | POST /auth/password/forgot (attempt)                                 | {user_id? redacted if unknown per timing-safe, attempted_email_hash, status: attempted}     |
| `user.email.change_requested`           | POST /me/email/request-change success                               | {user_id, new_email_hash, old_email_hash}                                                    |
| `user.email.changed`                    | POST /me/email/confirm-change success                               | {user_id, old_email_hash, new_email_hash, revoked_sessions_count}                            |
| `user.privacy.updated`                  | GET/PUT privacy success (PUT only for diff)                         | {user_id, diff: {before, after}}                                                             |
| `user.activity.export_requested`        | Export CSV requested                                                | {user_id, job_id}                                                                            |
| `user.activity.erase_requested`         | GDPR erasure submitted                                              | {user_id, erasure_id, scope: activity|full_user?}                                            |

## Caching Strategy

- Private: `user:me:{user_id}:include:{mask}` TTL 30s, invalidated on any user profile mutation.
- Public: `user:public:{username}` TTL 900s (15 min), invalidated on profile/privacy events.
- Privacy & avatar: short TTLs; when privacy changes → delete public cache for username immediately; when avatar changes → media module internal cache variant invalidation applies.

## Rate Limiting

Per RateLimiterPort configuration:

| Endpoint family          | Per user (authenticated) | Per IP (unauthenticated) |
|--------------------------|---------------------------|--------------------------|
| GET /me, GET /me/*       | 120/min                   | n/a                      |
| PATCH /me/profile        | 30/min                    | n/a                      |
| POST/DELETE /me/avatar   | 20/min                    | n/a                      |
| PUT /me/password         | 5/min                     | n/a                      |
| Email change             | 2/day                     | n/a                      |
| Forgot / reset password  | —                         | 5/hour, 15/day           |
| Activity export          | 1/hour                    | n/a                      |
| Public GET /users/{name} | n/a                       | 60/min                   |

## Audit

For every mutation endpoint:

- actor_id = JWT sub (or anonymous for forgot)
- action = `user.profile.edit_attempt`, `user.avatar.upload`, `user.password.change_attempt`, `user.password.reset_attempt`, etc.
- resource_type = `user_profile`, `avatar`, `password`
- resource_id = user_id
- metadata JSONB includes: attempted_keys (for edits), before_value_hash (for redacted fields), new_value_hash, result (success | validation_error | permission_denied | rate_limited), CSRF check result, 2FA or re-verify password proof status, impersonator_id/session_id.
- Never store raw passwords, reset tokens, phone cleartext, or full email addresses in audit metadata; use HMAC or truncated hashes with server-side pepper only for troubleshooting purposes in authorized debug streams.

## Unit / Service / Repository / Integration Tests Coverage Expectations

### Service Tests

- Allowlist: unknown patch keys rejected with 422; patch subset applied only for submitted keys; equal values not recorded in diff.
- Encryption round-trip for phone; bad data key decrypt → domain error; correct key → round-trips.
- Password: current_password missmatch → 403; history enforcement; token reuse rejected; jti invalid → 400.
- Privacy: visibility downgrade requires password proof; upgrade allowed without proof.
- Avatar: malware-scan reject does not create media_asset row.
- Email change: wrong token → error; old/wrong password_proof → 403.
- Activity append + export produce same category counts for given date filter.

### Repository Tests

- Upsert profile extension row; insert fails for dupe user_id because of unique constraint; CASCADE delete verified.
- Password history N lookups return correctly ordered last N records; when <N it still validates.
- Activity pagination produces deterministic order by created_at desc with tie-breaker id; category filters applied; CSV streaming export byte-identical to known fixture.
- Reset token: jti consumed sets consumed_at; double-attempt fails.

### Integration Tests

- Happy path: register → login → edit profile → upload avatar → change password → forgot & reset → change email → update privacy → all activity items present in activity export.
- Admin cross-user: edit profile succeeds with permission; 403 without.
- Impersonation: profile edit during session includes impersonator metadata in audit and event payload.
- Rate limiting: 6 rapid password change requests return 429 with Retry-After header; counter shared across processes via Redis.
- CSRF: profile PATCH without token from browser origin returns 403; valid token passes.
- Public profile + privacy: public 200 before toggle; 404 after privacy set private; `X-Robots-Tag` header toggled with search_allow_indexing.
- Avatar serve: detached after delete, but URLs for existing cached variants continue working until cache TTL or variant TTL; storage files are eventually cleaned via media module jobs.

## Hexagonal Dependency Guardrails

- HTTP handlers must never instantiate SMTP clients, storage adapters, or SQL connections directly; all go through service → outbound ports.
- No import cycles: core/user depends on domain + ports only; adapters depend on core + transport libraries.
- All database writes use transactions with outbox pattern (update + history + activity + outbox_events in same DB txn); rollback prevents partial state.

## Cross-Reference

- Avatar flow and media validation follow `docs/backend/media.md` and `docs/backend/media-relations.md`.
- Password rules, session behavior, 2FA step-up, audit expectations: `docs/architecture/security.md`.
- Event names and outbox pattern: `docs/backend/events.md` + AsyncAPI contract `docs/architecture/asyncapi.yaml`.
- HTTP envelope format + path versioning: `docs/backend/error-handling.md`, `docs/backend/api.md`, and `docs/swagger.json`.
- RBAC permission catalog appends: `docs/features/user-management.md`.
