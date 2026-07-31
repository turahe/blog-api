# Architecture

## Overview

This project uses a **hexagonal modular monolith** architecture for a Go backend built with Gin, GORM, PostgreSQL, Redis, and Watermill.

The main goal is to keep business logic independent from frameworks and infrastructure, while organizing the system into clearly bounded domain modules that can evolve independently and later be extracted into services if necessary.

## API and Event Contracts

The canonical contracts are kept under `contracts/` and serve as the source of truth for synchronous APIs and asynchronous event/stream interfaces.

- `contracts/openapi.yaml` — OpenAPI 3.1 REST specification
- `contracts/asyncapi.yaml` — AsyncAPI 2.6 event/stream/job specification

Use these files for documentation generation, validation in CI, code generation for clients or mock servers, and contract-based testing. See `docs/architecture/api-contracts.md` for usage and validation commands.

### Realtime (SSE) Stream Contracts

Long-lived streaming endpoints are modelled in both OpenAPI (REST path, auth, response headers and error codes) and AsyncAPI (per-user channel parameters, SSE binding to the HTTP endpoint, message payloads).

| Stream channel | OpenAPI path | Scope | Purpose |
|---|---|---|---|
| `notifications.user.{user_id}.stream` | `GET /api/v1/me/notifications/stream` | authenticated user-scoped | Realtime in-app notifications (created/read/dismissed), session lifecycle, and keep-alive frames. |
| `analytics.admin.realtime.stream` | `GET /api/v1/admin/analytics/realtime/stream` | admin + `analytics.read_all` | Live admin dashboards, impersonation audit, system health. |

All SSE endpoints:

- require a valid authenticated bearer token or http-only session cookie
- use strict CORS origin whitelist + CSRF gate for browser callers
- rate limit per user (concurrent streams + reconnect rate) with 429 `Retry-After`
- send standard `event:`, `data:`, `id:`, and `retry:` frames per the WHATWG Server-Sent Events specification
- fan out via internal Watermill → Redis pub/sub bridge to support multi-process deployments
- are documented end-to-end in [realtime-notifications-sse.md](../features/realtime-notifications-sse.md)

## Core Principles

- **Domain-centric**: business logic lives in `internal/core/<module>/domain`
- **Ports and Adapters**: the core declares inbound and outbound contracts in `internal/core/<module>/ports`
- **Application use cases**: application services in `internal/core/<module>/service` orchestrate flows through ports
- **Infrastructure isolation**: adapters in `internal/adapters` implement HTTP, persistence, cache, events, storage, and messaging
- **Dependency inversion**: the core must not depend on any adapter or framework package
- **Modularity**: every domain module owns its own domain, ports, and service package

## Recommended Package Layout

```text
project/
├── cmd/                  # package main — Cobra entrypoint and subcommands
│   ├── main.go
│   ├── command.go        # serve / migrate / seed / doctor / version
│   └── worker.go         # app worker
│
├── internal/
│   ├── bootstrap/        # DI and lifecycle wiring
│   ├── platform/         # infra primitives shared by adapters
│   │   ├── config/
│   │   ├── logger/
│   │   ├── database/
│   │   ├── messaging/    # Watermill factory (kafka / rabbitmq / googlepubsub)
│   │   └── redis/
│   │
│   ├── core/
│   │   ├── auth/
│   │   │   ├── domain/
│   │   │   ├── ports/
│   │   │   └── service/
│   │   ├── user/
│   │   │   ├── domain/
│   │   │   ├── ports/
│   │   │   └── service/
│   │   ├── rbac/
│   │   │   ├── domain/
│   │   │   ├── ports/
│   │   │   └── service/
│   │   ├── post/
│   │   │   ├── domain/
│   │   │   ├── ports/
│   │   │   └── service/
│   │   ├── media/
│   │   │   ├── domain/
│   │   │   ├── ports/
│   │   │   └── service/
│   │   ├── comment/
│   │   │   ├── domain/
│   │   │   ├── ports/
│   │   │   └── service/
│   │   └── notification/
│   │       ├── domain/
│   │       ├── ports/
│   │       └── service/
│   │
│   └── adapters/
│       ├── inbound/
│       │   └── http/      # Gin handlers, middleware, DTOs
│       └── outbound/
│           ├── persistence/  # GORM repositories
│           ├── cache/        # Redis adapters
│           ├── events/       # Watermill publishers/consumers
│           ├── storage/      # S3/R2 media adapters
│           ├── imageproc/    # image transform engine
│           ├── scanner/      # malware scanner adapter
│           ├── mailer/       # email provider adapter
│           └── oauth/        # social login provider adapter
│
├── configs/
├── migrations/
├── scripts/
├── docs/
└── Makefile
```

## Hexagonal Mapping

Each domain module follows the same hexagonal structure:

- `domain/`: entities, value objects, enums, business rules, domain errors
- `ports/inbound.go`: inbound interfaces exposed to the outside world (use cases)
- `ports/outbound.go`: outbound interfaces required by the core (repositories, cache, events, external services)
- `service/`: application services implementing inbound ports and calling outbound ports

