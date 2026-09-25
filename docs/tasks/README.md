# Task Backlog

Delivery backlog and roadmap for the blog API, organised by six phases.
Each phase file says *what is left to build*; the goals below say *what each phase is for*.

| Phase | Doc | Status | Goal |
| --- | --- | --- | --- |
| 1 — Foundation | [phase-1-foundation.md](./phase-1-foundation.md) | Done | Bootstrap, config, DB/Redis, health, auth, user/role/permission model |
| 2 — Content Core | [phase-2-content-core.md](./phase-2-content-core.md) | Done | Posts, categories, tags, media, public reads, Redis caching |
| 3 — Collaboration and Moderation | [phase-3-collaboration-moderation.md](./phase-3-collaboration-moderation.md) | Done | Comments, moderation, audit logging, notification hooks |
| 4 — Event-Driven Platform | [phase-4-event-driven-platform.md](./phase-4-event-driven-platform.md) | Done | Watermill publishing, durable outbox, workers/jobs, media cache, ops hardening |
| 5 — Product Expansion | [phase-5-product-expansion.md](./phase-5-product-expansion.md) | In progress | Richer admin UX, search, analytics dashboard, privacy/consent, media pipeline |
| 6 — Analytics and Reporting | [phase-6-analytics-reporting.md](./phase-6-analytics-reporting.md) | In progress | Telemetry ingest, popular/retention widgets, search CTR, realtime admin views, export/retention |

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

- **HTTP surface** — Gin `routes.Register*` binds in `internal/adapters/inbound/routes/` are the
  runtime source of truth; the published contract is swag-generated into
  [swagger.json](../swagger.json) (`make swagger`, `make routes-check`). Operations without a
  handler return `501 operation.not_implemented` via `routes.NotImplemented` (`stubs.go`,
  `analytics.go`).
- **Wired operations** — `health.live`, `health.ready`, `health.version`; `auth.login`,
  `auth.refresh`, `auth.logout`, `auth.password.forgot`, `auth.password.reset`,
  `auth.password.reset_token_validity`; `me.get`, `me.password.update`, `me.profile.patch`,
  `me.avatar.upload`, `me.avatar.delete`, `me.email.request_change`, `me.email.confirm_change`;
  `public.users.profile`; `admin.users.list`, `admin.users.profile.get`,
  `admin.users.profile.patch`;
  `admin.posts.create`, `admin.posts.list`, `admin.posts.update`, `admin.posts.publish`,
  `admin.posts.unpublish`, `admin.posts.archive`, `admin.posts.delete`, `admin.posts.restore`,
  `admin.posts.media.replace`; `admin.categories.list`, `admin.categories.create`,
  `admin.categories.update`, `admin.categories.delete`, `admin.categories.move`;
  `admin.tags.create`, `admin.tags.update`, `admin.tags.merge`, `admin.tags.delete`;
  `admin.media.create`, `admin.media.complete`, `admin.media.list`, `admin.media.delete`,
  `admin.media.tags.patch`; `public.posts.list`, `public.posts.get`, `public.categories.list`,
  `public.categories.get`, `public.tags.list`, `public.media.get`;
  `public.posts.comments.list`, `public.posts.comments.create`, `public.comments.get`,
  `public.comments.flag`, `self.comments.list`, `self.comments.patch`, `self.comments.delete`,
  `self.comments.upvote`; `admin.comments.list`, `admin.comments.get`, `admin.comments.stats`,
  `admin.comments.moderate`, `admin.comments.bulk_moderate`, `admin.comments.delete`.
- **Schema** — PostgreSQL migrations `00001`–`00012` create users, user_profiles,
  user_privacy_settings, roles, permissions, user_roles, role_permissions, posts (slug unique
  among live rows), categories (nested set), tags, post_tags, comments, comment_flags,
  comment_upvotes, comment_moderation_log, audit_logs, outbox_events, refresh_sessions,
  password_reset_tokens (also used for email change), casbin_rules, media_assets, post_media,
  plus list indexes. Entity tables key on `id bigint` identity with a
  unique public `uuid`; only the UUID leaves the persistence adapters.
- **Platform** — PostgreSQL (plus Cloud SQL connector), Redis (fixed-window rate limits and
  the generation-keyed public read cache), ES256 JWT, Argon2id, Casbin enforcer,
  S3-compatible object storage, and multi-broker Watermill messaging (`MESSAGE_BROKER`) with an
  `app worker` scaffold.
- **Audit log** — `audit_logs` (extended by `00016`) is written asynchronously by
  `middleware.Audit` and read by `me.activity.list` and `admin.users.activity.list`.
- **Schema-only** — the `outbox_events` table exists with no service or HTTP layer.

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
