# Phase 1 — Foundation

## Goal

Stand up the runtime skeleton: configuration, database and cache connectivity, health
endpoints, the auth foundation, and the user / role / permission model.
Index: [README.md](./README.md).

## Status

**Partial** — bootstrap, config, PostgreSQL persistence (bigint row ids plus public UUIDs),
Redis, health, password-based auth with refresh rotation, and Casbin authorization are in
place, along with the SMTP mailer, login lockout, and a messaging readiness check. Admin user
management, second-factor and OAuth login, and session administration are still open.

## Epic: project bootstrap and configuration

- [x] Hexagonal layout under `internal/core`, `internal/adapters`, `internal/platform`
- [x] `cmd` Cobra CLI (`main.go` at repo root → `cmd.Execute()`; subcommands under `cmd/`)
- [x] `internal/platform/config` loads and validates environment configuration
- [x] `.env.example` documents every supported variable
- [x] `Makefile` targets for build, test, lint, swagger, routes-check, infra, migrate, run
- [x] Dockerfile and `compose.yaml` for local infrastructure
- [x] GitHub Actions workflow at `.github/workflows/go.yml`
- [x] Request correlation IDs (`middleware.RequestID`, `X-Request-ID`) logged by `middleware.AccessLog`
- [x] Propagate the request ID into `context.Context` so core services and outbound adapters log it
      (`logging.WithRequestID`; the logger adds `request_id` to every `*Context` call)
- [x] Graceful-shutdown timeout driven by `APP_SHUTDOWN_TIMEOUT` (`cmd/serve.go`)

## Epic: persistence and cache

- [x] Database open in `internal/platform/database` (PostgreSQL; MySQL and SQL Server dropped)
- [x] Google Cloud SQL connectivity via `cloud.google.com/go/cloudsqlconn` with IAM auth and private IP
- [x] Connection pool tuning from config (`DB_MAX_OPEN`, `DB_MAX_IDLE`, lifetimes)
- [x] Goose SQL migrations `00001`–`00009` under `internal/platform/migrations/sql`
- [x] Every entity table keys on `id bigint` identity with a unique public `uuid`; foreign keys are bigint
- [x] Redis client in `internal/platform/redis`
- [x] Seeder in `internal/platform/seed` for baseline roles
- [x] Port the migrations to MySQL and SQL Server (they use PostgreSQL identity columns,
      `gen_random_uuid()`, and partial indexes) or drop those drivers from `internal/platform/database`
      (dropped: PostgreSQL only)
- [x] `app migrate down` rollback path, documented in [database.md](../backend/database.md)
- [x] Seeder covers permissions and Casbin policy rows, not just roles

## Epic: health and diagnostics

- [x] `health.live` — `GET /health/live`
- [x] `health.ready` — `GET /health/ready` with database and Redis checkers
- [x] `health.version` — `GET /health/version`
- [x] `app doctor` reports database driver, Redis, and messaging reachability
- [x] Add a messaging readiness checker to `health.ready` when `MESSAGE_BROKER` is set
      (`messaging.Probe`, TCP reachability)
- [x] Expose Prometheus-style metrics or document the deliberate decision not to
      (`METRICS_ADDR`, `internal/platform/metrics`)

## Epic: auth foundation

- [x] `auth.login` — `POST /api/v1/auth/login`
- [x] `auth.refresh` — `POST /api/v1/auth/refresh` backed by `refresh_sessions`
- [x] `auth.logout` — `POST /api/v1/auth/logout`
- [x] `auth.password.forgot` — `POST /api/v1/auth/password/forgot`
- [x] `auth.password.reset_token_validity` — `GET /api/v1/auth/password/reset/{token}`
- [x] `auth.password.reset` — `POST /api/v1/auth/password/reset`
- [x] `me.password.update` — `PUT /api/v1/me/password`
- [x] Argon2id password hashing in `internal/platform/security/password`
- [x] JWT issue and verify in `internal/platform/security/jwt`
- [x] Bearer auth middleware in `internal/adapters/inbound/http/middleware`
- [x] Transport request validation with `github.com/go-playground/validator/v10` (Laravel-style field errors via `bindJSON`)
- [ ] `admin.auth.login` — `POST /api/v1/admin/auth/login`
- [ ] `auth.2fa.challenge` — `POST /api/v1/auth/2fa/challenge`
- [ ] `auth.oauth.callback` — `POST /api/v1/auth/oauth/{provider}/callback`
- [x] Replace the reset-token log stub with a real mailer per [email.md](../backend/email.md)
      (`mail.SMTP` behind `notification/ports.Mailer`; log stub only when `SMTP_HOST` is empty)
- [x] Refresh-token rotation with reuse detection revoking the whole session family
- [x] Add login throttling and lockout on repeated failures (`AUTH_LOGIN_*`; per-IP limit plus
      per-email Redis lockout, `429` with `Retry-After`)

Spec: [authentication.md](../features/authentication.md)

## Epic: user, role, and permission model

- [x] `users`, `roles`, `permissions`, `user_roles`, `role_permissions` tables
- [x] `casbin_rules` table and enforcer in `internal/adapters/outbound/rbac`
- [x] `requirePermission` middleware wired to the Casbin enforcer
- [x] `me.get` — `GET /api/v1/me`
- [x] `admin.users.list` — `GET /api/v1/admin/users`
- [x] `admin.users.create` — `POST /api/v1/admin/users` (`user.create`; roles also need `role.manage`)
- [x] `admin.users.password.admin_reset` — `POST /api/v1/admin/users/{id}/password/admin-reset`
- [ ] `admin.users.activity.list` — `GET /api/v1/admin/users/{id}/activity`
- [x] Role and permission administration endpoints, or a documented decision to seed-only
      (`/api/v1/admin/roles`, `/admin/permissions`, `/admin/users/{id}/roles`; `admin` is protected)
- [ ] Policy reload without restart when `casbin_rules` changes

Specs: [user-management.md](../features/user-management.md),
[rbac-with-casbin.md](../features/rbac-with-casbin.md)

## Dependencies and order

1. Config and database open must land before migrations and seeding — done.
2. JWT and password hashing gate every auth handler — done.
3. Casbin enforcer gates admin route wiring — done.
4. The real mailer blocks completing password reset and email-change flows — done.
5. Refresh-token rotation should land before second-factor and OAuth login — done.

## Cross-cutting

- [x] Unit tests for JWT, config messaging validation, and database driver normalization
- [x] Route smoke tests (`make routes-check`) for `routes.Register*` auth ordering, route metadata, and 501 stubs
- [ ] Test that every mounted Gin route has a matching operation in `docs/swagger.json` and vice versa
- [ ] Integration tests for the full login / refresh / logout cycle against a real database
- [ ] Negative-path tests for expired and reused refresh tokens
- [ ] Document the auth threat model in [authn-authz.md](../security/authn-authz.md)
- [ ] Run the security checklist against the auth surface — [checklist.md](../security/checklist.md)

## References

| Topic | Doc |
| --- | --- |
| Architecture | [architecture.md](../architecture/architecture.md) |
| Tech stack | [tech-stack.md](../architecture/tech-stack.md) |
| Database | [database.md](../backend/database.md) |
| Data models | [model.md](../backend/model.md) |
| ERD | [ERD.md](../backend/ERD.md) |
| RBAC | [rbac-casbin.md](../backend/rbac-casbin.md) |
| Error envelope | [error-handling.md](../backend/error-handling.md) |
| Local setup | [local.md](../deployment/local.md) |
