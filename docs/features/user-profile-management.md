# User Profile Management

## Feature Summary

Provides a self-service user profile surface for all authenticated users: view and edit personal information (name, email, avatar, contact details), manage passwords (update / forgot reset), configure privacy settings (profile visibility, contact sharing, activity display), and review engagement activity history. Admin users are additionally able to impersonate-target profiles via existing impersonation middleware, subject to strict RBAC.

Follows modern security best practices: bcrypt/Argon2id password hashing, encrypted sensitive contact fields at rest, allowlisted editable fields with validation, rate limiting on sensitive endpoints, CSRF protection, mandatory authentication, and full audit logging for every profile state change.

Delivered with a responsive, accessible UI for desktop + mobile, comprehensive unit + integration tests, a security audit checklist, and a staging-deployment + UAT handoff workflow.

## In Scope

- `GET /api/v1/me` returns the current user's identity, profile, avatar object, and privacy flags (minimal by default; `include=` for extra data).
- `PATCH /api/v1/me/profile` partial update of name, contact fields, bio, social URLs.
- `POST /api/v1/me/avatar` upload an avatar image, attach to user as `avatar_id` FK → `media_assets.id`.
- `DELETE /api/v1/me/avatar` detach avatar (SET NULL FK, optionally archive the media_asset row per media lifecycle rules).
- `PUT /api/v1/me/password` update password with current-password verification + session invalidation.
- `POST /api/v1/auth/password/forgot` and `POST /api/v1/auth/password/reset` with signed JWT-like tokens, short TTL, single-use semantics.
- `GET /api/v1/me/privacy` and `PUT /api/v1/me/privacy` — visibility settings for profile, email, contact, activity, search engines.
- `GET /api/v1/me/activity` — engagement/activity timeline (login, post create/edit/comment/like, consent grant, 2FA events). Paginated, with category filters.
- Public profile `GET /api/v1/users/{username_or_id}` — returns only privacy-allowed fields; 404 when privacy hides the profile.
- Email change: `POST /api/v1/me/email/request-change` + verification email with token and `POST /api/v1/me/email/confirm-change`.
- Responsive UI components: Profile page, Privacy tab, Password tab, Activity tab, Avatar uploader with crop preview, Password reset flow screens, Staging UAT checklist.

## Out of Scope

- Per-user preferences that should live elsewhere (notification delivery preferences remain under notifications module; theme under frontend app config).
- Admin user management CRUD (covered by Admin Users endpoints); this module is **self-service only** for non-impersonation identity (impersonated effective user writes still self-service against the target user record).
- Social login / OAuth linking (covered by auth module). Profile module simply exposes the linked-provider metadata returned by auth when requested via `include=oauth_links`.

## User Roles

- **All authenticated users** (admin, editor, author, moderator, future content-only roles) can access the self-service profile endpoints.
- **Admin** with `user.read` permission can additionally request `GET /api/v1/admin/users/:user_id/profile` (read management view) and `user.update` to hard reset fields for support (still audited and rate-limited).
- **Superadmin** during active impersonation session: when impersonated effective user = target user, profile edits are applied to target, with audit rows including `impersonator_id`, `impersonation_session_id`.

## Permissions

Self-service:

- `me.profile.read` (granted by default to every authenticated user; not in role catalog since it is implicit)
- `me.profile.edit` — default for all authenticated users; may be revoked for locked-down accounts
- `me.password.change` — default for all authenticated users; revoked for users whose passwords are externally-managed (LDAP/OIDC-only accounts)
- `me.privacy.read`, `me.privacy.edit` — both default
- `me.activity.read` — default
- `me.avatar.upload`, `me.avatar.delete` — default

Admin cross-user:

- `user.profile.read` — read other users' full profiles + privacy flags (not public user pages, which always honor privacy settings)
- `user.profile.edit` — forcibly edit a user's profile as a support/admin action
- `user.password.admin_reset` — admin-initiated password reset email or temporary force-password-on-next-login
- `user.activity.read_all` — read any user's activity log regardless of user's own privacy preference

## Access & Security Rules

1. All profile endpoints require authentication. No anonymous access except the privacy-gated `GET /api/v1/users/{username}`.
2. Allowlisted editable fields; unknown keys rejected with validation error.
3. Server-side re-authorization in services; never trust only middleware.
4. High-risk actions (password change, email change, avatar delete, privacy toggle from public → private) require either:
   - `X-Re-Verify-Password` header with current password, OR
   - valid 2FA challenge within the last 5 minutes (whichever is stronger; 2FA takes precedence)
