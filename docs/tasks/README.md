# Task Backlog

Delivery backlog and roadmap for the blog API, organised by six phases.
Each phase file says *what is left to build*; the goals below say *what each phase is for*.

| Phase | Doc | Status | Goal |
| --- | --- | --- | --- |
| 1 — Foundation | [phase-1-foundation.md](./phase-1-foundation.md) | Done | Bootstrap, config, DB/Redis, health, auth (including self-registration), user/role/permission model |
| 2 — Content Core | [phase-2-content-core.md](./phase-2-content-core.md) | Done | Posts, categories, tags, media, public reads, Redis caching |
| 3 — Collaboration and Moderation | [phase-3-collaboration-moderation.md](./phase-3-collaboration-moderation.md) | Done | Comments, moderation, audit logging, notification hooks |
| 4 — Event-Driven Platform | [phase-4-event-driven-platform.md](./phase-4-event-driven-platform.md) | Done | Watermill publishing, durable outbox, workers/jobs, media cache, ops hardening |
| 5 — Product Expansion | [phase-5-product-expansion.md](./phase-5-product-expansion.md) | Done | Richer admin UX, search, privacy/consent, media pipeline, newsletter, impersonation |
| 6 — Analytics and Reporting | [phase-6-analytics-reporting.md](./phase-6-analytics-reporting.md) | Done | Telemetry ingest, popular/retention widgets, search CTR, realtime admin views, export/retention |

## Status legend

| Marker | Meaning |
| --- | --- |
| `- [x]` | Implemented and covered by tests or verified manually |
| `- [ ]` | Not started or incomplete |
| `Done` | Every epic in the phase is complete |
| `Partial` | Some epics complete, others outstanding |
| `Planned` | Nothing implemented yet |

## Current baseline

Snapshot of the tree these files describe:

- **HTTP surface** — Gin `routes.Register*` binds in `internal/adapters/inbound/routes/` are the
  runtime source of truth; the published contract is swag-generated into
  [swagger.json](../swagger.json) (`make swagger`, `make routes-check`).
  `routes/swagger_parity_test.go` expects every swagger operation to be mounted (no global
  `501 operation.not_implemented` stubs).
- **Wired operations** — full auth (login/refresh/logout, password reset, 2FA, OAuth,
  `auth.register` / `auth.verify_email`), `/me` profile and privacy, admin users/roles,
  posts/categories/tags/media, comments and moderation, notifications, settings
  (`admin.settings.*`, `public.settings.get`), post revisions and SEO (`public.home.seo_meta`,
  `public.posts.seo_meta`), search on `public.posts.list`, newsletter, impersonation,
  analytics ingest/consent/reports/realtime/export. Enumerate via swagger or
  [api.md](../backend/api.md).
- **Schema** — PostgreSQL migrations `00001`–`00036` under
  `internal/platform/migrations/sql` (users through registrations, plus settings, revisions,
  SEO, search, impersonation, newsletter, analytics events/rollups/exports/salts, outbox
  relay, processed messages, scheduled jobs, privacy requests, and related indexes). Entity
  tables key on `id bigint` identity with a unique public `uuid`; only the UUID leaves the
  persistence adapters.
- **Platform** — PostgreSQL (plus Cloud SQL connector), Redis (fixed-window rate limits and
  the generation-keyed public read cache), ES256 JWT, Argon2id, Casbin enforcer,
  S3-compatible object storage, multi-broker Watermill messaging (`MESSAGE_BROKER`),
  production `app worker` / `app scheduler` / `app outbox`, and imgproxy transforms.
- **Audit log** — `audit_logs` (extended by `00016`) is written asynchronously by
  `middleware.Audit` and read by `me.activity.list` and `admin.users.activity.list`.
- **Outbox** — `outbox_events` is written in the business transaction and relayed by the
  worker with retries, parking, pruning, and metrics; consumers handle email, newsletter,
  notifications, and analytics live fan-out.

## Maintenance rules

- Tick a box only when the behaviour is implemented **and** verified (test or documented smoke).
- Update the phase `Status` line and the table above whenever the last open box in a phase closes.
- Keep task lines imperative and verifiable; name the operation ID or file path.
- Do not restate feature specs here — link to them.
- Relative links only, per [relative-link-usage-rules.md](../guides/relative-link-usage-rules.md).
- No deadlines, story points, or assignees in these files.

## Related

| Area | Doc |
| --- | --- |
| Product requirements | [PRD.md](../product/PRD.md) |
| API surface and route groups | [api.md](../backend/api.md) |
| Data models | [model.md](../backend/model.md) |
| Events and channels | [events.md](../backend/events.md) |
| Testing strategy | [strategy.md](../testing/strategy.md) |
| Security handbook | [README.md](../security/README.md) |
| Deployment handbook | [README.md](../deployment/README.md) |