### Inbound Ports

Examples of inbound port types:

- `AuthServicePort`: login, refresh, challenge 2FA
- `UserServicePort`: get profile, update profile
- `PostServicePort`: create, update, publish, archive
- `MediaServicePort`: upload, list, delete, fetch transformed variant

### Outbound Ports

Examples of outbound port types:

- `UserRepository`
- `PostRepository`
- `MediaAssetRepository`
- `MediaStorage` (R2/S3 abstraction)
- `MediaProcessor` (resize/format/transform)
- `CacheStore`
- `EventPublisher`
- `Mailer`
- `OAuthProvider`

### Inbound Adapters

- HTTP REST (Gin handlers)
- **SSE (Server-Sent Events)** for real-time notification streams to authenticated clients
- CLI commands (Cobra commands trigger service use cases)
- future: GraphQL, gRPC, queue consumer adapters

### Outbound Adapters

- persistence: GORM with `DB_DRIVER` ∈ {`postgres`, `mysql`, `sqlserver`}; production preferred plane is **Google Cloud SQL** via `cloud.google.com/go/cloudsqlconn` (Private Service Connect, IAM DB auth for Postgres/MySQL, ephemeral mTLS). Full config is in [backend/database.md Cloud SQL Connectivity](../backend/database.md#google-cloud-sql-connectivity-cloudgooglecomgocloudsqlconn). Bootstrap wiring lives in `internal/platform/database`.
- cache: Redis
- events: Watermill (publishers + subscribers)
- storage: Cloudflare R2 + S3-compatible providers (MinIO, DigitalOcean Spaces, AWS S3)
- image processing: image transform engine with WebP/AVIF output
- security: malware scanner
- notifications: email provider
- identity: Google, GitHub, OIDC social login

## Cobra CLI

Use Cobra as the CLI framework for all entrypoints.

Standard commands:

| Command | Purpose |
|---------|---------|
| `app serve` | Start the HTTP API server (Gin) |
| `app worker` | Start Watermill event consumers and async job workers |
| `app scheduler` | Start recurring cron-style background jobs (cleanup, warmup, retries) |
| `app migrate up` | Apply pending database migrations |
| `app migrate down` | Roll back the last migration batch |
| `app seed` | Seed initial reference data, default roles, permissions, and admin user |
| `app doctor` | Run dependency diagnostics: DB, Redis, object storage, Watermill, config, env |
| `app version` | Print build version, commit, build time, and Go runtime info |

CLI rules:

- all commands must read configuration from environment variables first, with optional config file overlay
- `migrate up` must run before `serve` or `worker` receive production traffic
- `doctor` must exit non-zero when any required dependency is unhealthy
- `seed` must be idempotent and never overwrite explicitly modified production data
- commands should reuse the same bootstrap/wiring helpers so DI stays identical across modes

## Dependency Direction

Allowed dependency flow:

```text
inbound adapters
    -> inbound ports (use cases)
    -> application/services
    -> domain + outbound ports
    <- outbound adapters
```

Not allowed:

- domain importing adapters
- services directly importing Gin, GORM, Redis, Watermill, or other adapter packages
- handlers containing business rules
- outbound adapters importing types from HTTP or other inbound layers
- direct cross-module imports between domain packages (prefer explicit port interfaces if needed)

## Module Boundaries

Initial domain modules:

- `auth`
- `user`
- `rbac`
- `post`
- `category`
- `tag`
- `comment`
- `media`
- `notification`
- `health`

Module rules:

- a module owns its tables, repository ports, domain events, and use cases
- cross-module communication must happen through domain events or explicit port interfaces, not direct repository access
- when module coupling grows beyond events, plan an explicit application orchestration layer instead of leaking logic into handlers

## Integration Boundaries

Inbound integrations:

- REST API via Gin
- **SSE stream for authenticated notification feeds**
- Cobra CLI commands
- future admin UI or internal CLIs

Outbound integrations:

- PostgreSQL for source-of-truth data
- Redis for cache and ephemeral state
- Watermill for asynchronous events
- R2 and S3-compatible object storage for media files
- image processing engine for on-demand transforms
- email provider for notifications
- OAuth / OIDC providers for social login
- malware scanner for uploaded media

## Architectural Rules

- keep use cases in the service/application layer
- keep ORM models separate from domain entities where helpful
- prefer DTOs for transport and adapter boundaries
- emit domain events from use cases, not handlers
- use the outbox pattern for durable event publishing
- validate in adapters, enforce invariants in domain
- authorization checks live in middleware + service layer; UI-only checks are never sufficient
- configuration flows into bootstrap and adapters, never into domain packages

## Why a Hexagonal Modular Monolith

Benefits for this backend:

- business logic remains framework-agnostic and testable without infrastructure
- storage, cache, messaging, and auth providers can be swapped through ports
- domain modules can later move to separate services without rewriting core rules
- a single codebase keeps development and local DX simple while preserving future split options