5. CSRF token required for browser-based state-changing requests (`PATCH`, `PUT`, `POST`, `DELETE`)
6. CORS whitelist enforced.
7. Rate limiting tiers:
   - Profile read: 120/min/user
   - Profile edits, avatar operations, privacy edits: 30/min/user
   - Password change, email change, forgot/reset: 5/min/user, 10/hour, with progressive delay and captcha/device trust scoring when available
8. Input validation per backend/validation.md spec; output encoding for any rendered user-supplied strings (XSS mitigation CSP + escaping in frontend).
9. Passwords never logged, audited, or echoed; compared only via constant-time verifier. Sensitive contact details (phone, address, recovery email) are encrypted at rest with KMS/ENV key.
10. Session invalidation on password change, email change, or avatar/sensitive field changes at admin discretion: rotate refresh token family.

## User Information Display and Editing

### Fields (Editable Allowlist)

| Key                    | Value Type    | Sensitivity | Default                 | Validation Rules                                                                 |
|------------------------|---------------|-------------|-------------------------|----------------------------------------------------------------------------------|
| `full_name`            | string        | public-safe | empty                   | 1–150 chars, unicode letters, common punctuation; no control chars                |
| `display_name`         | string        | public-safe | derived from full_name  | 1–60 chars; unique per installation (optional; may be relaxed)                   |
| `bio`                  | string md     | public-safe | empty                   | max 4000 chars; plain text / markdown sanitized; no script tags; links nofollow   |
| `email`                | string email  | admin-only  | —                       | RFC 5322; no-change without verification flow; case-preserved lowercase unique   |
| `contact.phone`        | string        | encrypted   | null                    | E.164 format; requires SMS verify for public-exposed phone toggle                 |
| `contact.website`      | URL           | public-safe | null                    | http/https only; no data URLs; max 2048 chars                                    |
| `contact.location`     | string        | public-safe | null                    | max 120 chars                                                                     |
| `social_links.twitter` | string handle | public-safe | null                    | `@?[A-Za-z0-9_]{1,15}`; normalized to @-prefix or URL                             |
| `social_links.linkedin`| URL / handle  | public-safe | null                    | URL format or slug                                                                |
| `social_links.github`  | slug / URL    | public-safe | null                    | valid github user/repo scope check optional                                       |
| `locale`               | BCP 47 string | user-pref   | `en_US`                 | in app locale allowlist                                                            |
| `timezone`             | IANA          | user-pref   | `UTC`                   | in IANA timezone DB                                                               |
| `marketing_consent`    | boolean       | private     | false                   | may be toggled only when consent-management service acknowledges; audit logged    |

### UI (Responsive)

Profile page layout:

1. **Left column (desktop):** Avatar uploader card (square; 1:1 crop on upload; tap/click to select image; max 5 MB; preview of WebP small/medium sizes)
   - Remove avatar button
   - Upload drag-drop zone; file-type selector restricted to images
   - Mobile: avatar card spans full width at top.
2. **Center/right column (desktop):**
   - Full name, display name, bio textarea with live char counter and markdown preview
   - Contact section: email (read-only with *Change* modal), phone, website, location inputs
   - Social links section: compact row of prefix icons
   - Locale + timezone selectors
   - Marketing consent toggle with explainer text
3. **Action row:** Save Changes / Cancel; Save requires CSRF header; shows success banner and inline field errors.
4. **Accessibility:** All inputs have `<label>` tied via htmlFor; focus indicators; sufficient color contrast; error messages announced via aria-live; keyboard navigation works in avatar crop modal.
5. **Mobile:** Stacked single column; tabs replaced with accordion sections; save button pinned to bottom nav.

### Avatar Upload Flow

- Client: select file → client-side validate size/type → crop preview (Canvas) → multipart POST to `POST /api/v1/me/avatar` with CSRF header, Content-Type `multipart/form-data` and form field `file` + optional JSON `crop`.
- Server: size, MIME, magic bytes check → malware scan → store original + generate variants (512, 256, 128, 64 px squares, WebP/AVIF) via media transform module → media_asset row created → user.avatar_id FK upsert SET NULL for old (optionally archive) → emit `user.avatar.updated` → return full avatar object with URLs for each size.

## Password Management

### Update (Authenticated)

