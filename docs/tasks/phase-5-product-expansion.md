# Phase 5 — Product Expansion

## Goal

Broaden the product surface: richer admin capabilities, search integration, the admin analytics
dashboard, privacy and consent management, and media pipeline improvements.
Index: [README.md](./README.md).

## Status

**In progress** — admin settings, post versioning, post SEO, and analytics consent are implemented.
`/me/privacy` settings, the asynchronous `/me` data export and erasure, PostgreSQL post
search, staff impersonation, and newsletter subscriptions are implemented.

## Decisions

| Topic | Decision |
| --- | --- |
| Settings scope | Typed catalogue in code (site, content, media, analytics, notifications, SEO, security). Secrets, storage and SMTP credentials, and provider switching stay in env vars. Keys that enforce behaviour ship with the code that enforces them. |
| Search backend | PostgreSQL full-text search: a stored generated `tsvector` column with a GIN index, `websearch_to_tsquery` ranking, `ts_headline` highlights, behind a swappable search port. Language from `SEARCH_LANGUAGE` (default `simple`); changing it needs `app search reindex`. |
| Newsletter delivery | Built-in sending through the SMTP mailer and worker, plus a generic HMAC-signed `custom_http` adapter. Other ESPs are documented extension points. |
| Media variants | Named imgproxy presets (width, format) defined in settings and returned as signed URLs on media responses; no stored derivatives. Orphan cleanup and storage usage reporting are built. |
| Impersonation | A server-side impersonation session row and a separate short-lived access token with `sub` = target and an RFC 8693 `act` claim for the staff member. No refresh token; audit records store actor and subject. Holders of `impersonation.start` may impersonate active users whose permissions are a subset of theirs, never admins or other impersonators; step-up is the current password plus a TOTP or backup code when 2FA is enabled; a 10–255 character reason is required. `IMPERSONATION_TTL` (default 1h, 5m–2h) is a hard ceiling. Credential, 2FA, OAuth, privacy, export, erase, logout, refresh, and chained impersonation operations are refused to impersonation tokens. |
| Erasure | Anonymize: delete activity, consents, sessions, and tokens; scrub email, name, avatar, and phone; keep posts and comments attributed to "Deleted user"; keep audit rows with the actor id only. Runs asynchronously. Exports are a JSON archive in object storage behind an expiring link. |

## Epic: settings management

- [x] Settings schema and storage — see [settings.md](../backend/settings.md)
- [x] Settings service with typed groups and validation
- [x] `admin.settings.get` — `GET /api/v1/admin/settings`
- [x] `admin.settings.put` — `PUT /api/v1/admin/settings`
- [x] `admin.settings.history` — `GET /api/v1/admin/settings/history`
- [x] Cached read path with invalidation on write
- [x] Guard secret-bearing settings so they are never returned in plain text — secrets are not
      settings at all; `server_only` keys are never returned or writable over HTTP

Spec: [settings-management.md](../features/settings-management.md)

## Epic: impersonation

- [x] Impersonation session model and audit trail — see [impersonation.md](../backend/impersonation.md)
- [x] `admin.impersonation.start` — `POST /api/v1/admin/impersonation/start`
- [x] `admin.impersonation.stop` — `POST /api/v1/admin/impersonation/stop`
- [x] `admin.impersonation.current` — `GET /api/v1/admin/impersonation/current`
- [x] Distinguish actor from subject in JWT claims and in every audit record
- [x] Hard ceiling on impersonation session lifetime
- [x] Block privilege escalation: an impersonator must not gain permissions they lack
- [ ] Security review before enabling in production (needs an independent reviewer; the threat
  model row is in [authn-authz.md](../security/authn-authz.md))

Specs: [impersonation.md](../features/impersonation.md), [impersonation.md](../backend/impersonation.md)

## Epic: post versioning

- [x] Revision storage and diffing — see [post-versions.md](../backend/post-versions.md)
- [x] `admin.posts.revisions.list` — `GET /api/v1/admin/posts/{id}/revisions`
- [x] `admin.posts.revisions.get` — `GET /api/v1/admin/posts/{id}/revisions/{revisionId}`
- [x] `admin.posts.revisions.restore` — `POST /api/v1/admin/posts/{id}/revisions/{revisionId}/restore`
- [x] Capture a revision on every post mutation, including publish transitions
- [x] Revision retention or pruning policy — kept until an admin runs `app revisions prune --keep N`

Spec: [post-versioning.md](../features/post-versioning.md)

## Epic: post SEO

- [x] SEO fields storage — see [post-seo.md](../backend/post-seo.md)
- [x] `admin.posts.seo.get` — `GET /api/v1/admin/posts/{id}/seo`
- [x] `admin.posts.seo.update` — `PUT /api/v1/admin/posts/{id}/seo`
- [x] `admin.posts.seo.preview` — `POST /api/v1/admin/posts/{id}/seo/preview`
- [x] `public.posts.seo_meta` — `GET /api/v1/posts/{slug}/seo-meta`
- [x] Derive sensible defaults from post title, excerpt, and cover media
- [x] Sanitise SEO text so it cannot inject markup into rendered meta tags

Spec: [post-seo.md](../features/post-seo.md)

## Epic: newsletter subscriptions

