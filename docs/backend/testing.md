# Testing

## Test Strategy

- unit tests for domain entities and services
- integration tests for repositories, cache, and event publishing
- request-level tests for critical HTTP endpoints
- smoke tests for deployment health

## Priority Areas

- auth and session flows
- RBAC and permission checks
- post publishing workflow
- storage compatibility across R2 and S3-compatible providers
- dynamic image transform latency and cache behavior
- cache invalidation behavior
- event emission consistency
- comment moderation rules
- analytics consent enforcement and ingestion gating
- analytics dashboard query correctness across 7/30/90-day windows
- analytics search CTR and retention aggregation math
- realtime SSE stream access controls and fan-out isolation
- analytics export, withdrawal, and erasure flows
- settings GET/PUT auth, RBAC, and sensitivity filtering
- settings PUT validation: unknown keys, type mismatch, range/enum violations
- settings audit logging and optimistic locking behavior
- settings cache invalidation on successful updates
- impersonation role/permission gating (strict superadmin only)
- impersonation start/stop CSRF protection and 2FA step-up enforcement
- impersonation session inheritance: effective user = target user, zero privilege elevation
- impersonation audit trails: lifecycle events + per-action impersonator metadata
- impersonation exit and auto-expiry flows
- impersonation document access decisions follow target user exactly
- user ↔ media associations: avatar_id FK validation, SET NULL on media delete
- category ↔ media associations: image_id FK validation, SET NULL on media delete
- post ↔ media associations: post_media join table, cover consistency, sort order and kind allowlist
- retrieval endpoints for users, posts, categories include associated media correctly; include toggles work correctly
- post revision history:
  - create/update/publish transitions produce a revision with correct snapshot + diff_jsonb + changelog_text
  - revision_number is monotonically increasing per post and unique(post_id, revision_number) holds
  - restore creates new revision row (type=restore) with restore_from_revision_id set; original rows untouched
  - restore reproduces content, slug, excerpt, status, category, cover, media attachments, tags, and SEO snapshot exactly
  - RBAC: author cannot view/restore revisions for another author's posts unless granted editor/admin permissions
  - impersonator metadata propagates to revision audit fields when revision created during impersonation session
  - list endpoint respects pagination, author_id filter, date range, and include_diff toggle
- post SEO configuration:
  - validation: slug regex + uniqueness + reserved slugs; SEO title/description length warnings (soft); image FK existence; canonical origin allowlist; Twitter creator handle format; robots strict bools
  - PUT with unknown keys is rejected; partial field updates work via allowlist
  - SEO preview endpoint returns search/OG/Twitter rendered previews with correct fallback chains (seo_title→post.title, og_image→cover→default, canonical→pattern)
  - public `/posts/{slug}/seo-meta` returns 404/noindex for drafts and cached structured payload for published posts
  - slug update emits `blog.post.slug_changed` event; SEO update emits `blog.post.seo.updated`; both create corresponding revision entries with SEO diff + changelog
  - restoring a revision restores SEO snapshot content (meta fields + slug + robots flags) exactly
- user profile self-service:
  - unknown patch keys in `PATCH /me/profile` are rejected with `code=profile.unknown_field`; same for privacy put unknown flags
  - allowlisted patch keys applied; equal values produce no diff, no event, no audit write (idempotent)
  - encrypted contact_phone round-trip: write encrypted, decrypt matches original; wrong env KMS key produces decrypt error correctly mapped
  - avatar upload pipeline: valid jpeg/png/webp (≤5 MB) → media_asset row + variants 64/128/256/512 all 200 reachable; invalid MIME/magic bytes/malware-scan-fail → 422, no media or FK created
  - avatar DELETE sets users.avatar_id SET NULL; orphaned media correctly processed by cleanup consumer within TTL
  - password change: wrong current_password → 403; common password → 422 password.strength; last-N history reuse → 422 password.history_conflict; success → bcrypt/argon2id verify succeeds, password_history has new row, revoke_all_sessions=true invalidates old refresh tokens
  - forgot/reset password: request returns 200 for both existing and non-existing emails (timing-safe within ±5ms via sleep jitter); reset jti valid once, reuse → 400 auth.password.token_used; expired token → 400 auth.password.token_expired; valid flow completes → password_hash changes, sessions invalidated
  - email change: invalid password proof → 403; unverified change never applied to users.email; valid confirm token changes users.email + email_verified_at; old/new address both receive notice emails
  - privacy toggle: public → private requires step-up (reverify password or 2FA); private → public allowed without; public profile /users/:name returns 404 after toggle and X-Robots-Tag noindex header set
  - activity tracking: login/profile_edit/avatar_update/password_change/privacy_change/post_create/comment_create events produce correct activity_type enum rows with impersonator metadata when applicable
  - activity CSV export streams exactly matching rows for given date/category filters; export endpoint rate-limited 1/hour 429
  - activity erase: password reverify required; ip_address/ua_bucket/geo PII zeroed; audit record preserved; erasure_marker present
