# Secrets and Security Headers

## Environment secrets

Source of truth for local keys: [.env.example](../../.env.example).

| Variable | Rules |
| --- | --- |
| `APP_SESSION_KEY` | ≥ 32 random bytes; rotate on compromise |
| `APP_CSRF_KEY` | ≥ 32 random bytes; distinct from session key |
| `APP_PEPPER` | server-side password pepper; never commit real value |
| `DATABASE_URL` | least privilege DB role in staging/prod |
| `REDIS_URL` | network-restricted; AUTH in non-local envs |
| `S3_*` | scoped bucket credentials; no admin cloud keys in the API |

Never commit `.env`. Distroless runtime image must not bake secrets into layers
([Dockerfile](../../Dockerfile)).

## Password and crypto

- Argon2id or bcrypt for password hashes
- encrypt TOTP secrets at rest; hash backup codes
- encrypt `contact_phone`; logs may carry HMAC only

## Response headers (baseline)

Applied globally today:

- `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`
- `Referrer-Policy: no-referrer`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-Request-ID` (echo or generate UUID)

Additional (when feature lands):

- CORS allowlist (never `*`)
- `X-Robots-Tag: noindex` when user privacy disables indexing
- rate-limit headers + `Retry-After` on 429

## Logging redaction

Never log:

- passwords, refresh/access tokens, CSRF tokens, `jti`
- TOTP secrets, backup codes, OAuth client secrets
- decrypted phone, full card-like PII
- raw Authorization headers

Access logs may include `request_id`, `operation_id`, `route_group`, `auth_mode`, status, latency.