- [x] Subscriber and issue storage with double opt-in state — see [newsletter.md](../backend/newsletter.md)
- [x] `public.newsletter.subscribe` — `POST /api/v1/newsletter/subscribe`
- [x] `public.newsletter.confirm` — `POST /api/v1/newsletter/confirm`
- [x] `public.newsletter.confirm_resend` — `POST /api/v1/newsletter/confirm/resend`
- [x] `public.newsletter.unsubscribe` — `POST /api/v1/newsletter/unsubscribe`
- [x] `public.newsletter.preferences.get` — `GET /api/v1/newsletter/preferences/{token}`
- [x] `public.newsletter.preferences.patch` — `PATCH /api/v1/newsletter/preferences/{token}`
- [x] `me.newsletter.subscribe` — `POST /api/v1/me/newsletter/subscribe`
- [x] `me.newsletter.unsubscribe` — `POST /api/v1/me/newsletter/unsubscribe`
- [x] `me.newsletter.subscriptions.list` — `GET /api/v1/me/newsletter/subscriptions`
- [x] `admin.newsletter.subscribers.list` — `GET /api/v1/admin/newsletter/subscribers`
- [x] `admin.newsletter.subscribers.get` — `GET /api/v1/admin/newsletter/subscribers/{id}`
- [x] `admin.newsletter.subscribers.delete` — `DELETE /api/v1/admin/newsletter/subscribers/{id}`
- [x] `admin.newsletter.issues.list` — `GET /api/v1/admin/newsletter/issues`
- [x] `admin.newsletter.issues.get` — `GET /api/v1/admin/newsletter/issues/{id}`
- [x] `admin.newsletter.issues.patch` — `PATCH /api/v1/admin/newsletter/issues/{id}`
- [x] `admin.newsletter.issues.send` — `POST /api/v1/admin/newsletter/issues`
- [x] `admin.newsletter.provider_config.get` — `GET /api/v1/admin/newsletter/provider-config`
- [x] `admin.newsletter.provider_config.put` — `PUT /api/v1/admin/newsletter/provider-config`
- [x] Unsubscribe tokens must be unguessable and single-purpose (32 random bytes, SHA-256 on
      disk, one purpose each)
- [x] Send issues through a Phase 4 consumer, never inline in the request (`newsletter-dispatch`)

Spec: [newsletter-subscriptions.md](../features/newsletter-subscriptions.md)

## Epic: search integration

- [x] Choose the search backend: database full-text versus an external engine, and record the decision
- [x] Search port in the core layer with a swappable outbound adapter (`postports.Searcher`)
- [x] Search query support on `public.posts.list` (`?q=`)
- [x] Indexing on publish, update, and delete — `posts.search_vector` is a stored generated
      column, so it changes with every write and needs no consumer; an external engine adapter
      would subscribe to `blog.post.revision.created`
- [x] Reindex command exposed through `cmd` (`app search reindex`, for `SEARCH_LANGUAGE` changes)
- [x] Relevance and highlighting expectations documented — [search.md](../backend/search.md)

## Epic: privacy and consent management

- [x] Consent storage with purpose, version, and timestamp
- [x] `analytics.consent.store` — `POST /api/v1/analytics/consent`
- [x] `analytics.consent.get` — `GET /api/v1/analytics/consent`
- [x] `analytics.consent.withdraw` — `DELETE /api/v1/analytics/consent/{id}`
- [x] `me.privacy.get` — `GET /api/v1/me/privacy`
- [x] `me.privacy.update` — `PUT /api/v1/me/privacy`
- [x] `me.activity.export` — `GET /api/v1/me/activity/export`
- [x] `me.activity.erase` — `POST /api/v1/me/activity/erase`
- [x] Enforce consent at the analytics ingest boundary, not only in the UI
- [x] Export and erase must run asynchronously with a retrievable result (`privacy_requests`
      queue processed by the `privacy-requests` scheduler job)

## Epic: media pipeline improvements

- [x] Asynchronous derivative generation triggered by `media.uploaded` — superseded by the
      decision above: imgproxy renders derivatives on request behind the CDN, so nothing is
      generated or stored on upload
- [x] Variant set definition (thumbnail, card, hero) driven by settings (`media.variants`;
      signed URLs in a `variants` map on media responses)
- [x] Optional format conversion and compression policy (`media.default_transform_format`,
      `media.default_transform_quality`)
- [x] Orphan media detection and cleanup job (`media-orphans`: abandoned uploads and trash
      past `MEDIA_PURGE_AFTER`; unreferenced ready assets listed by `?unused=true`, never
      auto-deleted)
- [x] Storage usage reporting per user (`admin.media.usage`; single-tenant, so no per-tenant
      breakdown)

## Dependencies and order

1. Settings gates newsletter provider config and media variant definitions.
2. Post revisions and SEO both depend on the Phase 2 admin post update endpoint.
3. Newsletter sending depends on the Phase 1 mailer and the Phase 4 consumer runtime.
4. Search indexing depends on Phase 4 domain events.
5. Consent enforcement must land before Phase 6 telemetry ingest goes live.
6. Impersonation depends on a working audit writer from Phase 3.

## Cross-cutting

- [ ] Bind admin, newsletter, and privacy handlers in `routes.Register*`, annotate them, then
      `make swagger` + `make routes-check`
- [ ] Authorization tests for every new admin operation
- [ ] Impersonation-specific security review and audit assertions
- [ ] Token-handling review for newsletter and privacy tokens
- [ ] Document the privacy stance in [overview.md](../security/overview.md)

## References

| Topic | Doc |
| --- | --- |
| API surface | [api.md](../backend/api.md) |
| Settings | [settings.md](../backend/settings.md) |
| Post versions | [post-versions.md](../backend/post-versions.md) |
| Post SEO | [post-seo.md](../backend/post-seo.md) |
| Search | [search.md](../backend/search.md) |
| Impersonation | [impersonation.md](../backend/impersonation.md) |
| Newsletter | [newsletter.md](../backend/newsletter.md) |
| Media | [media.md](../backend/media.md) |
| Email | [email.md](../backend/email.md) |
| Product requirements | [PRD.md](../product/PRD.md) |
