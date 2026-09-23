# Media Upload MVP Design

Date: 2026-07-31
Status: implemented — plan at [2026-07-31-media-upload.md](../plans/2026-07-31-media-upload.md)
Scope: Presigned PUT upload + explicit complete for admin media assets (MinIO / S3-compatible).

## Goal

Ship a working admin media upload path: create a pending asset, return a presigned PUT URL, then finalize metadata after the client uploads bytes to object storage. Land the hexagonal media module, `media_assets` table, and S3-compatible outbound adapter without list/delete/transform/malware/outbox.

## Decisions

| Topic | Choice |
| --- | --- |
| Slice | Upload MVP only (not full Phase 2 media epic) |
| Bytes to storage | Presigned PUT (client → object store) |
| Finalize | Explicit `admin.media.complete` |
| Existing multipart `admin.media.create` | Replace with JSON presign start |
| MIME / size policy | Configurable env; defaults images-only, 10 MiB |
| Architecture | Hexagonal `internal/core/media` + storage/repo adapters |

## Non-goals

- `admin.media.list` / `delete` / `tags.patch`
- `public.media.get` / `transform`
- `admin.posts.media.replace`, avatar / category FKs, `post_media`
- Malware scanning
- Outbox / `media.uploaded` event
- Nested-set media folders
- Multipart upload through the API

## Architecture

```text
Client
  │ 1. POST /api/v1/admin/media
  ▼
HTTP adapter → media.Service.PresignUpload
                 ├─ validate MIME / size / filename
                 ├─ insert media_assets (status=pending)
                 └─ ObjectStorage.PresignPut
  │ 201 { media_id, upload_url, required_headers, expires_at, storage_key }

Client PUT upload_url → MinIO / S3 / R2 / Spaces

  │ 2. POST /api/v1/admin/media/{id}/complete
  ▼
HTTP adapter → media.Service.CompleteUpload
                 ├─ ObjectStorage.HeadObject (must exist)
                 ├─ update size / content_type / status=ready
                 └─ optional cheap image dimension probe
  │ 200 EnvelopeMediaResponse
```

### Packages

| Package | Responsibility |
| --- | --- |
| `internal/core/media/domain` | `MediaAsset`, status enum, validation helpers |
| `internal/core/media/ports` | `Repository`, `ObjectStorage` |
| `internal/core/media/service` | `PresignUpload`, `CompleteUpload` |
| `internal/adapters/outbound/storage` | AWS SDK v2 S3-compatible client (path-style for MinIO) |
| `internal/adapters/outbound/persistence` | GORM / SQL media repository |
| `internal/adapters/inbound/http` | Handlers + Casbin `media.create` |
| `internal/platform/migrations/sql/00005_media_assets.sql` | Schema |
| `internal/platform/config` | Load `S3_*` and `MEDIA_*` |

Core must not import Gin, GORM, Redis, Watermill, or AWS SDK.

## Schema

Table `media_assets` (no nested-set columns in this slice):

| Column | Type / notes |
| --- | --- |
| `id` | uuid PK |
| `storage_key` | text UNIQUE NOT NULL |
| `original_filename` | text NOT NULL |
| `content_type` | text NOT NULL |
| `size_bytes` | bigint NOT NULL DEFAULT 0 |
| `width` / `height` | int nullable |
| `checksum_sha256` | text nullable (optional in MVP) |
| `disk` | text CHECK IN (`s3`,`r2`,`minio`,`do_spaces`) |
| `status` | text CHECK IN (`pending`,`ready`,`failed`) |
| `uploaded_by` | uuid NULL REFERENCES users(id) ON DELETE SET NULL |
| `tags` | text[] NOT NULL DEFAULT '{}' |
| `presign_expires_at` | timestamptz NULL (set on presign) |
| `created_at` / `updated_at` | timestamptz |
| `deleted_at` | timestamptz NULL (soft delete) |

Indexes: unique `storage_key`; partial unique not required; btree on `uploaded_by`, `status`, `deleted_at`.

## API contract

Update `paths/media.yaml` and `components/schemas/Media.yaml`, then committed OpenAPI bundles under `contracts/` + `make routes`.

