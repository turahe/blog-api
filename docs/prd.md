# PRD: Blog Backend REST API

## 1. Product Summary

Build a production-ready backend REST API for a blog platform using Go, Gin, GORM, PostgreSQL, Redis, and Watermill. The system must support content management for administrators and content delivery for public consumers, while also enabling event-driven workflows for caching, notifications, and future integrations.

## 2. Problem Statement

The product needs a backend that can reliably manage blog content, users, and interactions without coupling everything into synchronous request flows. The system should provide a fast and clean REST API, a durable data model, and an event-based foundation for background processing and future feature growth.

## 3. Goals

- provide admin APIs to manage blog content end to end
- provide public APIs to read published content efficiently
- support authentication and role-based permissions for admin users
- support multiple internal users with assignable roles and permissions
- introduce event-driven workflows using Watermill
- improve response performance for public reads using Redis caching
- establish a maintainable service structure for long-term growth

## 4. Non-Goals

- building the frontend admin panel in this phase
- full-text search service integration in the initial release
- multi-tenant architecture
- distributed microservices in the initial version
- complex media processing pipelines

## 5. Target Users

- administrators managing the platform
- editors reviewing and publishing content
- authors creating and updating posts
- moderators reviewing comments and user-generated content
- public clients consuming published blog content
- internal systems consuming backend events

## 6. User Stories

- As an admin, I can log in securely and manage authors and editors.
- As a super admin, I can create users, assign roles, and manage permissions.
- As an author, I can create, update, and save posts as drafts.
- As an editor, I can review and publish content.
- As a moderator, I can review and moderate comments without getting full admin access.
- As a public client, I can list published posts and fetch post details by slug.
- As a reader, I can submit comments for moderation.
- As the system, I can publish events when important domain actions happen.
- As an operator, I can observe service health and troubleshoot failures.

## 7. Scope

### In Scope

- admin authentication
- role-based authorization
- permission-based access control
- multi-user management
- CRUD for posts
- CRUD for categories and tags
- public post listing and detail APIs
- comment submission and moderation
- Redis caching for public content
- Watermill-based event publication and consumption
- audit-friendly logging and health endpoints

### Out of Scope

- billing or subscription features
- external SSO providers
- real-time chat or websocket features
- automated content generation

## 8. Functional Requirements

### 8.1 Authentication and Authorization

- The system must provide admin login.
- The system must issue authenticated sessions or tokens for admin access.
- The system must enforce role-based access control.
- The system must support multiple internal users.
- The system must support assigning one or more roles to a user.
- The system must support fine-grained permissions attached to roles.
- The system must deny privileged actions when the authenticated user lacks the required permission.
- The system must allow protected routes for admin-only operations.

### 8.2 User, Role, and Permission Management

- Authorized admins must be able to create, update, deactivate, and list internal users.
- Authorized admins must be able to create, update, and list roles.
- Authorized admins must be able to assign and revoke roles for users.
- Authorized admins must be able to manage the permission set attached to a role.
- The system must keep an audit trail for user, role, and permission changes.

### 8.3 Post Management

- Admin users must be able to create, update, retrieve, delete, publish, unpublish, and archive posts.
- Posts must support draft and published workflows.
- Each post must include title, slug, content, excerpt, SEO fields, status, and author.
- Slugs must be unique.

### 8.4 Category and Tag Management

- Admin users must be able to manage categories and tags.
- Posts must support one category and multiple tags.
- Public APIs must expose category and tag data tied to published content.

### 8.5 Public Content APIs

- Public clients must be able to list published posts with pagination.
- Public clients must be able to fetch a single published post by slug.
- Public clients must be able to filter posts by category, tag, author, or search keyword if implemented.
- Responses must exclude unpublished content.

### 8.6 Comments

- Public clients must be able to submit comments on published posts.
- Comments must enter a moderation flow before public visibility.
- Admin users must be able to approve, reject, or mark comments as spam.

### 8.7 Caching

- Public post list and detail endpoints should use Redis-backed caching.
- Cache entries must be invalidated or refreshed when posts are updated or published.

### 8.8 Event-Driven Workflows

- The system must emit domain events for major actions.
- Event publishing must be reliable and consistent with database changes.
- Consumers must support at least cache invalidation and audit or notification hooks.

### 8.9 Observability and Operations

- The system must expose health endpoints.
- The system must support structured application logs.
- The system must attach request IDs to requests and error logs.

## 9. Example Event Catalogue

- `blog.post.created`
- `blog.post.updated`
- `blog.post.published`
- `blog.post.archived`
- `blog.comment.created`
- `blog.comment.approved`
- `blog.auth.logged_in`
- `blog.user.created`
- `blog.user.role_assigned`
- `blog.role.updated`

