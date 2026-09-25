# Error Handling

## Goals

- keep errors predictable for clients
- avoid leaking sensitive internals
- preserve enough detail in logs for operators

## Error Mapping

- validation problems -> `400`
- authentication failures -> `401`
- authorization failures -> `403`
- missing resources -> `404`
- privacy-gated resources (e.g. private public profile) -> `404` (not 403; avoids leaking the target exists)
- conflict conditions -> `409`
- corrupted uploads or invalid transforms -> `422`
- unprocessable domain logic (password strength, history reuse, token already used) -> `422`
- rate limiting -> `429` with `Retry-After` header in seconds and `code=rate_limit.exceeded`
- overload (`HTTP_MAX_INFLIGHT` reached) -> `503` with `Retry-After: 1` and `code=server.overloaded`
- unexpected failures -> `500`

## Stable Error Code Catalogue (profile area)

Client-safe codes only; never include usernames, emails, hashes, IPs, or internal DB errors in `error.message`:

| Code                                           | HTTP | Meaning                                                                 |
|------------------------------------------------|------|-------------------------------------------------------------------------|
| `profile.unknown_field`                        | 400  | Profile/Privacy patch contained an unknown or non-allowlisted key      |
| `profile.invalid_type`                         | 400  | Expected type mismatch for profile flag (e.g. string where bool needed)|
| `profile.display_name.taken`                   | 409  | display_name unique constraint violation; suggest alternatives         |
| `profile.bio.too_long`                         | 422  | bio exceeds 4000 chars after sanitization                               |
| `profile.contact.phone.invalid`                | 422  | E.164 format or region validation fail                                  |
| `profile.contact.phone.unverified`             | 422  | attempted to set visibility contact_details true without phone OTP OK  |
| `profile.contact.website.invalid`              | 422  | URL scheme or length invalid                                            |
| `profile.social.twitter.invalid`               | 422  | Twitter handle format mismatch                                          |
| `profile.locale.not_supported`                 | 422  | locale not in app allowlist                                             |
| `profile.timezone.invalid`                     | 422  | IANA tzdb lookup fails                                                  |
| `profile.avatar.too_large`                     | 413  | image > 5 MB                                                            |
| `profile.avatar.invalid_mime`                  | 422  | MIME or magic bytes not in image whitelist                              |
| `profile.avatar.malware_detected`              | 422  | Malware scan failed; no media persisted                                 |
| `profile.avatar.dimensions_exceeded`           | 422  | 8192×8192 cap hit                                                       |
| `password.strength`                            | 422  | New password fails policy; details array of `{rule, hint}`              |
| `password.history_conflict`                    | 422  | New password matches one of last N historical hashes                    |
| `password.confirm_mismatch`                    | 422  | `confirm_password` != `new_password`                                    |
| `password.current_mismatch`                    | 403  | update `current_password` failed verify                                |
| `auth.password.reset_token_invalid`            | 400  | token signature / aud / nbf fails validation                            |
| `auth.password.reset_token_expired`            | 400  | expires_at passed                                                       |
| `auth.password.reset_token_used`               | 400  | token jti already consumed (single-use)                                 |
| `email.change.proof_required`                  | 403  | email change missing password_proof or recent 2FA                       |
| `email.change.token_invalid`                   | 400  | email-change token invalid signature/jti/expiry                         |
| `email.change.email_mismatch`                  | 400  | token's email_address_hash != expected user email HMAC                  |
| `privacy.direction_requires_proof`             | 403  | visibility reduced (public→private) missing password/2FA proof         |
| `privacy.unknown_flag`                         | 400  | allowlisted flag unknown                                                |
| `activity.not_found`                           | 404  | activity id lookup nonexistent (for detail expanders)                   |
| `activity.export.rate_limited`                 | 429  | export throttle 1/hour                                                  |
| `activity.erase.proof_required`                | 403  | erase missing password proof/2FA                                        |
| `public_user.not_found_or_private`             | 404  | username/UUID private or nonexistent; avoid leaking existence via 403  |
| `encryption.decrypt_failed`                    | 500  | contact.phone envelope decryption failed; operator must check KMS       |
| `encryption.key_unavailable`                   | 500  | KMS/ENV master key not accessible at decrypt time                       |
| `rate_limit.exceeded`                          | 429  | with Retry-After header seconds                                         |
| `server.overloaded`                            | 503  | too many requests in flight on this replica; retry after Retry-After    |
| `settings.version_conflict`                    | 409  | a setting changed since the submitted `version`; reload and retry       |
| `csrf.missing` / `csrf.invalid`                | 403  | browser-origin state-changing requests without/invalid CSRF             |
| `cors.origin_not_allowed`                      | 403  | origin not in whitelist for profile mutations                           |
| `stepup.required`                              | 403  | missing `X-Re-Verify-Password` or recent 2FA for high-risk action       |
| `twofa.required`                               | 403  | policy requires 2FA but user has none enabled                           |
| `consent.marketing.record_failed`              | 500  | marketing_consent=true but ConsentService event not recorded; rollback  |

## Response Rules

- always include a stable machine-readable error code
- include a human-readable message safe for clients
- attach request ID in metadata
- keep stack traces out of API responses
- profile endpoints with `details[]` return per-field `{ field, code, message, hint? }` structure; validation.md style

## Logging Rules

- log unexpected failures at error level
- avoid logging secrets or full sensitive payloads
- correlate logs with request ID and actor when available
- NEVER log `current_password`, `new_password`, `confirm_password`, reset/email-change tokens, raw JWT bodies, encrypted contact cleartext, or reset-token URLs
- HMAC (peppered) hash or truncated SHA-256 allowed for password_reset_tokens.email_address_hash logging when authorized operator debug stream needs correlation
- profile mutation audit logs: `before_value_hash` / `after_value_hash` (never before_value / after_value) for sensitive fields; plain before/after permitted for public-safe display fields (full_name, display_name, bio, locale, timezone, website non-sensitive) with data minimization
