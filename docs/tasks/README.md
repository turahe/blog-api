# Task Backlog

Delivery backlog and roadmap for the blog API, organised by six phases.
Each phase file says *what is left to build*; the goals below say *what each phase is for*.

| Phase | Doc | Status | Goal |
| --- | --- | --- | --- |
| 1 — Foundation | [phase-1-foundation.md](./phase-1-foundation.md) | Partial | Bootstrap, config, DB/Redis, health, auth, user/role/permission model |
| 2 — Content Core | [phase-2-content-core.md](./phase-2-content-core.md) | Partial | Posts, categories, tags, media, public reads, Redis caching |
| 3 — Collaboration and Moderation | [phase-3-collaboration-moderation.md](./phase-3-collaboration-moderation.md) | Planned | Comments, moderation, audit logging, notification hooks |
| 4 — Event-Driven Platform | [phase-4-event-driven-platform.md](./phase-4-event-driven-platform.md) | Partial | Watermill publishing, durable outbox, workers/jobs, media cache, ops hardening |
| 5 — Product Expansion | [phase-5-product-expansion.md](./phase-5-product-expansion.md) | Planned | Richer admin UX, search, analytics dashboard, privacy/consent, media pipeline |
| 6 — Analytics and Reporting | [phase-6-analytics-reporting.md](./phase-6-analytics-reporting.md) | Planned | Telemetry ingest, popular/retention widgets, search CTR, realtime admin views, export/retention |

## Status legend

| Marker | Meaning |
| --- | --- |
| `- [x]` | Implemented and covered by tests or verified manually |
| `- [ ]` | Not started or incomplete |
| `Done` | Every epic in the phase is complete |
| `Partial` | Some epics complete, others outstanding |
| `Planned` | Nothing implemented yet |

## Current baseline

Snapshot of the tree these files were written against:

- **HTTP surface** — hand-maintained routes in `internal/adapters/inbound/routes/api.go`.
  About 19 are wired to real handlers; the rest return `501` through `notImplementedHandler`.
- **Wired operations** — `health.live`, `health.ready`, `health.version`, `auth.login`,
  `auth.refresh`, `auth.logout`, `auth.password.forgot`, `auth.password.reset`,
  `auth.password.reset_token_validity`, `me.get`, `me.password.update`, `admin.users.list`,
  `admin.posts.create`, `admin.posts.publish`, `public.posts.list`, `public.posts.get`,
  `public.categories.list`, `public.categories.get`, `public.tags.list`.
- **Schema** — migrations `00001`–`00004` create users, roles, permissions, user_roles,
  role_permissions, posts, categories, tags, post_tags, comments, audit_logs, outbox_events,
  refresh_sessions, password_reset_tokens, casbin_rules.
- **Platform** — multi-dialect database (`postgres` / `mysql` / `sqlserver`) plus Cloud SQL
  connector, Redis, JWT, Casbin enforcer, and multi-broker Watermill messaging
  (`MESSAGE_BROKER`) with an `app worker` scaffold.
- **Schema-only** — `comments` and `outbox_events` tables exist with no service or HTTP layer.

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