- `PUT /api/v1/me/password` body: `{ "current_password": "...", "new_password": "...", "confirm_password": "...", "revoke_all_sessions": true }`
- Validation rules:
  - `current_password` must verify with stored hash
  - `new_password` length ≥ 12 chars (adjustable by admin policy); reject top 100k common passwords; include at least 3 character classes if enforced by policy
  - `new_password` must not equal any of user's last `N` passwords (configurable N default 10)
  - `confirm_password` matches
- Side effects: store new bcrypt/Argon2id hash; if `revoke_all_sessions=true`, invalidate all refresh tokens (rotate family key); emit audit + `user.password.changed` + `user.session.invalidated_family` events; force re-login on current session except the user-provided current session when explicitly opted-in via header.
- 2FA step-up required when user has TOTP/WebAuthn enabled.

### Reset (Unauthenticated Forgot-Flow)

1. `POST /api/v1/auth/password/forgot` with `{ email_or_username, captcha_token? }`:
   - Timing-attack-safe constant-time response regardless of user existence.
   - If valid user found, emit short-lived reset token (15 minute JWT with `jti`, single-use via Redis `SETNX jti used EX 900`), send password-reset email with signed URL containing `token` and `email_hash` salted.
   - Rate limit per email + IP: 5/hour, 15/day; audit attempt.
2. `GET /api/v1/auth/password/reset/{token}` (frontend only) — verify token validity and return status 200.
3. `POST /api/v1/auth/password/reset` with `{ token, new_password, confirm_password }`:
   - Verify JWT signature, aud claim, and Redis single-use; reject on any fail.
   - Same password strength validation as update flow.
   - On success, consume jti in Redis; invalidate sessions; emit `user.password.reset`; audit; return success with new session or redirect to login with force-change=false.

### UI (Responsive)

Password tab: current password input with toggle visibility, new password input with live strength meter (zxcvbn style), confirm password match indicator, revoke-all-sessions checkbox with warning tooltip. Mobile: stack vertically with inline helper text.

Forgot/reset flows: email step with loading state and "If your account exists, we have sent an email" generic messaging; reset step with same strength meter; after-reset success with back-to-login link.

## Privacy Settings

### Configuration (Per User)

| Key                                    | Value Type | Default  | Purpose                                                                |
|----------------------------------------|------------|----------|------------------------------------------------------------------------|
| `visibility.profile`                   | enum       | public   | `public | unlisted | private | followers_only` (simplified: public/private only in v1) |
| `visibility.email`                     | boolean    | false    | whether to show email on public profile (never shown unless admin)     |
| `visibility.contact_details`           | boolean    | false    | phone/website/location on public profile                               |
| `visibility.activity_timeline`         | boolean    | false    | activity page public                                                   |
| `search.allow_indexing`                | boolean    | true     | `X-Robots-Tag: noindex` emitted by SSR on public profile when false    |
| `tracking.personalize_ads`             | boolean    | false    | consent for personalization; maps to analytics ConsentService record   |
| `data.download_requested`              | boolean    | false    | toggled after GDPR/CCPA data download submit; set by server            |

### UI

Privacy tab with descriptive per-setting copy, toggles, and privacy-summary card: "Your profile is currently: **public** — anyone can see your name, bio, avatar, and posts. To restrict this, switch to **Private** mode below." Save with CSRF + re-verify password when reducing visibility level from public to private (low-to-high risk direction does not require reverify).

### Public Behavior

`GET /api/v1/users/{username}` enforces visibility exactly; returns 404 if private and caller is not owner/admin; when `followers_only` is enabled, follow relationship must be confirmed. `noindex` header injected based on settings.

## Activity Tracking

### Categories

`login | logout | profile_edit | avatar_update | password_change | password_reset | email_change | consent_grant | consent_withdraw | post_create | post_edit | post_publish | comment_create | twofa_enable | twofa_disable | oauth_link | oauth_unlink | impersonation_start | impersonation_end | role_change`

### Storage & Retention

Append-only `user_activity` rows; retention 395 days by default; user can request deletion of own activity before retention policy (GDPR/CCPA right to erasure via dedicated endpoint). Aggregates are produced via `user_activity_daily` materialized view when user requests it.

### UI

Activity tab with category filter chips, date from/to pickers, pagination. Each row shows icon + category label, summary text, timestamp, and a detail expander with IP partial hash, device UA bucket, location (if authorized). Export CSV button for user's own data; admin export via `user.activity.read_all`.