### `admin.media.create` — `POST /api/v1/admin/media`

Replaces multipart body.

Request (JSON):

```json
{
  "original_filename": "hero.png",
  "content_type": "image/png",
  "size_bytes": 1048576,
  "tags": ["cover"]
}
```

Response `201` envelope `data`:

```json
{
  "media_id": "uuid",
  "storage_key": "media/{uuid}/hero.png",
  "upload_url": "https://…",
  "required_headers": { "Content-Type": "image/png" },
  "expires_at": "RFC3339",
  "disk": "minio"
}
```

Auth: bearer + permission `media.create`.

### `admin.media.complete` — `POST /api/v1/admin/media/{id}/complete`

No body required (MVP). Response `200` `EnvelopeMediaResponse` with full `MediaAsset` (`status=ready`).

Idempotent: if already `ready` and object still present → `200` same asset.

## Object storage port

```go
type ObjectStorage interface {
    PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, headers map[string]string, err error)
    HeadObject(ctx context.Context, key string) (size int64, contentType string, etag string, err error)
}
```

Adapter uses AWS SDK v2 against `S3_ENDPOINT` with path-style addressing when `S3_FORCE_PATH_STYLE=true` (MinIO default). `disk` is taken from `S3_DISK` (default `minio`).

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `S3_ENDPOINT` | from `.env.example` | API endpoint |
| `S3_REGION` | `auto` | Region / signing region |
| `S3_BUCKET` | `blog-media` | Bucket name |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | — | Credentials |
| `S3_PUBLIC_BASE_URL` | — | Reserved for future public URLs; unused in MVP response |
| `S3_DISK` | `minio` | Persisted `disk` enum value |
| `S3_FORCE_PATH_STYLE` | `true` | MinIO-compatible addressing |
| `MEDIA_ALLOWED_MIME_TYPES` | `image/jpeg,image/png,image/webp,image/gif` | Allowlist |
| `MEDIA_MAX_UPLOAD_BYTES` | `10485760` | Max declared size |
| `MEDIA_PRESIGN_TTL` | `15m` | Presign expiry |

Document in [config.md](../../deployment/config.md) and `.env.example`.

## Validation and errors

Presign rejects (400 `validation_error`):

- empty / path-traversal filename
- content_type not in allowlist
- size_bytes < 1 or > `MEDIA_MAX_UPLOAD_BYTES`

Complete:

| Situation | Status | `error.code` |
| --- | --- | --- |
| Unknown / deleted id | 404 | `not_found` |
| Object missing in bucket | 409 | `media.upload_incomplete` |
| `presign_expires_at` passed and still pending | 409 | `media.upload_expired` |
| Head reports content-type outside allowlist | 400 | `validation_error` |
| Head size > max | 400 | `validation_error` |
| Storage SDK failure | 502 | `storage_unavailable` |
| Already ready | 200 | idempotent |

## Bootstrap / wiring

- Open storage client when `S3_BUCKET` (and credentials) are configured; fail `serve`/`doctor` clearly if media routes are wired but storage misconfigured.
- Inject media service into HTTP deps; map `admin.media.create` and `admin.media.complete` in `handlerFor`.
- Guard with Casbin `media.create` (already seeded for admin/editor/author).

## Testing

- Domain/service tests with fake `ObjectStorage` and fake repository (no Gin/GORM/AWS).
- HTTP table tests: presign 201 shape; validation 400; complete happy path; incomplete → 409; unauthorized / forbidden.
- No live MinIO required in CI.

## Success criteria

1. committed OpenAPI bundles under `contracts/` + `make routes` succeed after OpenAPI updates.
2. Against local MinIO: client can presign → PUT → complete and receive `status=ready` asset.
3. Core remains free of AWS SDK / Gin / GORM imports.
4. Docs: config guide, media backend notes, phase-2 backlog boxes for create/presign path updated.
5. `make test` / `make lint` green.

## Follow-ups (out of scope)

List/delete/tags, public get + transform, malware scan, outbox event, post/avatar relations — tracked in [phase-2-content-core.md](../../tasks/phase-2-content-core.md).
