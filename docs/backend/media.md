# Media Backend Module

## Purpose

The media module handles upload, storage, metadata, dynamic image transformation, cache strategy, and delivery for image assets.

## Storage Requirements

- support Cloudflare R2
- support AWS S3
- support S3-compatible providers such as MinIO and DigitalOcean Spaces
- expose a single storage interface in the application core

## Dynamic Image Delivery

- no pre-generated resized variants are required
- variants are generated when a transform endpoint is requested
- support resize, crop, rotate, quality adjustment, and format conversion
- support optimized delivery in WebP and AVIF where available

## Cache Strategy

Recommended tiers:

- in-memory cache for hot variants
- Redis or equivalent distributed cache for repeated transforms
- optional object-storage-backed cached variants for heavy traffic paths

## Upload Workflow (MVP — implemented)

Admin upload uses **presigned PUT** (bytes go client → object storage, not through the API).

1. `POST /api/v1/admin/media` (`admin.media.create`, permission `media.create`) with JSON:
   `original_filename`, `content_type`, `size_bytes`, optional `tags`
2. API validates MIME allowlist / max size / filename, creates a `pending` `media_assets` row, returns
   `upload_url`, `required_headers`, `expires_at`, `media_id`, `storage_key`, `disk`
3. Client `PUT`s the file to `upload_url` with the required headers (at least `Content-Type`)
4. `POST /api/v1/admin/media/{id}/complete` (`admin.media.complete`, same permission) — API `HeadObject`s
   storage, re-checks type and size, sniffs the first 512 bytes (a mismatch with the declared type
   is a `400`), then marks the asset `ready` (or returns `media.upload_incomplete` / `media.upload_expired`)

Presign is rate limited per user (`media.presign`, 60/min).

Config: `S3_*` + `MEDIA_ALLOWED_MIME_TYPES`, `MEDIA_MAX_UPLOAD_BYTES`, `MEDIA_PRESIGN_TTL` — see
[config.md](../deployment/config.md). Design: [2026-07-31-media-upload-design.md](../superpowers/specs/2026-07-31-media-upload-design.md).
Security review: [upload-security.md](upload-security.md).

## Server-side image upload (avatars)

`POST /api/v1/me/avatar` is the one multipart path through the API
(`media.Service.UploadImage`). The type comes from magic bytes (JPEG, PNG, GIF, WebP), the key
extension is rewritten to match, dimensions are read from the header (max 8192 px per side),
and the asset is stored `ready` with its width, height and SHA-256. The size cap is
`AVATAR_MAX_BYTES`. The bucket must exist before the first upload — locally, create
`S3_BUCKET` in RustFS or MinIO once.

**Deferred:** malware scan, outbox events, and on-the-fly transform (see below).

## Transform decision (Phase 2)

`public.media.transform` stays a `501` stub and avatar size variants (64–512 px) are not
produced; clients use the original. Doing transforms properly needs work that does not belong
in Phase 2:

- a non-cgo resizer and encoder (WebP encoding needs cgo or an external service),
- a bounded worker pool and a decoded-pixel budget, because decoding is where image bombs and
  CPU amplification happen,
- a variant cache (Redis or object storage) and invalidation tied to media delete,
- a parameter allowlist (fixed width steps) so the variant space cannot be enumerated.

The preferred direction is to delegate to a CDN or image proxy (imgproxy, Cloudflare Images)
in front of the bucket rather than decode in the API process. Revisit together with the
Phase 4 worker runtime.

## Featured media on posts

`PATCH /api/v1/admin/posts/{id}/media` (`admin.posts.media.replace`, permission `post.update`):

- Replaces all `post_media` rows for the post (`cover` | `inline_image` | `attachment`)
- Only **ready** media assets may be attached
- At most one `kind=cover` item; syncs `posts.cover_image_media_id`
- Migration: `00006_media_relations.sql`

## Upload Workflow (target / full)

1. validate file size and mime type
2. read and inspect image metadata
3. scan file for malware
4. store original object
5. persist metadata
6. emit upload event

## Metadata and Tagging

Store:

- original filename
- storage key
- content type
- size
- dimensions
- checksum
- tags

## Failure Rules

- corrupted images return safe validation errors
- invalid transform parameters return client-safe errors
- storage failures return retriable server errors where appropriate
- malware scan failures reject the file before finalization
