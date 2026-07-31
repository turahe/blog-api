# Phase 2 — Content Core

## Goal

Deliver the content model and its public read surface: posts, categories, tags, media upload
and listing, and Redis caching for public reads.
Index: [README.md](./README.md).

## Status

**Partial** — post create/publish, admin post list/update, and public post/category/tag reads are wired.
Media upload, list/delete/tags, public get, and post featured-media replace are wired;
on-the-fly transform and public-read caching remain open.

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
- [ ] Post unpublish / archive transition
- [ ] Soft delete and restore semantics consistent with [model.md](../backend/model.md)
- [ ] Cursor or page pagination on both list endpoints with `meta` populated per the envelope
- [ ] Slug uniqueness collision handling with a deterministic suffix strategy

## Epic: categories and tags

- [x] `categories`, `tags`, and `post_tags` tables
- [x] Category domain and service under `internal/core/category`
- [x] Tag repository in `internal/adapters/outbound/persistence/tag_repository.go`
- [x] `public.categories.list` — `GET /api/v1/categories`
- [x] `public.categories.get` — `GET /api/v1/categories/{slug}`
- [x] `public.tags.list` — `GET /api/v1/tags`
- [ ] Admin category create, update, delete, and reorder endpoints
- [ ] Admin tag create, merge, and delete endpoints
- [ ] Category tree / nesting support if the product requires it — confirm against [PRD.md](../product/PRD.md)
- [ ] Attach and detach tags when creating or updating a post

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
- [ ] `public.media.transform` — `GET /api/v1/media/{id}/transform` (deferred)
- [x] Upload validation: MIME allowlist, size ceiling, and filename sanitisation
- [x] Presigned-URL strategy documented in [media.md](../backend/media.md)
- [x] Media relations migration `00006_media_relations.sql` (`post_media`, `cover_image_media_id`, avatar/category FKs)

Spec: [media-management.md](../features/media-management.md) · MVP design: [2026-07-31-media-upload-design.md](../superpowers/specs/2026-07-31-media-upload-design.md)

## Epic: public read caching

- [ ] Redis cache adapter for public post, category, and tag reads
- [ ] Cache-key scheme including query parameters and a schema version prefix
- [ ] TTL configuration per read family, surfaced in `.env.example`
- [ ] Invalidation on publish, update, and delete
- [ ] Cache bypass switch for debugging and for `app doctor`
- [ ] Document the caching contract in [services.md](../backend/services.md)

## Epic: profiles and public user reads

- [ ] `me.profile.patch` — `PATCH /api/v1/me/profile`
- [ ] `me.avatar.upload` — `POST /api/v1/me/avatar` (depends on the media adapter)
- [ ] `me.avatar.delete` — `DELETE /api/v1/me/avatar`
- [ ] `me.email.request_change` — `POST /api/v1/me/email/request-change`
- [ ] `me.email.confirm_change` — `POST /api/v1/me/email/confirm-change`
- [ ] `public.users.profile` — `GET /api/v1/users/{username}`
- [ ] `admin.users.profile.get` — `GET /api/v1/admin/users/{id}/profile`
- [ ] `admin.users.profile.patch` — `PATCH /api/v1/admin/users/{id}/profile`

Specs: [user-profile-management.md](../features/user-profile-management.md),
[user-profile.md](../backend/user-profile.md)

## Dependencies and order

1. The object-storage adapter blocks every media endpoint and avatar upload.
2. Avatar and email-change flows need the real mailer from Phase 1.
3. Caching should land after admin post mutation endpoints exist, so invalidation has real triggers.
4. Tag attach/detach depends on admin post update.

## Cross-cutting

- [ ] Contract-first updates to `paths/posts.yaml`, `paths/categories.yaml`, `paths/media.yaml`
      before handlers, then `make routes`
- [ ] Handler tests for each newly wired operation ID
- [ ] Repository tests against a real database for pagination and filtering
- [ ] Upload security review — MIME sniffing, path traversal, and quota abuse
- [ ] Update [api.md](../backend/api.md) as operations move off the 501 stub

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
