# Services

## Core Services

- `AuthService`: login, token/session renewal, auth validation, TOTP/WebAuthn 2FA, forgot/reset password orchestration, email-change token verification
- `UserService`: identity CRUD, status transitions, admin cross-user actions
- `UserProfileService`: self-service profile display + partial patch with allowlist; avatar attach/detach; locale/timezone/marketing_consent updates; contact/social fields with encryption for phone; read/write through Hex ports
- `UserPrivacyService`: privacy_settings CRUD with allowlisted keys; visibility direction gating (public→private requires password proof/2FA); public profile field scoping
- `UserPasswordService`: password strength validator (reject common + history N=10 + character-class policy); change with current-proof; admin reset; invalidate refresh token families
- `UserActivityService`: append-only activity log; GDPR export/erase; category filters + CSV streaming; impersonator metadata propagation from impersonation middleware context
- `RoleService`: roles, permissions, assignments
- `PostService`: post CRUD, publish flow, attachment associations via post_media, cover image consistency, revision snapshot orchestration
- `PostRevisionService`: create post revisions with snapshot + diff + changelog, list/get revisions, restore previous revision as new revision
- `PostSEOService`: SEO config CRUD and validation, live preview (SERP/OG/Twitter), structured SEO meta rendering, slug uniqueness and redirect hooks
- `CommentService`: moderation flow
- `MediaService`: uploads, metadata, dynamic transforms, deletion, tagging, usage lookup (where referenced across users/categories/posts)
- `NotificationService`: dispatch email or future messaging notifications (email-change notices, password-reset, password-change confirmations, marketing-consent receipt delivery)
- `AnalyticsIngestionService`: consent-aware tracking events for views, search, navigation
- `AnalyticsQueryService`: dashboard widgets, trend series, search analytics, retention cohorts
- `AnalyticsRealtimeService`: admin live activity feed and SSE fan-out
- `ConsentService`: analytics consent grant, rejection, withdrawal, and erasure hooks; propagates profile.marketing_consent changes into consent audit trail
- `SettingsService`: schema-validated reads and partial updates for global admin settings
- `ImpersonationService`: superadmin-only start/stop impersonation, session validation, audit/events
- `CategoryService`: category CRUD + nested set management plus category cover image_id association
- `FieldEncryptionPort`: AES-256-GCM envelope encryption for `user_profiles.encrypted_contact_phone` and any future encrypted contact fields; per-row data keys + KMS/ENV master key rotation support
- `RateLimiterPort`: profile endpoint tiers; forgot/reset buckets per email + ip, export/erase throttles
- `AvatarMediaServicePort`: delegate upload/malware-scan/variant-generation to MediaService while keeping profile domain decoupled; returns `MediaAsset` value object with signed variant URLs
- `UserSessionRepository`: refresh-token family rotation + bulk invalidation on password/email/avatar/security-setting changes

## Service Rules

- services orchestrate use cases
- services depend on ports, not adapters
- services return domain or application-level errors
- services may publish domain events through outbound ports
- analytics services must enforce consent gating before persistence
- admin-facing analytics services must enforce RBAC before responding
- `SettingsService` must re-check permissions, validate keys against an allowlisted schema, and write audit/history before emitting events
- `ImpersonationService` must enforce `superadmin` role check first, then `impersonation.*` permissions, step-up 2FA where enabled, CSRF integrity, and target eligibility
- `UserProfileService` must reject unknown patch keys and only allow fields on the documented allowlist; it must not allow editing user `status`, `role`, `password_hash`, or other non-self-service fields
- `UserProfileService` must call `AvatarMediaServicePort` (delegates to MediaService) inside the same transaction when uploading or detaching an avatar so users.avatar_id FK and media_asset row stay consistent, then emit `user.avatar.updated` event
- `UserPrivacyService` must require password proof or recent 2FA for visibility direction `public -> private` or any setting that reduces the amount of data previously exposed (contact toggles off after on do not require proof, but contact toggles on after off do)
- `UserPasswordService` must compare history hashes against `user_password_history.last 10` using constant-time verifiers; new hash must pass strength (min 12 chars, not in common password bloom set)
- `UserActivityService` must never update a row after creation; erasure requests must zero PII-bearing fields (ip_address, user_agent_bucket, geo_* in detail_jsonb) or redact and move to archive table; audit trail of the erasure itself is preserved in audit_logs
- every mutation in profile/privacy/password/email change MUST append an audit_logs row AND an outbox event row inside the same DB transaction via the outbox pattern; no single mutation produces events without audit

## Cross-Cutting Concerns

- audit logging
- authorization checks
- validation
- id generation
- clock abstraction
- privacy-aware data minimization for analytics
- media association lifecycle:
  - when setting `avatar_id`, `image_id`, `post_media` change, emit events and invalidate entity-level caches
  - when deleting media, ensure referential integrity via SET NULL / CASCADE rules and clear any post_media joins
- `PostService` must call `PostRevisionService.CreateRevision` inside the same transaction whenever a post is created, updated, restored, published, or archived so that revision snapshots remain consistent with the current post state
- `PostSEOService` must participate in the same transaction as post updates and revision creation so SEO snapshot, post row, and revision row all agree; slug changes additionally emit `blog.post.slug_changed` through outbox
- self-service profile caches: `user:me:{user_id}:include:{mask}` TTL 30s invalidated on profile/privacy/avatar/password/email events; `user:public:{name}` TTL 900s invalidated on profile/privacy events
- profile endpoints must apply CSRF, rate limit, step-up, and audit before the service is called; middleware composition: auth → impersonation → csrf → rate limit → step-up → audit; service re-checks ownership and permissions defensively
