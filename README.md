



# Blog Platform REST API

**Contracts-first, production-ready backend for a multi-role blog platform with
authentication, RBAC, media, analytics, impersonation, and audit tooling.**

![OpenAPI 3.1](https://img.shields.io/badge/OpenAPI-3.1-6BA539?logo=openapiinitiative&logoColor=white)
![AsyncAPI 2.6](https://img.shields.io/badge/AsyncAPI-2.6-4D5E7A?logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCIgc3R5bGU9ImZpbGw6d2hpdGUiPjxwYXRoIGQ9Ik0xMiAyIDIgMTBIMnYxNGgyMFYxMFoiLz48L3N2Zz4=)
![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)
![Node 20.19+](https://img.shields.io/badge/Node-20.19%2B-339933?logo=nodedotjs&logoColor=white)
![PostgreSQL 15+](https://img.shields.io/badge/PostgreSQL-15%2B-336791?logo=postgresql&logoColor=white)
![Redis 7+](https://img.shields.io/badge/Redis-7%2B-FF4438?logo=redis&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-blue.svg)
![Changelog](https://img.shields.io/badge/Changelog-keepachangelog-10B981.svg)

**[Docs](#documentation)** ·
**[Contract Reference](https://redocly.github.io/redoc/?url=https://raw.githubusercontent.com/turahe/blog-api/main/openapi.yaml)** ·
**[Architecture](./docs/architecture/architecture.md)**



---



## Table of Contents

1. [Project Introduction](#1-project-introduction)
2. [Features](#2-features)
3. [Technology Stack](#3-technology-stack)
4. [Environment Setup Guide](#4-environment-setup-guide)
5. [Usage & API Examples](#5-usage-api-examples)
6. [Project Deployment Guide](#6-project-deployment-guide)
7. [Contribution Guidelines](#7-contribution-guidelines)
8. [Issue Feedback Channels](#8-issue-feedback-channels)
9. [License](#9-license)

---



## 1. Project Introduction



### Core Positioning

`blog-api` is a **contracts-first backend API** for a modern blog platform.
Every public HTTP endpoint and every domain event is defined as machine-readable
specifications ([OpenAPI 3.1](./openapi.yaml), [AsyncAPI 2.6](./contracts/asyncapi.yaml))
*before* implementation code is written. This lets teams share a single source
of truth across frontend, backend, QA, and documentation workstreams.

### Development Background

The project started from a clear, non-negotiable observation: most blog CMSs
ship either **admin-focused monoliths with poor API ergonomics** or **headless
engines with weak multi-role governance and no audit trail**. This project was
designed from scratch to solve both pain points:

- **Strong separation of concerns** — Hexagonal (ports-and-adapters) modular
monolith so domain logic never couples to Gin, GORM, Redis, or S3.
- **Role-safe operations** — RBAC, superadmin impersonation with step-up 2FA,
and append-only audit logs so teams can operate the platform safely.
- **Privacy-by-default analytics** — Consent-managed pageview/search/engagement
telemetry with 7/30/90-day historical windows and GDPR/CCPA export + erasure
queues for user activity.
- **Reliable content workflows** — Post revisions with side-by-side compare +
one-click restore, per-post SEO meta, on-the-fly image transforms, and
transactional domain events through an outbox pattern.



### Core Value

1. **Predictable integrations**. Frontends and third-party consumers build
  against a versioned, linted spec, not against implementation drift.
2. **Production operations from day one**. Audit logs, RBAC enforcement
  telemetry, structured slog logs, outbox-based event fanout, health/readiness
   probes, and retention/partitioning helpers are all baked in.
3. **Maintainable codebase**. Every domain module lives under
  `internal/core/<module>/` with a consistent `domain / ports / service`
   split. Teams can add new surfaces without untangling monolith spaghetti —
   and later extract modules into services if traffic demands it.

---



## 2. Features

Below is a structured tour of everything the API ships with. Technical
differentiators are tagged with **★**.

### Authentication & Identity

- Email/password login with Argon2id (or bcrypt) password hashing.
- Secure server-managed sessions + HTTP-only cookies; bearer-token fallback for
non-browser clients.
- **TOTP 2FA** with encrypted-at-rest secrets and hashed backup codes.
- **OAuth 2.0 / OpenID Connect** social login with nonce/state verification
and provider-data minimization.
- Password reset (forgot/confirm) and self-service email change with
out-of-band confirmation.



### Multi-Role Administration ★

- **5 native roles**: `superadmin`, `admin`, `editor`, `author`, `moderator`.
Plus `public_reader` for anonymous content access.
- **RBAC + permission inheritance**: roles own `(object, action, domain)`
triples; enforcement points log every allow/deny decision to
`rbac_enforcement_events`.
- **Superadmin impersonation** with CSRF, step-up 2FA, short-TTL sessions that
*do not* retain superadmin grants, and full impersonator attribution on
every audit and activity row.



### Content Management

- Full post CRUD plus draft / scheduled / published / archived workflow.
- **Post revision history** — per-field changelogs, auto-generated summaries,
optional editor notes, side-by-side compare, and one-click restore (creates
a new revision; history is append-only).
- **Per-post SEO controls** — title/description/keywords, editable slug with
regex+allowlist validation, canonical URL, Open Graph + Twitter card fields,
`noindex`/`nofollow` toggles, and SERP/OG/Twitter live previews.
- Categories, tags, and featured-media associations.



### Media & Image Delivery ★

- Upload originals to **S3-compatible object storage** (Cloudflare R2, AWS S3,
RustFS in local-dev).
- On-the-fly image transformation (resize, crop, format) with WebP/AVIF
output, and TTL/size-bounded caching of hot variants.



### Comments & Moderation

- End-user comments on published posts with rate limits.
- Moderator queue, soft deletes, and redact flows.



### Privacy-Conscious Analytics ★

- Consent-managed ingestion: sessions that decline consent are never tracked.
- Admin dashboard views with **7 / 30 / 90-day** traffic, engagement,
retention, and search analytics.
- End-user export + erasure queues for personal activity records.



### Audit, Activity & Security Telemetry ★

- **Dual append-only corpus**: `user_activity` (engagement/self-service,
GDPR-erasable) and `audit_logs` (admin/security, non-repudiation).
- PostgreSQL PL/pgSQL **UPDATE/DELETE denial triggers** for both tables.
- **RLS (Row-Level Security)** scaffold with self-vs-admin policies.
- **PII hygiene** at capture time: IPv4 `/24` + IPv6 `/64` truncation,
user-agent 64-byte bucketing, recursive JSONB redaction of
`password/token/secret/otp/...`, and optional BYTEA peppered-HMAC lookups.
- **Transactional outbox pattern**: every user-facing write can fan out a
domain event atomically inside the same DB transaction.
- **Retention + partitioning**: default 395-day retention job;
`rbac_enforcement_events` is RANGE-partitioned by month.
- **Fail-open capture**: `ActivityLogger.Capture(...)` never returns an error
or blocks a request — failures route to a structured `slog` fallback sink,
and an inflight semaphore drops captures when saturated (default 4096).



### Public & Internal Health

- Public health endpoint (`/api/v1/health`) and internal readiness with
dependency checks reachable via `app doctor`.

---



## 3. Technology Stack

Every version below is the **minimum supported version**. Pin exact versions
in `package-lock.json`, `go.sum`, and container images.

### Backend Runtime


| Component  | Version       | Where it's used                                                   |
| ---------- | ------------- | ----------------------------------------------------------------- |
| Go         | 1.26+         | Main API, workers, scheduler, migrations. See [go.mod](./go.mod). |
| Gin        | 1.10.x        | HTTP router + middleware stack.                                   |
| GORM       | 1.25+ / 1.30+ | ORM; PostgreSQL dialect in prod, SQLite dialect in unit tests.    |
| `log/slog` | stdlib        | Structured logger; replaceable handler for OTel sinks.            |




### Data & Storage


| Component                    | Version              | Purpose                                                                                                 |
| ---------------------------- | -------------------- | ------------------------------------------------------------------------------------------------------- |
| PostgreSQL                   | 15+ (16 recommended) | Relational truth for users, content, audit logs, outbox. See [database.md](./docs/backend/database.md). |
| Redis                        | 7+                   | Cache, rate limit counters, short-lived session/security state.                                         |
| S3-compatible object storage | R2 / S3 / MinIO      | Original media + hot transformed variants.                                                              |




### Messaging & Events


| Component | Version       | Purpose                                                                               |
| --------- | ------------- | ------------------------------------------------------------------------------------- |
| Watermill | latest stable | Domain event pub/sub; pluggable transports (Redis Streams / Kafka / NATS).            |
| AsyncAPI  | 2.6           | Event contract source of truth: [contracts/asyncapi.yaml](./contracts/asyncapi.yaml). |




### API Contract Toolchain


| Tool        | Version | Purpose                                                                               |
| ----------- | ------- | ------------------------------------------------------------------------------------- |
| OpenAPI     | 3.1.0   | HTTP contract: [openapi.yaml](./openapi.yaml) (split under `paths/` + `components/`). |
| Redocly CLI | 2.41.x  | Linting, bundling, deref, build-docs. See [package.json](./package.json).             |




### Frontend (Recommended Future Stack)

Not part of this repo today, but pinned for planning purposes:


| Component    | Version |
| ------------ | ------- |
| React        | 19+     |
| TypeScript   | 7.x     |
| Vite         | 7.x     |
| Tailwind CSS | 4.x     |




### DevOps & Local


| Tool                          | Version | Purpose                                                          |
| ----------------------------- | ------- | ---------------------------------------------------------------- |
| Docker                        | 29+     | Container build + runtime.                                       |
| Docker Compose                | v5      | Local multi-service orchestration (PostgreSQL / Redis / RustFS). |
| Node.js                       | 26.19+  | Redocly toolchain. See [package.json](./package.json).           |
| GitHub Actions / GitHub Pages | latest  | CI/CD pipeline (lint → test → migrate → deploy).                 |




### Testing


| Component                               | Version |
| --------------------------------------- | ------- |
| Go `testing`                            | stdlib  |
| testify (`assert` / `require` / `mock`) | 1.9+    |
| gorm.io/driver/sqlite                   | 1.6+    |


---



## 4. Environment Setup Guide

This section walks you from a fresh clone to a running local environment.

### 4.1 Prerequisites

Install the following on your host machine:


| Tool           | Min version               | Install check            |
| -------------- | ------------------------- | ------------------------ |
| Go             | 1.26                      | `go version`             |
| Node.js        | 26                        | `node -v`                |
| npm            | 11.x (ships with Node 26) | `npm -v`                 |
| Docker         | 24                        | `docker version`         |
| Docker Compose | v2                        | `docker compose version` |




### 4.2 Clone the Repository

```bash
git clone git@github.com:turahe/blog-api.git
cd blog-api
```



### 4.3 Install Toolchain Dependencies

```bash
# OpenAPI / AsyncAPI contract tooling (Redocly CLI)
npm install --no-audit --no-fund

# Go module dependencies + go.sum
go mod tidy
```

Expected on first run:

- `node_modules/@redocly/cli/` appears.
- `go.sum` is generated or updated.



### 4.4 Configure Environment Variables

Copy the template and edit the values:

```bash
cp .env.example .env
```

Required variables (fill these in before starting services):

```dotenv
# ---------- App ----------
APP_ENV=local                        # local | staging | production
APP_ADDR=0.0.0.0:8080                # listen address for `app serve`
APP_SESSION_KEY=replace_me_with_32b_random
APP_CSRF_KEY=replace_me_with_32b_random
APP_PEPPER=replace_me_for_hmac_lookups
APP_JWT_PRIVATE_KEY_PATH=configs/dev/jwt-rsa-private.pem
APP_JWT_PUBLIC_KEY_PATH=configs/dev/jwt-rsa-public.pem

# ---------- PostgreSQL ----------
DB_DRIVER=postgres
DB_HOST=127.0.0.1
DB_PORT=5432
DB_USER=blog
DB_PASSWORD=blog
DB_NAME=blog
DB_SSLMODE=disable

# ---------- Redis ----------
REDIS_DRIVER=redis
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# ---------- Media (S3-compatible) ----------
S3_ENDPOINT=http://127.0.0.1:9000     # RustFS for local; R2/S3 for real envs
S3_REGION=auto
S3_BUCKET=blog-media
S3_ACCESS_KEY=rustfsadmin
S3_SECRET_KEY=rustfsadmin
S3_PUBLIC_BASE_URL=http://127.0.0.1:9000/blog-media

# ---------- OAuth (optional, fill if using social logins) ----------
# OAUTH_GITHUB_CLIENT_ID=...
# OAUTH_GITHUB_CLIENT_SECRET=...
# OAUTH_GOOGLE_CLIENT_ID=...
# OAUTH_GOOGLE_CLIENT_SECRET=...

# ---------- Activity/Audit ----------
ACTIVITY_RETENTION_DAYS=395           # default retention
ACTIVITY_MAX_INFLIGHT=4096
ACTIVITY_TRUNCATE_IP_V4=24
ACTIVITY_TRUNCATE_IP_V6=64
```

> **Security note**: Never commit `.env` to source control. `.env.example` is
> intentionally full of placeholder values. `.gitignore` already excludes
> `.env`, `.env.local`, and `*.local`.



### 4.5 Start Local Infrastructure (PostgreSQL / Redis / RustFS)

```bash
docker compose up -d postgres redis rustfs
docker compose ps        # confirm all three are "healthy"
```



### 4.6 Run Database Migrations & Seeds

```bash
# Load .env and run migrations + seeding
go run ./cmd migrate up
go run ./cmd seed
```



### 4.7 Validate Contracts & Run Tests

```bash
# Contract lint + bundle
npm run contracts:validate

# Go unit + integration tests (hermetic, uses SQLite in memory)
go test -count=1 ./...
```

Expected results:

- Redocly reports contracts valid (or shows actionable `nullable: true`
schema warnings which are tracked separately).
- `go test` exits **0** for all packages.



### 4.8 Start the Local API

```bash
# Runs `app serve` on APP_ADDR (default 0.0.0.0:8080)
go run ./cmd serve
```

Sanity-check the running service:

```bash
curl -sS http://127.0.0.1:8080/api/v1/health | jq .
# {
#   "status": "ok",
#   "version": "0.1.0"
# }
```

---



## 5. Usage & API Examples

The **full reference** is always the [openapi.yaml](./openapi.yaml) spec plus
the split files under [paths/](./paths) and [components/schemas/](./components/schemas).
The examples below illustrate the common call patterns a developer will need.

### 5.1 Authentication (Get a Session)

```http
POST /api/v1/auth/login
Content-Type: application/json
Accept: application/json

{
  "email": "author@example.com",
  "password": "correct horse battery staple"
}
```

**200 OK — response body (envelope format)**:

```json
{
  "ok": true,
  "data": {
    "user": {
      "id": "0192f3c4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
      "username": "ada",
      "email": "author@example.com",
      "display_name": "Ada Lovelace",
      "roles": ["author"],
      "mfa_required": false
    },
    "session": {
      "expires_at": "2026-08-05T12:00:00Z"
    }
  }
}
```



### 5.2 Self-Service — List My Activity

Authenticated users paginate their own activity. `category[]`, `from_date`,
and `to_date` are filter parameters that already exist in the OpenAPI spec.

```bash
curl -sS 'http://127.0.0.1:8080/api/v1/me/activity?page=1&per_page=25&category[]=login&category[]=password_change&from_date=2026-01-01T00:00:00Z' \
  -b cookies.txt | jq .
```

**Typical 200 OK response (abbreviated)**:

```json
{
  "ok": true,
  "data": [
    {
      "id": "019...",
      "activity_type": "login",
      "summary": "Successful password login",
      "created_at": "2026-07-15T08:34:01Z",
      "outcome_status": "success",
      "resource_type": null,
      "resource_id": null,
      "ip_address": "203.0.113.0",
      "user_agent_bucket": "Firefox 128 · Linux x86_64"
    }
  ],
  "meta": { "page": 1, "per_page": 25, "total": 42 }
}
```



### 5.3 Admin — Publish a Draft Post

Requires `post.publish` permission (editor/admin/superadmin).

```http
POST /api/v1/admin/posts/0192f3c4...beef/publish
Content-Type: application/json
If-Match: "3"

{
  "publish_at": null,
  "notify_subscribers": true
}
```

`200 OK` returns the updated post state **and** the system atomically:

- writes a `post_published` row to `user_activity`,
- writes an `admin.post.published` row to `audit_logs`,
- enqueues a `blog.post.published` event in `outbox_events` for Watermill
to fan out (notifications, search index, cache invalidation, analytics
ingest, …).



### 5.4 Admin — Export Audit Logs as CSV

Requires `audit.export` permission, restricted to superadmin by default.

```bash
curl -sS -o audit-export.csv \
  'http://127.0.0.1:8080/api/v1/admin/audit/access?export=csv&from_date=2026-07-01&to_date=2026-08-01' \
  -b cookies.txt
wc -l audit-export.csv
```

The export job itself writes an `audit_logs.exported` self-audit row so the
admin action is non-repudiable (see §2 “Audit, Activity & Security
Telemetry”).

### 5.5 Go Code: Capture a Custom Activity

If you add a new domain action, plug into the `ActivityLogger` facade — it
already implements fail-open capture, truncation, redaction, and outbox
fanout. Example:

```go
package example

import (
  "context"
  "github.com/google/uuid"
  "github.com/turahe/blog-api/internal/activity"
)

func changeUsername(ctx context.Context, al *activity.ActivityLogger, userID uuid.UUID, newUsername string) error {
  // ... domain work inside a DB transaction ...

  // Always fire capture AFTER the transaction commits so the IDs are stable.
  // Capture never returns an error and never panics on bad input.
  al.Capture(ctx, activity.Capture{
    UserID:        userID,
    ActivityType:  activity.TypeUsernameUpdate,
    Summary:       "Username updated to " + newUsername,
    Detail:        map[string]any{"old_username": "ada", "new_username": newUsername},
    ResourceType:  "user",
    ResourceID:    userID.String(),
    OutcomeStatus: activity.OutcomeSuccess,
    AuditAction:   "user.username.changed",
    EventName:     "user.username.changed",
  })
  return nil
}
```

---



## 6. Project Deployment Guide

Deployment model: **single binary (Go) + sidecar services**. The repo ships
best-practice recommendations; plug your cloud of choice into the steps.

### 6.1 Runtime Prerequisites (Server)


| Resource              | Minimum (staging)            | Minimum (production small)                             |
| --------------------- | ---------------------------- | ------------------------------------------------------ |
| API VM / Pod          | 1 vCPU / 1 GB RAM            | 2 vCPU / 4 GB RAM                                      |
| PostgreSQL            | 2 vCPU / 4 GB RAM, 50 GB SSD | 4 vCPU / 8 GB RAM, 250 GB SSD with backups             |
| Redis                 | 0.5 vCPU / 512 MB            | 1 vCPU / 2 GB, persistence enabled                     |
| Object storage bucket | 1 bucket, versioning off     | 1 bucket, object versioning + cross-region replication |




### 6.2 Containerized Deployment (Docker)

`Dockerfile` **reference pattern** (add this to your repo as `Dockerfile`):

```dockerfile
# syntax=docker/dockerfile:1.6
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/app ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /bin/app
EXPOSE 8080
ENTRYPOINT ["/bin/app", "serve"]
```

**Build + push**:

```bash
docker build -t ghcr.io/turahe/blog-api:$(git rev-parse --short HEAD) .
docker push   ghcr.io/turahe/blog-api:$(git rev-parse --short HEAD)
```



### 6.3 Release Process (Staging → Production)

This is the documented release checklist from
[docs/architecture/deployment.md](./docs/architecture/deployment.md):

1. **Build** the tagged container and a `migrate` binary.
2. **Validate contracts** locally or in CI:
  - OpenAPI: `npm run contracts:validate`
  - AsyncAPI: `npx @asyncapi/cli validate contracts/asyncapi.yaml`
3. **Run automated tests**: `go test -count=1 ./...`
4. **Apply DB migrations** *before* enabling new traffic:
  `./app migrate up` (with `DB_HOST` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` set)
5. **Deploy API** (`/bin/app serve`) with a rolling / canary strategy.
6. **Deploy workers and scheduler** if separated: `/bin/app worker`,
  `/bin/app scheduler`. Workers consume the outbox via Watermill.
7. **Validate runtime deps**: `./app doctor`
8. **Post-deploy probes**:
  ```bash
   curl -fsS https://api.example.com/api/v1/health          # liveness
   curl -fsS https://api.example.com/internal/ready         # readiness
  ```
9. Roll back any time. High-risk launches ship behind a feature flag so
  rollback = flag off.



### 6.4 CI/CD Pipeline (Reference)

```yaml
# .github/workflows/ci.yml — abbreviated
name: ci
on: [push, pull_request]
jobs:
  contract-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "20.19" }
      - run: npm ci && npm run contracts:validate
  go-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - run: go mod tidy && go test -count=1 ./...
  build-and-push:
    needs: [contract-lint, go-test]
    if: github.ref == 'refs/heads/main'
    uses: ./.github/workflows/build-deploy.yml
```



### 6.5 Staging vs. Production Differences


| Concern            | Staging                                  | Production                                                         |
| ------------------ | ---------------------------------------- | ------------------------------------------------------------------ |
| DB tier            | General-purpose single-AZ                | Memory-optimized, multi-AZ, point-in-time recovery                 |
| Object storage     | No cross-region replication              | Replication + lifecycle retention + legal holds on audit exports   |
| Session cookie     | `Secure: off` acceptable for `localhost` | `Secure; HttpOnly; SameSite=Lax; Domain=…` + `__Host-` prefix      |
| Activity retention | 30 days                                  | **395 days (default)**; longer via policy + legal holds            |
| Log shipping       | stdout only                              | OTel / Loki / CloudWatch; 90-day hot + 12-month archive            |
| Impersonation      | Enabled for internal QA                  | Superadmin-only, requires 2FA step-up, reviewed by audit quarterly |


---



## 7. Contribution Guidelines

These rules are enforced for **every change** that lands on `main`. The short
version: *scoped changes, tests green, docs in sync, two approving reviews for
security-sensitive files.*

### 7.1 Branch Model

- `main` — deployable, protected, requires CI green + one approving review.
- `feat/xxx` / `fix/xxx` / `docs/xxx` / `chore/xxx` — short-lived feature
branches off `main`, rebased on top of latest `main` before PR.
- No long-lived `develop` or `release/*` branches unless you explicitly adopt
them for a team-wide release cadence.



### 7.2 Opening a PR

1. **Open an issue first** for anything non-trivial (new domain module,
  breaking contracts, architecture changes). Reference it in the PR body.
2. Fork or create a topic branch on this repo; open PR against `main`.
3. PR **title** uses Conventional Commits:
  `feat(posts): side-by-side revision compare UI`,
   `fix(auth): reject blank passwords on set-password endpoint`,
   `docs(readme): add deployment chapter`.
4. PR **body** must contain:
  - **What** changed and **why** (one-paragraph summary).
  - **Tests added**: link to the new tests or explain why existing coverage
  is sufficient.
  - **Risk + rollback**: anything a reviewer needs to check before merging,
  and a one-line rollback plan.
  - **Screenshots / diffs** when changing UI or contract response shapes.
5. Rebase onto latest `main` and squash noisy commits *before* asking for
  review; linear history is enforced.



### 7.3 Code Style & Lint Checks


| Check                                                         | Command                          | Required          |
| ------------------------------------------------------------- | -------------------------------- | ----------------- |
| OpenAPI / event contracts                                     | `npm run contracts:validate`     | Always            |
| Go formatting                                                 | `gofmt -l .` (or `go fmt ./...`) | Always            |
| Go vet                                                        | `go vet ./...`                   | Always            |
| Staticcheck (recommended)                                     | `staticcheck ./...`              | All new Go code   |
| Tests                                                         | `go test -count=1 ./...`         | Always            |
| Security gating (`docs/architecture/security.md` regressions) | manual review                    | Sensitive changes |


Hexagonal coding rules are enforced in review:

- Domain entities live in `internal/core/<module>/domain`.
- They **must not** import `gorm.io/…`, `github.com/gin-gonic/gin`,
`github.com/redis/…`, or any adapter framework.
- Ports live in `internal/core/<module>/ports`; adapters live in
`internal/adapters/…`.
- Refer to [docs/architecture/coding-standards.md](./docs/architecture/coding-standards.md) for full rules.



### 7.4 Review Assignments

- **Security, impersonation, RBAC, audit/activity, secrets handling** —
require **two reviews**, one of whom must be a repo admin.
- **Contract changes (**`openapi.yaml`**,** `paths/`**,** `components/`**)** — a reviewer
from the frontend-integration guild must approve if the endpoint is
consumer-facing.
- Everything else: one approving review from any active maintainer.



### 7.5 Merging

- **Squash merge** is the default.
- Linear history (no merge commits) is enforced on `main`.
- CI must be green; review requirements met; no outstanding `Request changes`.



### 7.6 Contributors

People who have authored code, contracts, or documentation merged into this
project (sorted alphabetically; full list also lives in `AUTHORS` at the repo
root):


| Name        | Email                                           | Focus                                                             |
| ----------- | ----------------------------------------------- | ----------------------------------------------------------------- |
| Nur Wachid  | [wachid@outlook.com](mailto:wachid@outlook.com) | API contracts, OpenAPI validation toolchain, domain schema design |
| Turahe Team | —                                               | Project stewardship, architecture, release management             |


To have yourself added here, land at least one PR on `main` and include a note
in the PR description asking to be added (or update `AUTHORS` directly in the
same PR).

---



## 8. Issue Feedback Channels



### 8.1 Filing a Bug Report

Use the **Bug Report** issue template. Include *at least*:

1. **Environment**:
  - OS / arch, Go version (`go version`), Node version (`node -v`)
  - Commit SHA of the code you're running
  - Deployment environment (`local` / Docker Compose / staging / custom)
2. **Reproduction steps** — a scriptable `curl` sequence or a failing unit
  test is worth a thousand words.
3. **Expected behavior** vs. **actual behavior**.
4. **Logs / stack traces**: attach redacted `slog` output, a GORM query log
  , or a panic trace. **Never paste** `DB_PASSWORD`**, session keys, bearer
   tokens, passwords, or real user data.** Redact before pasting.



### 8.2 Requesting a Feature

Use the **Feature Request** template. Include:

1. User / role who wants the feature (e.g. *editor*).
2. The problem it solves, not just the solution you want.
3. OpenAPI / AsyncAPI contract sketch, or a link to a PR that updates the
  spec.
4. Any RBAC or security implications (new permissions, impersonation, audit
  logging, PII exposure).



### 8.3 Security Disclosures

**Do not open a public issue.** Send the report directly to the maintainers
via:

- Email: `[security@turahe.dev](mailto:security@turahe.dev)`
- Keybase / Signal: available on request via security email for end-to-end
encrypted reports.

Expected turnaround:

- Triage within 2 business days.
- Fix or mitigation plan within 7 business days for high-severity issues.
- Coordinated disclosure window of 30–90 days per industry norms.



### 8.4 Maintainers


| Handle           | Role            | Domain                           |
| ---------------- | --------------- | -------------------------------- |
| `@turahe`        | Maintainer lead | Architecture, security, releases |
| `@platform-team` | Review          | Contract lint, infra, CI/CD      |


---



## 9. License

This project is distributed under the **MIT License**.

```text
MIT License

Copyright (c) 2026 Turahe and blog-api contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

All documentation files under `docs/` are additionally licensed under
**CC-BY-4.0** for the benefit of contributors publishing derived guides.

If you embed or redistribute this project in a commercial product, a
mention in your **NOTICE** or about page is appreciated but not required.

---



## Documentation

Deeper reads live under `docs/` and are the source of truth for design
decisions. Start here:

- [Product Requirements (PRD)](./docs/product/PRD.md) — goals, scope, success criteria.
- [Task Backlog](./docs/tasks/README.md) — per-phase checklists and current build status.
- [Project Configuration](./docs/deployment/config.md) — environment variables, defaults, and validation.
- [Architecture Overview](./docs/architecture/architecture.md) — hexagonal modular monolith, package
layout, contract ownership.
- [Tech Stack](./docs/architecture/tech-stack.md) — pinned versions + rationale.
- [Database Design](./docs/backend/database.md) — full tables, indexes, partitioning, RLS,
audit/activity schema, outbox.
- [Security Model](./docs/architecture/security.md) — RBAC, impersonation, passwords, secrets.
- [Deployment](./docs/architecture/deployment.md) — release steps, migration policy, rollback.
- [Coding Standards](./docs/architecture/coding-standards.md) — Go, API, testing, naming, review hygiene.
- [Changelog](./CHANGELOG.md) — per-date release notes.