### Caller Rules

- Owner sees full rows with IP/UA bucketized.
- Admin with `user.activity.read_all` sees all plus request_id, full IP with geo, audit links.
- `GET /api/v1/me/activity` returns 403 when `visibility.activity_timeline` is false for a different user trying to read via public endpoint.

## Modern Security Best Practices Implementation Matrix

1. **Input validation:** allowlisted keys, strict types; regex validation; RFC formats for email/URL/BCP47/IANA/E.164; size limits; markdown sanitizer for bio with nofollow.
2. **Data encryption:**
   - Passwords: bcrypt (cost ≥ 12) or Argon2id (t=3, m=64MB, p=2) with unique per-user salt; constant-time verify.
   - Sensitive contact fields: AES-256-GCM envelope encryption with ENV/KMS key; distinct data key per row; never decrypt in logs.
   - Tokens: reset/verify/change-email JWTs with jti, Redis single-use; signed with ES256 asymmetric; stored server secret rotation schedule.
3. **Authentication checks:** every endpoint re-verifies JWT/session; self-service endpoints validate `sub == target_user_id`; admin overrides require explicit permissions. Password change / email change / avatar delete require re-verification password or recent (≤ 5 min) 2FA challenge success.
4. **CSRF + CORS + SameSite cookies / token CSRF patterns:** browser state changes require `X-CSRF-Token`; origins allowed via whitelist; cookies (if used) `HttpOnly; Secure; SameSite=Lax`.
5. **Rate limiting & account lockout:** tiered rate limits as above; progressive delay; brute force attempt logging; account lockout after N failed auth events.
6. **Audit & non-repudiation:** all changes produce audit_logs row + outbox events; include actor, impersonator, request_id, IP, before/after values; immutable append-only for activity.
7. **Session hygiene:** rotate refresh tokens; password/email changes invalidate token families; re-login for sensitive actions by policy.
8. **CSP & XSS:** strict CSP; sanitize markdown bio; frontend escape user-supplied strings; avatar upload restrict image content types + malware scan; never serve user uploads from main origin (separate asset domain).

## Responsive, User-Friendly Interface Design

- **Desktop (≥ 1024 px):** 3-column layout: avatar card, profile forms, right summary/privacy preview panel; tabs for password/privacy/activity.
- **Tablet (≥ 640 px):** 2-column avatar + forms; tabs collapse to pill-nav with icons; activity list 50 rows per page.
- **Mobile (< 640 px):** avatar card hero at top; tabs become bottom navigation or section accordions; sticky Save Changes in FAB-style bottom bar; modals for avatar crop and password re-verify.
- **Accessibility Level:** AA+ WCAG 2.2 — focus order, visible focus rings, 4.5:1 contrast, text-resize without loss, landmark regions, ARIA labels on icons only.
- **Internationalization & i18n:** All labels, copy, placeholders, error messages, date/time formatting localized via `locale` user setting.

## Unit & Integration Testing

### Unit Tests

Profile Service unit tests cover:

- Allowlist: unknown keys in edit body are rejected.
- Avatar upload: size, mime-type, magic bytes, malware-scan failures produce safe errors.
- Password strength: common passwords rejected; confirm mismatch errors; N-history enforcement (bcrypt verify against old hashes).
- Privacy filtering: public/private profiles return the correct scoped payload per caller identity.
- Activity emission: category enum, required fields, sensitive fields redacted from serialized payloads.
- Encryption round-trip: contact.phone encrypts and decrypts to same value; wrong data-key fails.
- Validation: E.164, email regex, BCP-47, timezone IANA DB lookups.

### Integration Tests

- End-to-end self-service:
  - Register/login → view `/me` → edit name/bio → save → response reflects changes; 2FA re-verification on password change.
  - Upload avatar flow: multipart → media_asset created → FK set → URLs work for 64/128/256/512px variants.
  - Password change → old refresh token invalidated; new token works; CSRF + wrong-password returns 403.
  - Forgot flow: timing-safe responses; token valid single-use; token reuse fails; reset invalidates sessions.
  - Privacy toggle public→private → public `/users/{name}` returns 404.
  - Activity entries produced for login/profile_edit/avatar_update/password_change/privacy_edit.
  - Email change: unverified change not applied; verified token applies change; old email notification sent with warning and cancel option.
