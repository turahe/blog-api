# AI Context: Blog Backend API

## Project Intent

This repository is the backend service for a blog platform. The system exposes REST APIs for an admin console and public consumers, persists relational data in PostgreSQL, uses Redis for caching and short-lived state, and publishes domain events through Watermill for internal workflows and future integrations.

The backend should be optimized for:

- clean separation of transport, business logic, and persistence
- predictable REST API behavior
- clear domain events for asynchronous workflows
- production readiness from the start

## Core Stack

- Language: Go
- HTTP framework: Gin
- ORM: GORM
- Primary database: PostgreSQL
- Cache and ephemeral state: Redis
- Eventing and messaging: Watermill

## Suggested Architecture

Use a hexagonal modular monolith with clear domain boundaries, where core business logic is decoupled from external infrastructure and transport layers. This maintains the flexibility to split modules into independent services later while enforcing clean separation of concerns.

The package layout aligns with hexagonal (ports-and-adapters) principles, isolating domain logic from external dependencies:

```text
/main.go
/cmd
/internal/bootstrap
/internal/core
/internal/core/auth/domain
/internal/core/auth/ports
/internal/core/auth/service
/internal/core/user/domain
/internal/core/user/ports
/internal/core/user/service
/internal/core/post/domain
/internal/core/post/ports
/internal/core/post/service
/internal/adapters/inbound/http
/internal/adapters/inbound/http/middleware
/internal/adapters/outbound/persistence
/internal/adapters/outbound/cache
/internal/adapters/outbound/events
/internal/platform/config
/internal/platform/database
/internal/platform/redis
/internal/platform/logger
/internal/platform/watermill
/pkg
```

Architecture rules:

- `core/domain` contains entities, value objects, and business rules only
- `core/ports` defines inbound and outbound interfaces
- `core/service` implements use cases through ports
- `adapters/inbound` exposes use cases to HTTP or other entrypoints
- `adapters/outbound` implements database, cache, and event integrations
- `bootstrap` wires dependency injection
- the core must not import Gin, GORM, Redis, Watermill, or other infrastructure packages

## Domain Modules

Start with these modules:

- auth
- roles and permissions
- users
- posts
- categories
- tags
- comments
- media metadata
- system health

### Auth

Responsibilities:

- admin login
- token refresh or session renewal
- authentication for multiple internal users
- role-based access control for admin operations
- permission checks for protected actions

Suggested default roles:

- `admin`
- `editor`
- `author`
- `moderator`

Permission model:

- a user may have one or more roles
- a role contains one or more permissions
- permissions are evaluated per action such as `post.create`, `post.publish`, `comment.moderate`, `user.manage`
- authorization should be enforced in inbound adapters and application services, not only in controllers

### Users and RBAC

Support multiple internal users from day one.

Core entities:

- users
- roles
- permissions
- user_roles
- role_permissions

Recommended user fields:

- id
- full name
- email
- password hash
- status
- last login at

Recommended user status values:

- active
- inactive
- suspended

Recommended permission groups:

- `user.read`
- `user.create`
- `user.update`
- `user.delete`
- `role.read`
- `role.manage`
- `post.create`
- `post.update`
- `post.publish`
- `post.archive`
- `comment.moderate`

### Posts

Core post lifecycle:

- draft
- scheduled
- published
- archived

Key fields:

- title
- slug
- excerpt
- content
- cover image
- seo title
- seo description
- status
- published at
- author id

### Categories and Tags

Posts may belong to one category and many tags. Slugs must be unique within their resource type.

### Comments

Support moderated comments. Recommended statuses:

- pending
- approved
- rejected
- spam

## REST API Direction

Split routes into public and admin concerns.

Examples:

- `POST /api/v1/admin/auth/login`
- `GET /api/v1/admin/posts`
- `POST /api/v1/admin/posts`
- `PATCH /api/v1/admin/posts/:id`
- `POST /api/v1/admin/posts/:id/publish`
- `GET /api/v1/posts`
- `GET /api/v1/posts/:slug`
- `GET /api/v1/categories`
- `GET /api/v1/tags`
- `POST /api/v1/posts/:id/comments`

Basic API conventions:

- JSON request and response bodies
- versioned routes under `/api/v1`
- pagination for list endpoints
- consistent error envelope
- idempotent update semantics where practical

Recommended response envelope:

```json
{
  "data": {},
  "meta": {},
  "error": null
}
```

Recommended error envelope:

```json
{
  "data": null,
  "meta": {
    "request_id": "..."
  },
  "error": {
    "code": "validation_error",
    "message": "title is required",
    "details": {
      "field": "title"
    }
  }
}
```

## Persistence Guidance

### PostgreSQL

Use PostgreSQL as the source of truth for:

- users
- roles
- permissions
- user_roles
- role_permissions
- posts
- categories
- tags
- post_tags
- comments
- audit logs
- outbox or event records if using transactional event publishing

Guidance:

- use the `uuid` column as the only external identifier; `id` is an internal bigint primary key
- maintain unique indexes for slugs and emails
- maintain unique indexes for role names and permission keys
- add `created_at`, `updated_at`, and nullable `deleted_at` where soft delete is useful
- keep migrations explicit and reversible

### Redis

Use Redis for:

- access token denylist or session lookup
- rate limiting counters
- cache for public post lists and detail responses
- short-lived background workflow coordination

Do not treat Redis as a source of truth.

## Eventing with Watermill

Watermill is used for asynchronous domain and integration events.

Start with domain events such as:

- `blog.user.created`
- `blog.user.role_assigned`
- `blog.role.updated`
- `blog.post.created`
- `blog.post.updated`
- `blog.post.published`
- `blog.comment.created`
- `blog.comment.approved`
- `blog.user.logged_in`

Likely consumers:

- cache invalidation
- search indexing
- notifications
- analytics hooks
- audit enrichment

Important rule:

Use the outbox pattern or an equivalent transactional publishing approach so database changes and emitted events stay consistent.

Suggested event payload shape:

```json
{
  "event_id": "uuid",
  "event_name": "blog.post.published",
  "occurred_at": "2026-07-28T00:00:00Z",
  "aggregate_id": "post_uuid",
  "actor_id": "user_uuid",
  "version": 1,
  "payload": {
    "post_id": "post_uuid",
    "slug": "hello-world",
    "published_at": "2026-07-28T00:00:00Z"
  }
}
```

## Security Expectations

- protect admin APIs with authentication middleware
- enforce RBAC and permission checks for every privileged action
- hash passwords with a modern algorithm such as bcrypt or argon2id
- validate and sanitize request payloads
- use request IDs and structured logs
- apply rate limiting to auth and comment endpoints
- store secrets in environment variables

## Quality Bar

Prefer these defaults:

- handler tests for request validation and response contracts
- service tests for business rules
- repository tests against a real PostgreSQL test database where possible
- event publishing tests around important workflows

## Operational Expectations

- health endpoint for liveness and readiness
- graceful shutdown
- configuration via environment variables
- structured logging
- metrics-ready middleware
- migration workflow in CI or deployment pipeline

## Assumptions for Future Contributors

- this service owns the blog domain data
- this service supports multiple backoffice users with roles and permissions
- an admin frontend will consume admin endpoints
- public web or mobile clients will consume public endpoints
- asynchronous integrations should subscribe through Watermill rather than being embedded into request handlers
- code should be written for clarity first, then optimized using profiling and production evidence
