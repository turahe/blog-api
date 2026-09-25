# Phase 2 — Content Core

## Goal

Deliver the content model and its public read surface: posts, categories, tags, media upload
and listing, and Redis caching for public reads.
Index: [README.md](./README.md).

## Status

**Done** — the full post lifecycle (create, update, publish, unpublish, archive, soft delete,
restore, deterministic slug suffixes), categories, tags, media (presigned upload with content
sniffing on completion, list, delete, tags, public get, post attachments), Redis caching of
public reads (posts, categories, tags, user profiles), and all 8 profile, avatar, and email
change operations are wired. Migrations run through `00012_user_profiles.sql`.

Deferred by decision: `public.media.transform` and avatar size variants (see the media epic).
Carried to later phases: email delivery uses a logging notifier until the Phase 1 mailer
lands; 2FA step-up, profile audit or outbox events, `/me/privacy`, `followers_only`
visibility, per-user storage quota, and malware scanning are listed in
[upload-security.md](../backend/upload-security.md) and [api.md](../backend/api.md#implementation-status).

## Epic: posts

- [x] `posts` table with slug, status, and author relations
- [x] Post domain and service under `internal/core/post`
- [x] Post repository in `internal/adapters/outbound/persistence/post_repository.go`
- [x] `admin.posts.create` — `POST /api/v1/admin/posts` (guarded by `post.create`)
- [x] `admin.posts.publish` — `POST /api/v1/admin/posts/{id}/publish` (guarded by `post.publish`)
- [x] `public.posts.list` — `GET /api/v1/posts` (published only)
- [x] `public.posts.get` — `GET /api/v1/posts/{slug}`
- [x] `admin.posts.list` — `GET /api/v1/admin/posts` with status, author, and category filters
- [x] Post update endpoint
- [x] Post unpublish / archive transition
- [x] Soft delete and restore semantics consistent with [model.md](../backend/model.md)
- [x] Page pagination on `public.posts.list` and `admin.posts.list` with `meta` and `links` per the envelope
- [x] Slug uniqueness collision handling with a deterministic suffix strategy

## Epic: categories and tags

- [x] `categories`, `tags`, and `post_tags` tables
- [x] Category domain and service under `internal/core/category`
- [x] Tag repository in `internal/adapters/outbound/persistence/tag_repository.go`
- [x] `public.categories.list` — `GET /api/v1/categories`
- [x] `public.categories.get` — `GET /api/v1/categories/{slug}`
- [x] `public.tags.list` — `GET /api/v1/tags`
- [x] Admin category create, update, delete, and reorder endpoints
- [x] `admin.tags.create` — `POST /api/v1/admin/tags`
- [x] `admin.tags.update` — `PATCH /api/v1/admin/tags/{id}`
- [x] `admin.tags.merge` — `POST /api/v1/admin/tags/{id}/merge`
- [x] `admin.tags.delete` — `DELETE /api/v1/admin/tags/{id}`
- [x] Category tree / nesting via nested-set rebuild (`lft`, `rgt`, `depth`, `sort_order`; adjacency source of truth)
- [x] Attach and detach tags on post create/update via `tags: string[]` (create-or-link; omit = unchanged, `[]` = clear)

## Epic: media upload and listing

- [x] Object-storage outbound adapter for MinIO and S3-compatible backends (`internal/adapters/outbound/storage`)
- [x] `media_assets` table — migration `00005_media_assets.sql` (relations deferred; see [media-relations.md](../backend/media-relations.md))
- [x] Media domain and service under `internal/core/media`
- [x] `admin.media.create` — `POST /api/v1/admin/media` (JSON **presign**, not multipart)
- [x] `admin.media.complete` — `POST /api/v1/admin/media/{id}/complete`
- [x] `admin.media.list` — `GET /api/v1/admin/media`
- [x] `admin.media.delete` — `DELETE /api/v1/admin/media/{id}` (soft delete; clears FK refs)
- [x] `admin.media.tags.patch` — `PATCH /api/v1/admin/media/{id}/tags`
- [x] `admin.posts.media.replace` — `PATCH /api/v1/admin/posts/{id}/media` (`post_media` + cover sync)
- [x] `public.media.get` — `GET /api/v1/media/{id}` (ready assets only)
- [x] `public.media.transform` — `GET /api/v1/media/{id}/transform`, or a documented decision to
      defer — **deferred**: the route stays a `501` stub and avatars are served as the original
      image. Safe transforms need a non-cgo encoder, a decode budget, a variant cache, and a
      width allowlist; the preferred path is an image proxy or CDN in front of the bucket,
      revisited with the Phase 4 worker. Implemented in Phase 4 through imgproxy; see
      [media.md](../backend/media.md#image-transforms-imgproxy)
- [x] Upload validation: MIME allowlist, size ceiling, and filename sanitisation
- [x] Presigned-URL strategy documented in [media.md](../backend/media.md)
- [x] Media relations migration `00006_media_relations.sql` (`post_media`, `cover_image_media_id`, avatar/category FKs)

Spec: [media-management.md](../features/media-management.md) · MVP design: [2026-07-31-media-upload-design.md](../superpowers/specs/2026-07-31-media-upload-design.md)

## Epic: public read caching

- [x] Redis cache adapter for public post, category, and tag reads
- [x] Cache-key scheme including query parameters and a schema version prefix
- [x] TTL configuration per read family, surfaced in `.env.example`
- [x] Invalidation on publish, update, and delete
- [x] Cache bypass switch for debugging and for `app doctor`
- [x] Document the caching contract in [services.md](../backend/services.md)

## Epic: profiles and public user reads

- [x] `me.profile.patch` — `PATCH /api/v1/me/profile`
- [x] `me.avatar.upload` — `POST /api/v1/me/avatar` (depends on the media adapter)
- [x] `me.avatar.delete` — `DELETE /api/v1/me/avatar`
- [x] `me.email.request_change` — `POST /api/v1/me/email/request-change`
- [x] `me.email.confirm_change` — `POST /api/v1/me/email/confirm-change`
- [x] `public.users.profile` — `GET /api/v1/users/{username}`
- [x] `admin.users.profile.get` — `GET /api/v1/admin/users/{id}/profile`
- [x] `admin.users.profile.patch` — `PATCH /api/v1/admin/users/{id}/profile`

Specs: [user-profile-management.md](../features/user-profile-management.md),
[user-profile.md](../backend/user-profile.md)

## Dependencies and order

1. The object-storage adapter blocks every media endpoint and avatar upload.
2. Email change needs the real mailer from Phase 1 to deliver its token; until then the
   `EmailChangeNotifier` port logs a masked address (never the token).
3. Caching should land after admin post mutation endpoints exist, so invalidation has real triggers.
4. Tag attach/detach is wired on admin post create/update; public tag list uses `TagService`.

## Cross-cutting

- [x] Bind post, category, tag, and media routes in `routes.Register*`, annotate handlers, and
      publish them with `make swagger` + `make routes-check`
- [x] Handler tests for each newly wired operation ID
- [x] Repository tests against a real database for pagination and filtering
- [x] Upload security review — MIME sniffing, path traversal, and quota abuse
      ([upload-security.md](../backend/upload-security.md))
- [x] Keep [api.md](../backend/api.md) in step as operations move off the 501 stub

## References

| Topic | Doc |
| --- | --- |
| API surface | [api.md](../backend/api.md) |
| Data models | [model.md](../backend/model.md) |
| Media | [media.md](../backend/media.md) |
| Media relations | [media-relations.md](../backend/media-relations.md) |
| Validation rules | [validation.md](../backend/validation.md) |
| Services | [services.md](../backend/services.md) |
| Contract testing | [http-and-contracts.md](../testing/http-and-contracts.md) |