- Cross-user admin:
  - Admin with `user.profile.edit` hard-resets another user's display name; audit row shows both admin user and target user.
  - Admin without permission gets 403.
- Impersonation:
  - impersonator edits target profile; target record changes; audit includes impersonator_id.
  - session expiry immediately triggers if parent session is revoked.
- Rate limiting:
  - 6 rapid password change requests trigger 429 retry-after header; counter persists across requests.
  - Per-IP forgot-password limits trigger after 5 attempts.

## Security Audit Checklist

Executed against staging before production cut:

1. Authentication bypass attempts: missing/invalid/expired JWT, missing target-user ownership check in every endpoint, admin permission mis-mapping.
2. Insecure Direct Object Reference: user A reads/changes profile of user B via parameter tampering (user_id in body, query, path).
3. Password & reset: enumeration via timing, brute force, token reuse, email/username reflection in errors, token leak via referrer headers.
4. Sensitive data exposure: contact.phone not returned unless caller authorized; encrypted fields in JSON logs; avatar not served with cookies.
5. CSRF/XSS: state-changing requests with missing/invalid CSRF; bio markdown reflects script tags; unsafe inline styles.
6. File upload: oversized avatars, non-image MIME, WebP polyglot, SVG script, zip bomb; storage path traversal; filename with NULL bytes; content-sniff bypass via `X-Content-Type-Options: nosniff`.
7. Privacy leaks: private profile fields in search results, activity visible on public route, robots tag mismatch.
8. CORS/headers: wildcard origins; missing `SameSite`; `Secure` flag on production cookies; `X-Frame-Options DENY`; CSP restrictive.
9. Session handling: password change without revocation, email change without verification, token family not rotated on user escalation.
10. Audit completeness: for every mutation endpoint, confirm audit_logs row produced and events emitted to outbox for consumers.
11. Encryption at rest: verify encrypted columns via DB dump inspection; confirm keys not in code; KMS policy restricts decryption to application identity only.
12. Third-party dependencies: SCA for known vulns in avatar processing, email delivery client, password entropy library.

## Staging Deployment & UAT Workflow

1. **Docker Compose** (per project rule): Apply migrations (`app migrate up`), run seeds with sample users + roles.
2. **Environment checks** via `app doctor` — confirm all profile dependencies (media upload bucket, email SMTP, Redis rate limit, consent service) healthy.
3. **Run full test suite:** unit + integration + security audit checklist scripts, produce jUnit + HTML report.
4. **Staging deploy** via standard pipeline: deploy image → run migrations → warm cache → smoke-test each profile endpoint via health sub-command `app doctor --profile-smoke`.
5. **UAT handoff doc:**
   - Test accounts (3 persona: author, editor, admin) with initial passwords + 2FA seed QR.
   - Step-by-step walkthrough: profile view/edit, avatar upload, password change/reset, privacy public/private toggle, activity filters + CSV export.
   - Acceptance criteria checklist:
     1. Password reset email arrives in < 1 min, token works once, expires after 15 min.
     2. Avatar uploads return valid URLs across 4 size variants, each under 200KB.
     3. Private profile responds 404 to non-owner unauthenticated callers.
     4. All edits produce audit entries with correct before/after + impersonator metadata when applicable.
     5. Mobile emulator (375×667) layout has no horizontal scroll, and all form fields reachable without pinch-zoom.
     6. Staging security audit checklist has zero critical/high items; medium items with acceptance sign-off only.
6. **Rollback plan:** Roll back image + down-migration reversible; feature flag toggles profile endpoints behind `profile.feature_enabled` setting in case of UAT blocker.
7. **Production release gate:** Signed off by QA + PM after 24 hours of staging UAT + no regressions in analytics/notification/media downstream modules.

## Cross-Reference to Existing Architecture

- Endpoints follow existing `/api/v1/me` self-service and `/api/v1/users/{slug}` public patterns in the REST contract.
- Password, audit, session rules follow `docs/architecture/security.md` and `docs/backend/api.md`.
- Avatar FK follows `media_assets.id` SET NULL rules in `docs/backend/media-relations.md` and `docs/backend/database.md`; upload pipeline uses media module adapters (R2/S3/MinIO), malware scan, and transform rules per `docs/backend/media.md`.
- Engagement activity writes share Redis cache invalidation and Watermill outbox pattern with `docs/backend/analytics.md` (but remain separate domain entity from analytics page views: user activity for self-service audit trail; analytics for site-wide aggregated stats).
