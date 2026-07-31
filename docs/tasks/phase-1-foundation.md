# Phase 1 — Foundation

## Goal

Stand up the runtime skeleton: configuration, database and cache connectivity, health
endpoints, the auth foundation, and the user / role / permission model.
Index: [README.md](./README.md).

## Status

**Partial** — bootstrap, config, multi-dialect persistence, Redis, health, password-based
auth, and Casbin authorization are in place. Admin user management, second-factor and OAuth
login, session administration, and the real mailer are still open.

## Epic: project bootstrap and configuration

- [x] Hexagonal layout under `internal/core`, `internal/adapters`, `internal/platform`
- [x] `cmd` Cobra CLI with `serve`, `migrate`, `seed`, `doctor`, `worker`
- [x] `internal/platform/config` loads and validates environment configuration
- [x] `.env.example` documents every supported variable
- [x] `Makefile` targets for build, test, lint, contracts, routes, infra, migrate, run
- [x] Dockerfile and `compose.yaml` for local infrastructure
- [x] GitHub Actions workflow at `.github/workflows/go.yml`
- [ ] Add structured request logging with correlation IDs propagated into the core layer
- [ ] Add graceful-shutdown timeouts driven by config rather than hard-coded values

## Epic: persistence and cache

- [x] Multi-dialect database open in `internal/platform/database` (`postgres`, `mysql`, `sqlserver`)
- [x] Google Cloud SQL connectivity via `cloud.google.com/go/cloudsqlconn` with IAM auth and private IP
- [x] Connection pool tuning from config (`DB_MAX_OPEN`, `DB_MAX_IDLE`, lifetimes)
- [x] Goose-style SQL migrations `00001`–`00004` under `internal/platform/migrations/sql`
- [x] Redis client in `internal/platform/redis`
- [x] Seeder in `internal/platform/seed` for baseline roles
- [ ] Verify every migration applies cleanly on MySQL and SQL Server, not only PostgreSQL
- [ ] Add a `migrate-down` / rollback path and document it in [database.md](../backend/database.md)
- [ ] Extend the seeder to cover permissions and Casbin policy rows, not just roles

## Epic: health and diagnostics

- [x] `health.live` — `GET /health/live`
- [x] `health.ready` — `GET /health/ready` with database and Redis checkers
- [x] `health.version` — `GET /health/version`
- [x] `app doctor` reports database driver, Redis, and messaging reachability
- [ ] Add a messaging readiness checker to `health.ready` when `MESSAGE_BROKER` is set
- [ ] Expose Prometheus-style metrics or document the deliberate decision not to

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
- [x] Bearer auth middleware in `internal/adapters/inbound/http/auth_middleware.go`
- [ ] `admin.auth.login` — `POST /api/v1/admin/auth/login`
- [ ] `auth.2fa.challenge` — `POST /api/v1/auth/2fa/challenge`
- [ ] `auth.oauth.callback` — `POST /api/v1/auth/oauth/{provider}/callback`
- [ ] Replace the reset-token log stub with a real mailer per [email.md](../backend/email.md)
- [ ] Enforce refresh-token rotation with reuse detection and session revocation
- [ ] Add login throttling and lockout on repeated failures

Spec: [authentication.md](../features/authentication.md)

## Epic: user, role, and permission model

- [x] `users`, `roles`, `permissions`, `user_roles`, `role_permissions` tables
- [x] `casbin_rules` table and enforcer in `internal/adapters/outbound/rbac`
- [x] `requirePermission` middleware wired to the Casbin enforcer
- [x] `me.get` — `GET /api/v1/me`
- [x] `admin.users.list` — `GET /api/v1/admin/users`
- [ ] `admin.users.create` — `POST /api/v1/admin/users`
- [ ] `admin.users.password.admin_reset` — `POST /api/v1/admin/users/{id}/password/admin-reset`
- [ ] `admin.users.activity.list` — `GET /api/v1/admin/users/{id}/activity`
- [ ] Role and permission administration endpoints, or a documented decision to seed-only
- [ ] Policy reload without restart when `casbin_rules` changes

Specs: [user-management.md](../features/user-management.md),
[rbac-with-casbin.md](../features/rbac-with-casbin.md)

## Dependencies and order

1. Config and database open must land before migrations and seeding — done.
2. JWT and password hashing gate every auth handler — done.
3. Casbin enforcer gates admin route wiring — done.
4. The real mailer blocks completing password reset and email-change flows.
5. Refresh-token rotation should land before second-factor and OAuth login.

## Cross-cutting

- [x] Unit tests for JWT, config messaging validation, and database driver normalization
- [x] Router contract test asserting generated routes match `paths/`
- [ ] Integration tests for the full login / refresh / logout cycle against a real database
- [ ] Negative-path tests for expired and reused refresh tokens
- [ ] Document the auth threat model in [authn-authz.md](../security/authn-authz.md)
- [ ] Run the security checklist against the auth surface — [checklist.md](../security/checklist.md)

## References

| Topic | Doc |
| --- | --- |
| Architecture | [architecture.md](../architecture/architecture.md) |
| Tech stack | [tech-stack.md](../architecture/tech-stack.md) |
| Database and dialects | [database.md](../backend/database.md) |
| Data models | [model.md](../backend/model.md) |
| ERD | [ERD.md](../backend/ERD.md) |
| RBAC | [rbac-casbin.md](../backend/rbac-casbin.md) |
| Error envelope | [error-handling.md](../backend/error-handling.md) |
| Local setup | [local.md](../deployment/local.md) |