## 10. API Surface

### Admin Endpoints

- `POST /api/v1/admin/auth/login`
- `POST /api/v1/admin/auth/refresh`
- `GET /api/v1/admin/users`
- `POST /api/v1/admin/users`
- `PATCH /api/v1/admin/users/:id`
- `POST /api/v1/admin/users/:id/roles`
- `DELETE /api/v1/admin/users/:id/roles/:roleId`
- `GET /api/v1/admin/roles`
- `POST /api/v1/admin/roles`
- `PATCH /api/v1/admin/roles/:id`
- `POST /api/v1/admin/roles/:id/permissions`
- `GET /api/v1/admin/posts`
- `POST /api/v1/admin/posts`
- `GET /api/v1/admin/posts/:id`
- `PATCH /api/v1/admin/posts/:id`
- `DELETE /api/v1/admin/posts/:id`
- `POST /api/v1/admin/posts/:id/publish`
- `POST /api/v1/admin/posts/:id/archive`
- `GET /api/v1/admin/categories`
- `POST /api/v1/admin/categories`
- `GET /api/v1/admin/tags`
- `POST /api/v1/admin/tags`
- `GET /api/v1/admin/comments`
- `POST /api/v1/admin/comments/:id/approve`
- `POST /api/v1/admin/comments/:id/reject`

### Public Endpoints

- `GET /api/v1/posts`
- `GET /api/v1/posts/:slug`
- `GET /api/v1/categories`
- `GET /api/v1/tags`
- `POST /api/v1/posts/:id/comments`

### System Endpoints

- `GET /health/live`
- `GET /health/ready`

## 11. Data Requirements

Core entities:

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
- audit_logs
- outbox_events

Recommended post status values:

- draft
- scheduled
- published
- archived

Recommended comment status values:

- pending
- approved
- rejected
- spam

Recommended user status values:

- active
- inactive
- suspended

Recommended default roles:

- admin
- editor
- author
- moderator

Recommended permission keys:

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

## 12. Non-Functional Requirements

- API p95 latency for cached public reads should target under 150 ms under normal load.
- API p95 latency for non-cached standard reads should target under 300 ms under normal load.
- Authenticated write endpoints must validate payloads and return consistent error responses.
- The system must support horizontal scaling of stateless API instances.
- Redis outages should degrade gracefully for cache-backed reads.
- Event consumers should be retry-capable and idempotent where practical.
- All critical config must be environment-driven.

## 13. Security Requirements

- passwords must be hashed securely
- admin routes must require authentication
- authorization must be role-aware
- authorization must also be permission-aware
- request payloads must be validated
- auth endpoints must be rate limited
- secrets must never be hard-coded
- audit logs should capture privileged actions

## 14. Success Metrics

- admins can create and publish posts without direct database access
- admins can create multiple internal users and assign them appropriate access levels
- public consumers can retrieve published posts with stable response times
- cache invalidation works correctly after post changes
- key domain actions emit events that are consumed successfully
- moderation workflows reduce unreviewed public comments

## 15. Risks and Mitigations

- Event consistency risk: use an outbox pattern and retryable consumers.
- Cache staleness risk: invalidate cache on publish and update events.
- Permission drift risk: centralize authorization logic in middleware and services.
- Schema growth risk: keep migrations explicit and review indexes early.
- Comment abuse risk: add rate limits and moderation states from day one.

## 16. Delivery Phases

### Phase 1

- project bootstrap
- config management
- PostgreSQL and Redis connectivity
- health endpoints
- auth foundation
- user, role, and permission foundation

### Phase 2

- post CRUD
- categories and tags
- public read APIs
- initial caching

### Phase 3

- comments and moderation
- Watermill event publishing
- background consumers for cache invalidation and notifications

### Phase 4

- hardening
- observability
- performance tuning
- production deployment readiness

## 17. Acceptance Criteria

- Admin users can authenticate and access protected routes.
- Authorized admins can create and manage multiple internal users.
- Roles and permissions are enforced consistently across admin endpoints.
- Authors and editors can manage posts through REST endpoints.
- Public APIs return only published content.
- Redis caching is active for high-read public endpoints.
- Watermill events are emitted for major post and comment actions.
- Comment moderation states are enforced correctly.
- Health endpoints reflect dependency readiness accurately.
- The service can be deployed with environment-based configuration.

## 18. Open Decisions

- choose JWT vs server-backed sessions for admin auth
- choose migration tool strategy
- choose Watermill transport backend for production
- decide whether users can hold multiple roles or one primary role plus overrides
- decide whether comments require email verification
- decide whether scheduled publishing is part of MVP or a follow-up
