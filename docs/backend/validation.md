# Validation

## Validation Layers

- **transport validation** for request shape — Gin `binding` tags + `github.com/go-playground/validator/v10` via `bindJSON` (Laravel-style field error bag)
- domain validation for business rules
- persistence constraints for data integrity

## Transport validation (Laravel-style)

Handlers call `bindJSON(c, &req)` instead of raw `ShouldBindJSON`. Failures return HTTP `400` with:

```json
{
  "ok": false,
  "meta": { "request_id": "..." },
  "error": {
    "code": "validation_error",
    "message": "The given data was invalid.",
    "details": {
      "email": ["The email field is required."],
      "password": ["The password field is required."]
    }
  }
}
```

`details` is a map of JSON field name → message list (same shape as Laravel's `errors` bag).
Malformed JSON uses the `_form` key. Struct tags use Gin `binding:"required,email,min=12,..."`;
JSON names come from `json` tags registered on the shared validator engine.

## Rules

- validate all external input server-side
- use allowlists for mutable fields
- keep business invariants inside the domain layer
- map validation failures to consistent client-safe errors

## Typical Validations

- required fields
- slug uniqueness
- email format
- role and permission key format
- comment status transitions
- published post state transitions
- media file type and max size
- image resize dimensions, quality, crop mode, and rotation values
- tag limits and allowed metadata fields for media assets
- user avatar: valid media_asset_id existence and image content-type whitelist
- category image: valid media_asset_id existence and image content-type whitelist
- post attachments: valid media_asset_id existence, kind allowlist, sort_order range, cover consistency with post_media join table
- post cover_image_media_id: either null or matches an existing kind=cover post_media entry when enforce_cover_sync is enabled
- profile PATCH `/me/profile` allowlist: `full_name, display_name, bio, contact_phone, contact_website, contact_location, social_links{twitter, linkedin, github}, locale, timezone, marketing_consent`; unknown keys rejected with `code=profile.unknown_field`
- privacy PUT `/me/privacy` allowlist: `visibility_profile, visibility_email, visibility_contact_details, visibility_activity_timeline, search_allow_indexing, tracking_personalize_ads`; enums and booleans strict typed; no type coercion
- profile field validators:
  - `full_name` / `display_name` / `contact_location`: no control chars (`\p{Cc}`), 1–N length
  - `display_name`: partial unique index constraint per DB; case-insensitive duplicate check before insert/update
  - `bio` markdown sanitized: tags allow {a,strong,em,code,pre,blockquote,p,h3-h6,ul,ol,li}; strip `<script,iframe,form,input,on*>`; all `href` must be http/https; add `rel=nofollow noreferrer noopener`; length ≤ 4000 after sanitization
  - `contact_phone`: E.164 format + libphonenumber-style region valid; when `visibility_contact_details` toggled to true for phone, enforce SMS OTP verified (contact_phone_verified_at not null)
  - `contact_website`: RFC 3986 with scheme http|https; length ≤ 2048; reject data:/file: schemes; allow IDNA punycode domains only
  - `social_links.twitter`: `^@?[A-Za-z0-9_]{1,15}$` normalized to `@handle` unless full twitter URL
  - `social_links.linkedin`: profile slug matching `^[A-Za-z0-9\-]{5,}$` OR full https://www.linkedin.com/in/{slug} URL
  - `social_links.github`: `^[A-Za-z0-9][A-Za-z0-9\-]{0,38}$` OR https://github.com/{slug}
  - `locale`: BCP 47 in application allowlist (default: en_US, en_GB, zh_Hans_CN, id_ID, vi_VN); fall back to en_US if unknown
  - `timezone`: IANA tzdb entry (e.g. `America/New_York`, `Asia/Jakarta`); reject abbreviations like `EST`/`GMT`
  - `marketing_consent`: strict boolean; when toggled true must also record a consent event through ConsentService with UTC timestamp and request_id
- password validators:
  - update/reset min length 12; max 128; reject control chars
  - reject top 100k common passwords via bloom/hash-lookup (NIST SP 800-63B guidance)
  - confirm password must match exactly
  - history N=10 reuse check against `user_password_history` using constant-time verifier; equal to current password also rejected
- email validators:
  - RFC 5322 section 3.4.1 grammar + TLD existence check against known TLD list + MX DNS lookups optional
  - no change without verification flow token (jti) + old address warning sent
- reset/email tokens:
  - JWT RS256 signature valid; aud claim matches `blog:password_reset` or `blog:email_change`
  - jti not consumed via Redis SETNX and not in password_reset_tokens.consumed_at
  - expires_at not passed; nbf if present respected
  - email_hash salted HMAC in payload matches the stored hash or user's current email HMAC (for email change, matches old+new pair)
- avatar upload:
  - Content-Length ≤ 5 MB; multipart/form-data form field `file`
  - MIME type in {image/jpeg, image/png, image/webp, image/gif, image/avif} AND file magic bytes match (e.g. JPEG SOI, PNG 89 50 4E 47)
  - image dimensions ≤ 8192×8192 to prevent decompression bomb; image aspect ratio any but crop tool expects roughly square input, warn if not 1:1
  - malware scan pass (ClamAV or provider policy); failure → 422 profile.avatar.malware_detected, nothing persisted
- CSRF verification: browser clients must present `X-CSRF-Token` header matching server issued cookie (double-submit) OR form token per framework; non-browser exempt per admin security policy
- rate limits: strict per `docs/backend/user-profile.md` tier table; Retry-After header returned on 429
- public `/users/:name`: input `username_or_id` either UUID v4 regex or display_name/username regex; path traversal attempts rejected before routing via Gin router parameter whitelist
