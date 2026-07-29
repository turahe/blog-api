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
- Google Cloud SQL is the managed relational data plane in production deployments
  - managed connectivity via **`cloud.google.com/go/cloudsqlconn`** Go connector with IAM Database Authentication (no static passwords), ephemeral mTLS certificate auto-rotation, 1-hour cert refresh cycle, Private Service Connect private IP path (no public IP on instances)
  - both PostgreSQL 15+ (source of truth) and MySQL 8.0+ (future secondary stores) supported through the same connector; see [backend/database.md Cloud SQL section](../backend/database.md#L62-L457) for go.mod pins, pooling parameters, private IP setup, org-policy guardrails, and troubleshooting
- Redis stores cache entries, rate-limit counters, and short-lived session/security state
- object storage stores original media files and optionally hot transformed variants
- Watermill publishes domain events such as post, auth, and notification events

## Recommended Supporting Libraries

- UUID generation library
- structured logging with `log/slog`
- validation package for request validation
- OpenTelemetry-compatible instrumentation
- testing with Go `testing` + `testify`
- **Mermaid.js 10.x** for in-repo, version-controlled architecture, sequence, and ER diagrams. Diagrams in this repository are authored, validated, and governed under the [Mermaid Competency Framework](../guides/mermaid-competency-framework.md) — proficiency bands, platform integration, theming, responsive, accessibility (WCAG 2.1 AA), CI syntax validation, and maintenance guidelines are all enforced there. The canonical production-grade Mermaid document in this repository is the PostgreSQL source-of-truth ERD in [backend/ERD.md](../backend/ERD.md).

## Frontend Applications

This project ships two distinct Next.js frontend applications sharing a component library and
REST/HTTP client layer but using different rendering strategies matched to their traffic profile
and security requirements.

### Customer-Facing Web Application

- Framework: Next.js (App Router) + React + TypeScript
- Rendering strategy: Server-Side Rendering (SSR) with Incremental Static Regeneration (ISR)
  where appropriate, and streaming Server Components for personalised views.
- Styling: Tailwind CSS with a design-token theme and responsive breakpoints aligned to the
  design system in `docs/frontend/design-system.md`.
- HTTP client: typed fetch wrappers generated from the OpenAPI contract in
  `contracts/openapi.yaml` for end-to-end type safety with the Go/Gin backend.
- Runtime: Node.js server process colocated with or proxied in front of the Go API; SSR requests
  are authenticated via the same http-only session cookies issued by the backend so no tokens
  are exposed to client JavaScript for the critical reading path.
- Justification:
  - Personalised home feeds, comment threads, author profiles, and per-user newsletter
    preferences require per-request data assembly that cannot be fully static.
  - SSR delivers first-byte times comparable to static HTML while keeping content fresh and
    correctly surfaced for SEO crawlers on every render.
  - Server Components reduce the client bundle shipped to mobile and low-bandwidth readers.
  - Incremental Static Regeneration is applied to individual post and category pages when the
    page has been stable for a configurable TTL, trading freshness against load for popular
    long-tail content.

### Administrative Backend Interface

- Framework: Next.js (App Router) + React + TypeScript, shared UI primitives with the customer
  app.
- Rendering strategy: Static Site Generation (SSG) at deploy time, paired with small
  authenticated CSR islands for management interactions and dynamic tables.
- Distribution: pre-built HTML/CSS/JS bundle served from the object-storage/CDN tier; the
  management hostname is CORS-restricted and fronted by the same WAF/rate-limit layer that
  protects `/api/v1/admin/*`.
- Build-time pages: statically generated layouts, empty-state screens, and the auth-gate
  shell; list/detail views are fetched on-demand in the browser by the typed API client after
  the admin session cookie is verified.
- Justification:
  - Management operations have a small internal audience, so serving pre-built HTML guarantees
    the fastest possible Time to Interactive on cold admin sessions.
  - SSG eliminates a warm server process for the admin domain and reduces the attack surface:
    the only dynamic entry points are the same `/api/v1/admin/*` endpoints already documented
    in the contract.
  - Static output is easier to cache at the edge, easier to audit for unintended data leaks,
    and naturally SEO-friendly for any indexed help/support pages bundled into the admin app.
  - Mutation flows (post edit, comment moderation, newsletter issue composition) are kept as
    client-side React islands to keep interactions snappy while still reusing the shared
    contract-typed API layer.

### Common Frontend Dependencies

- UI foundations: React 18+, TypeScript (strict), Tailwind CSS.
- Build pipeline: Next.js built-in bundler and minifier; Vite-style fast refresh via the
  Next.js dev server for local work.
- Validation and forms: shared zod schemas re-derived from the OpenAPI contract schemas.
- Testing: Vitest + React Testing Library for unit/component coverage, Playwright for the
  SSR/SSG render smoke suite documented in `docs/backend/testing.md`.
- Accessibility and browser targets: WCAG 2.1 AA baseline, evergreen desktop + mobile browsers
  matching the compatibility matrix in `docs/frontend/browser-support.md`.

## Versioning Policy

- pin major versions for critical libraries
- upgrade infrastructure libraries intentionally, not opportunistically
- document breaking changes before adoption