- user profile admin cross-user:
  - `/admin/users/:id/profile` read returns full contact.phone decrypted (admin-only), unredacted privacy, summary activity counts; without `user.profile.read` permission → 403
  - `/admin/users/:id/profile` PATCH requires 2FA step-up and `user.profile.edit`; edits produce audit_logs row showing admin actor + target user; effective target user sees corresponding `user.profile.updated` event with admin-initiated flag set true; users' privacy toggles edited by admin also produce `user.privacy.updated`
  - `/admin/users/:id/activity` with `user.activity.read_all` returns full rows (full IP, detail_jsonb without redactions) regardless of target user privacy setting; permission missing → 403
- impersonation + profile interaction:
  - impersonated effective user edits profile → target record changes; audit includes impersonator_id; activity row records impersonator_id + impersonation_session_id
  - impersonation revocation (parent session expired) causes immediate downstream profile-write attempts to 401; no partial state left
- rate limiting tiers:
  - 6 rapid `/me/password` requests from same user → 429 with Retry-After header; counter persists across processes via Redis
  - 6 rapid `/auth/password/forgot` from same email_hash + ip → 429 Retry-After
  - `/me/activity/export` 2 rapid calls → 429; second call rejected even if first job still in-progress
- public user page correctness:
  - admin sets user_privacy_settings all-on for public test user → includes avatar/posts_preview when `include=` present; contact details appear when visibility_contact_details=true; email never shown regardless of settings
  - private test user: unauthenticated caller gets 404; owner via `/me` gets 200 full payload; admin cross-user gets 200 with permission
  - search_allow_indexing=false → `X-Robots-Tag: noindex` present on `/users/:name` public response; toggle back → no header (or `index,follow` depending on convention)
- CSRF + auth:
  - browser-origin profile PATCH/PUT/POST/DELETE without CSRF token → 403 csrf.missing; invalid token → 403 csrf.invalid; valid token + valid auth → 200
  - non-browser API-token callers (configured allowlist) exempt from CSRF but still rate limited
- CORS + security headers profile routes:
  - wildcard origin rejected for profile mutations; only whitelist origins allowed; non-whitelisted → 403 cors.origin_not_allowed
  - production responses set CSP, SameSite=Lax/Secure cookies, X-Content-Type-Options nosniff; profile avatar endpoints served on distinct media domain without auth cookies

## Realtime SSE Stream

- contract and OpenAPI compliance:
  - `GET /api/v1/me/notifications/stream` with valid bearer JWT → 200; headers present exactly per contract (`Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-cache`; non-HTTP/2 env also includes `Connection: keep-alive`, `X-Accel-Buffering: no`)
  - 200 body contains exactly one `retry:` line (default `5000`) and exactly one opening `event: stream.opened` with data payload `{ stream_id, user_id, server_ts, retry_ms, channels }`
  - `Last-Event-ID:` header (and equivalent `?replay_after=`) both accepted; header wins when both provided
  - responses for `401 unauth`, `403 cors/csrf`, `429 too many connections` all return standard `EnvelopeError` JSON with documented `code` enums (`sse.stream.too_many_connections`, `sse.stream.reconnect_rate`, `sse.stream.jailed`, `cors.origin_not_allowed`, `csrf.invalid`) and `429` always sets numeric `Retry-After` header
