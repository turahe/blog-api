# Upload Security Review

Scope: every path that puts bytes into object storage — the admin presigned upload
(`admin.media.create` then `admin.media.complete`) and the self-service avatar upload
(`me.avatar.upload`). Reviewed for Phase 2 against three threats: content that lies about
its type, filenames that escape their key, and storage or quota abuse.
Related: [media.md](media.md), [user-profile.md](user-profile.md), [validation.md](validation.md).

## Upload paths

| Path | Who | How bytes arrive | Where checks run |
| --- | --- | --- | --- |
| Presigned | staff with `media.create` (author and up) | client `PUT`s straight to the bucket with the signed `Content-Type` | `PresignUpload` before signing, `CompleteUpload` on the stored object |
| Avatar | any authenticated user | multipart `file` field through the API | `readAvatar` in the handler, then `media.Service.UploadImage` |

Only `ready` assets are served (`public.media.get` uses `GetReady`), so an upload that fails
any check below is never exposed.

## Content type (MIME sniffing)

The declared type is never trusted on its own.

- **Avatar:** the type comes from the bytes (`http.DetectContentType`), must be one of JPEG,
  PNG, GIF or WebP, and must also be in `MEDIA_ALLOWED_MIME_TYPES`. The filename and the
  multipart part header are ignored. The storage key's extension is rewritten to the
  sniffed type, so `me.html` holding a PNG is stored as `media/{uuid}/me.png` with
  `Content-Type: image/png`.
- **Avatar dimensions:** `DecodeConfig` reads only the image header — pixels are never
  decoded, so decompression bombs cost nothing. Each side must be 1–8192 px
  (`MaxImageDimension`). The width, height and SHA-256 are recorded.
- **Presigned:** the declared type must be allowlisted before signing, and the signature
  binds the `Content-Type` header, so the stored object carries the declared type.
  `CompleteUpload` then reads the first 512 bytes with a ranged `GET` and sniffs them.
  JPEG, PNG, GIF, WebP and PDF must sniff as exactly the declared type. Any other allowlisted
  type is rejected if it sniffs as HTML or XML. A mismatch returns `400` and leaves the asset
  `pending`.
- SVG is not in the default allowlist. Enabling it would allow script in served objects,
  so it must not be added without sanitisation and a sandboxing `Content-Security-Policy` on
  the media origin.

## Filenames and storage keys (path traversal)

- Keys are `media/{asset uuid}/{name}`. The UUID is generated server-side, so no filename
  can reach another asset's key or leave the `media/` prefix.
- `sanitizeFilename` rejects empty names, names over 255 bytes, any `/` or `\`, control
  characters (including NUL), `.` and `..`, and anything `filepath.Base` would change.
- For multipart uploads, Go's `multipart.FileHeader.Filename` is already reduced to its
  base name, so `../../x.png` arrives as `x.png` (verified live). The service check still
  runs for other callers.
- The original filename is stored only as metadata and never used as a path on disk; the
  multipart form's temporary files are removed after each request.

## Quota and abuse

| Control | Presigned | Avatar |
| --- | --- | --- |
| Per-object size cap | `MEDIA_MAX_UPLOAD_BYTES` (10 MiB), checked at presign and again from `HeadObject` | `AVATAR_MAX_BYTES` (5 MiB) and the media cap, whichever is smaller; body capped by `MaxBytesReader`, `413 profile.avatar_too_large` |
| Rate limit (per user) | `media.presign` 60/min | `profile.avatar` 10/min |
| Upload window | presigned URL expires after `MEDIA_PRESIGN_TTL` (15m) | single request |
| Growth | one row per presign | a replaced or deleted avatar is soft-deleted when it was uploaded by the same user and tagged `avatar` |

Email change is rate limited separately (`email.change.request` 3/hour,
`email.change.confirm` 10/min); it stores no objects.

## Residual risks and deferred work

- **Re-upload after complete:** a presigned URL stays valid until it expires, so a staff
  uploader can overwrite a completed object with different bytes of the same declared type
  during that window. The sniff runs only at completion. Mitigations: copy to a final key on
  completion, or keep `MEDIA_PRESIGN_TTL` short.
- **Orphans:** pending assets whose presign expired, and soft-deleted assets, keep their
  objects. There is no sweeper job yet (Phase 4 worker).
- **No per-user storage quota.** Rate limits bound the rate, not the total. Needs a quota
  table or bucket-side limits.
- **No malware scanning.** Image types are validated structurally; other allowlisted types
  (PDF) are not scanned.
- **Presigned dimensions are not checked:** JPEG headers can sit beyond the 512-byte
  prefix. Dimensions are enforced on the avatar path only.
- **Transforms run in imgproxy, not the API.** The API only validates `w` against
  `MEDIA_TRANSFORM_WIDTHS` and `format` against webp/avif/jpeg/png, then redirects to a URL
  signed with `IMGPROXY_KEY`/`IMGPROXY_SALT`, so clients cannot request other sizes or
  sources. imgproxy caps decoding with `IMGPROXY_MAX_SRC_RESOLUTION` and
  `IMGPROXY_MAX_SRC_FILE_SIZE`, never enlarges, and accepts only `s3://` sources. SVG is
  not transformed.
- **CSRF** does not apply: all upload routes take bearer tokens, not cookies.
- Serve the bucket from a separate origin with `X-Content-Type-Options: nosniff` set at the
  CDN or bucket so browsers never second-guess the stored type.
