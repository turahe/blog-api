# Tech Stack

## Backend

- Language: Go
- HTTP framework: Gin
- ORM: GORM
- Database: PostgreSQL
- Cache: Redis
- Event bus: Watermill
- Object storage: Cloudflare R2 and S3-compatible providers
- Image transformation: high-performance image processing engine with WebP and AVIF output support

## Local Development

- local environment orchestration: Docker Compose
- local object storage for media testing: MinIO or another S3-compatible service

## Authentication and Security

- password hashing: Argon2id or bcrypt
- session strategy: secure server-managed sessions or secure http-only cookies
- 2FA: TOTP
- social auth: OAuth 2.0 / OpenID Connect

## Data and Async

- PostgreSQL stores users, roles, permissions, posts, comments, audit logs, and outbox data
- Redis stores cache entries, rate-limit counters, and short-lived session/security state
- object storage stores original media files and optionally hot transformed variants
- Watermill publishes domain events such as post, auth, and notification events

## Recommended Supporting Libraries

- UUID generation library
- structured logging with `log/slog`
- validation package for request validation
- OpenTelemetry-compatible instrumentation
- testing with Go `testing` + `testify`

## Frontend and Admin Considerations

If an admin UI is added later, prefer:

- React + TypeScript
- Vite
- Tailwind CSS

## Versioning Policy

- pin major versions for critical libraries
- upgrade infrastructure libraries intentionally, not opportunistically
- document breaking changes before adoption