- connection lifecycle and cleanup (integration tests, see §6.1–6.3 in [realtime-notifications-sse.md](../features/realtime-notifications-sse.md)):
  - clean client close → hub.Unregister called, process-local `activeCount` decremented within one watchdog cycle; Redis `sse_active:user:<id>` counter matches
  - half-open TCP (server sends RST via watchdog after two missed pings) → watchdog closes socket within `30 + 2*ping` seconds, releases goroutines; `runtime.NumGoroutine` delta from start→after 10 connections stays bounded
  - SIGTERM graceful server shutdown → every open stream receives `event: stream.closed { code: "shutdown", retry_ms: 15000 }` then drains; zero goroutines leaked after 10s grace window
- realtime delivery and fan-out (requires Redis test instance for multi-process fan-out):
  - single user, N connections (N=5) → single published `notification.created` event arrives on all 5 within p99 latency `MAX_FANOUT_LATENCY_MS` (500ms default); `id:` monotonic across frames per connection; same `data.notification_id` delivered to all
  - cross-user isolation: publish to user A → user B stream read buffer contains zero new frames within a 300ms settle window (no cross-leak)
  - inter-process: two HTTP servers (A and B) share same Redis; publish to A's Watermill bus → both A-connected and B-connected subscribers receive the notification.created within 500ms
- reconnect behaviour:
  - publish 500 events, drop client TCP mid-stream, reconnect with exact `Last-Event-ID` of 250 → replay of events 251..500 exact; `stream.opened.replay_applied=true`, `replay_count=250`; client dedup by `notification_id` yields 500 unique ids (no dupes, no gaps)
  - client with `Last-Event-ID` older than replay TTL → stream still opens, `replay_applied=false`, client fires `GET /api/v1/me/notifications` REST history catchup and ends with union covering all ids within the retention window
  - reconnect backoff: wrapper doubles delay each consecutive transport failure up to 120s cap; resets to `retry_ms` immediately upon next `stream.opened` arrival
  - aggressive reconnect jail: 12 connect attempts within 60s window from same `ip:user` → last attempts → 429 `sse.stream.jailed` with `Retry-After: 120`; attempts inside jail all 429 until TTL elapses
- security and rate limiting:
  - missing `Authorization`/cookie or tampered JWT → 401 `unauthorized` envelope; zero SSE frame bytes written before close
  - non-whitelisted `Origin: https://evil.example` on cookie call → 403 `cors.origin_not_allowed`
  - session cookie without `X-CSRF-Token` when CSRF enabled for browser origins → 403 `csrf.invalid`; allowlisted API tokens pass through
  - concurrent connection limit: 4 simultaneous connects for user (max default=3) → 4th returns 429 `sse.stream.too_many_connections`, `X-RateLimit-Limit=3`, `X-RateLimit-Remaining=0`, `Retry-After=30`
  - slow consumer buffer overflow: attach 1 r/s reader, write 1000 events as fast as possible → exactly one `event: error { code: "fanout.buffer_full", dropped_count: N }` frame emitted with integer `N`, no other clients blocked, no goroutine leak, process continues
- graceful degradation for SSE-unsupported clients:
  - jsdom or unit environment without `EventSource` → wrapper falls back to REST polling at 60s interval, hydrates first 50 notifications from `GET /api/v1/me/notifications`
  - HTTP/1.0 `Connection: close` clients → route either writes handshake then cleanly closes with 0 bytes leaked, or returns documented 400 `sse.protocol_upgrade_required` (per configured strictness); never 500, never goroutine leak per NumGoroutine assertion
- SSE contract fixtures live in `tests/contracts/fixtures/notifications-stream.json` alongside comments/newsletter fixtures, each test case is a JSON object with fields `{ name, request_headers, expected_status, expected_events[], last_event_id?, cors_origin?, expected_error_code? }` and the contract test runner replays them against an httptest server using the configured Gin engine with middleware chain.

## Test Environment

- isolated test database
- deterministic fixtures
- Redis test instance where cache behavior matters
- fake or test event publisher for service-level tests
- storage emulator or MinIO for provider compatibility tests

## Rules

- tests must be independent
- avoid shared mutable global fixtures
- keep security-sensitive scenarios covered first
- include corrupted image, invalid transform, and storage failure scenarios

## Operational handbook

Layers, HTTP/contract test patterns, and PR checklist: [docs/testing](../testing/README.md).
