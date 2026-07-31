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
   storage, then marks the asset `ready` (or returns `media.upload_incomplete` / `media.upload_expired`)

Config: `S3_*` + `MEDIA_ALLOWED_MIME_TYPES`, `MEDIA_MAX_UPLOAD_BYTES`, `MEDIA_PRESIGN_TTL` — see
[config.md](../deployment/config.md). Design: [2026-07-31-media-upload-design.md](../superpowers/specs/2026-07-31-media-upload-design.md).

**Deferred (not in MVP):** multipart through API, malware scan, list/delete/tags, on-the-fly transform,
outbox events, `post_media` wiring.

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
