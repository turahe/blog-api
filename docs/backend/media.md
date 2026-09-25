# Media Backend Module

## Purpose

The media module handles upload, storage, metadata, dynamic image transformation, cache strategy, and delivery for image assets.

## Storage Requirements

- support Cloudflare R2
- support AWS S3
- support S3-compatible providers such as MinIO and DigitalOcean Spaces
- expose a single storage interface in the application core

## Dynamic Image Delivery

- no resized derivatives are stored; imgproxy renders them on request (see
  [Image transforms](#image-transforms-imgproxy))
- resize (width, aspect kept), quality, and format conversion to WebP, AVIF, JPEG, or PNG;
  crop and rotate are not offered
- named presets are returned as signed URLs on media responses (see [Variants](#variants))
- caching is the CDN in front of imgproxy, keyed by the signed URL

## Upload Workflow (MVP — implemented)

Admin upload uses **presigned PUT** (bytes go client → object storage, not through the API).

1. `POST /api/v1/admin/media` (`admin.media.create`, permission `media.create`) with JSON:
   `originalFilename`, `contentType`, `sizeBytes`, optional `tags`
2. API validates MIME allowlist / max size / filename, creates a `pending` `media_assets` row, returns
   `uploadUrl`, `requiredHeaders`, `expiresAt`, `mediaId`, `storageKey`, `disk`
3. Client `PUT`s the file to `uploadUrl` with the required headers (at least `Content-Type`)
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

**Deferred:** malware scan. Sizes come from the transform endpoint (see below).

## Image transforms (imgproxy)

`GET /api/v1/media/{id}/transform?w=256&format=webp` (`public.media.transform`) answers
`302` to a signed imgproxy URL. The API never decodes images: imgproxy fetches the original
from the private bucket (`s3://S3_BUCKET/<storage_key>`), resizes, and encodes.

- `w` is required and must be in `MEDIA_TRANSFORM_WIDTHS`; the height follows the aspect
  ratio and images are never enlarged. `format` is optional (`webp`, `avif`, `jpeg`, `png`);
  omitted keeps the source format. Anything else is `400 validation_error`.
- Only `ready` JPEG, PNG, GIF, WebP, and AVIF assets are transformed (SVG is `400`); other
  assets are `404` like `public.media.get`.
- The URL is signed with `IMGPROXY_KEY`/`IMGPROXY_SALT` (HMAC-SHA256 over salt and path), so
  the parameters and source cannot be changed. It carries `exp:`: it expires between one and
  two `MEDIA_TRANSFORM_URL_TTL` after issue and is identical within a TTL window, so caches
  keep hitting.
- Without `IMGPROXY_URL` the endpoint answers `501 media.transform_disabled`.

**Caching.** Put a CDN in front of imgproxy (`IMGPROXY_URL` is its public origin). The cache
key is the URL, which already encodes the source key and the transform, so derivatives are
cached per source and parameters without storing them in the bucket. imgproxy sends
`Cache-Control: max-age=IMGPROXY_TTL`; keep it at or below `MEDIA_TRANSFORM_URL_TTL`. The
redirect itself is `Cache-Control: public, max-age=300`.

**Invalidation.** Deleting media makes the endpoint `404` at once (after the 300 s redirect
cache). URLs already handed out keep working until their `exp`, at most two TTLs, and CDN
copies until `IMGPROXY_TTL`. For immediate removal, purge the CDN by the asset's storage key.
Uploads never overwrite an object (each asset has its own key), so a replaced image gets new
URLs.

**Operations.** Run imgproxy with `IMGPROXY_USE_S3=true`, the storage credentials,
`IMGPROXY_ALLOWED_SOURCES=s3://`, and decode limits (`IMGPROXY_MAX_SRC_RESOLUTION`,
`IMGPROXY_MAX_SRC_FILE_SIZE`). Locally: `docker compose --profile imgproxy up -d imgproxy`
and `IMGPROXY_URL=http://127.0.0.1:8081`.

**Quality and default format.** Every transform URL carries `q:<media.default_transform_quality>`
(site setting, default 80). A request without `format` uses `media.default_transform_format`
(default `original`, which keeps the source format). Both apply to presets too.

## Variants

The site setting `media.variants` lists named presets as `name:width` or `name:width:format`
(default `thumbnail:320:webp`, `card:640:webp`, `hero:1280:webp`; at most 10, width 16–4096,
names unique). When imgproxy is configured, each ready raster asset in these responses gains a
`variants` object mapping preset name to a signed imgproxy URL:

- admin media list, complete, and tag update (`admin.media.list`, `admin.media.complete`,
  `admin.media.tags.patch`)
- `public.media.get`
- the `media` rows of `admin.posts.media.replace`

SVG, pending, and failed assets get an empty object. Without `IMGPROXY_URL` the field is
omitted. Preset widths do not have to be in `MEDIA_TRANSFORM_WIDTHS`: that allowlist bounds
what anonymous callers of the transform endpoint can ask for, while presets are chosen by an
admin. The URLs follow the same signing, expiry window, and caching as the transform endpoint.
Changing a preset changes its URLs, so the CDN renders the new size on the next request;
nothing is stored or regenerated.

## Orphan cleanup

The hourly `media-orphans` job in `app scheduler` (see [jobs.md](jobs.md)) deletes, up to 200
of each per run:

- **abandoned uploads**: assets never completed (`pending` or `failed`) whose presign expired
  more than 24 hours ago; the client can no longer upload, so the row and any partial object
  are deleted
- **trashed assets**: assets soft-deleted longer than `MEDIA_PURGE_AFTER` (default 30 days,
  `0` keeps them). References were cleared at soft delete.

The object is deleted first and the row second, re-checking the condition, so an asset that
was completed or restored in between is kept, and a failed object delete leaves the row for
the next run. The job is skipped when media storage is not configured.

Ready assets that nothing references are **not** deleted: post bodies are Markdown and are not
parsed for image links, so "unreferenced" is only a hint. `GET /api/v1/admin/media?unused=true`
lists ready assets that no avatar, category image, post cover, post media row, or post SEO
image points at, for an admin to review and delete.

## Storage usage

`GET /api/v1/admin/media/usage` (`admin.media.usage`, permission `media.usage.read`, admin and
editor) reports stored assets:

- `total`: `count` and `bytes`
- `byStatus`: `pending`, `ready`, `failed`, and `deleted` (soft-deleted, not yet purged)
- `byContentType`
- `topUploaders`: `userId`, `username`, `count`, `bytes`, largest first; assets without an
  uploader (deleted users) are one row with `userId: null`. `top` sets the length (default 10,
  max 100).

`userId` limits the report to one uploader. The response is `Cache-Control: no-store`. It is
a report only; there are no quotas.

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
