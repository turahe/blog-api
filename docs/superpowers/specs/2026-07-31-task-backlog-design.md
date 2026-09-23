# Task Backlog Docs Design

Date: 2026-07-31
Status: implemented
Scope: Create `docs/tasks/*.md` — a phase-organised, checklist-based delivery backlog derived from the PRD, feature specs, and the actual implementation state.

## Goal

Give contributors (human and agent) a single place to see what is built, what is partially built, and what remains — organised by six delivery phases, with actionable checklists that link back to the authoritative specs.

## Non-goals

- Inventing deadlines, sprints, story points, or assignees
- Duplicating feature spec content (backlog links, it does not restate)
- Maintaining a separate high-level roadmap outside `docs/tasks/` (phase goals live in [README.md](../../tasks/README.md))
- Tracking task state in code or a database (Markdown checkboxes only)

## Structure

```text
docs/tasks/
├── README.md                              index + status legend + maintenance rules
├── phase-1-foundation.md
├── phase-2-content-core.md
├── phase-3-collaboration-moderation.md
├── phase-4-event-driven-platform.md
├── phase-5-product-expansion.md
└── phase-6-analytics-reporting.md
```

Phase names are the six delivery phases listed in the tasks index.

## Per-file layout

Each phase file contains, in order:

1. **Goal** — one or two sentences stating what the phase is for
2. **Status** — one of `Done` / `Partial` / `Planned`, plus a one-line summary
3. **Epics** — grouped `- [ ]` / `- [x]` checklists of actionable tasks
4. **Dependencies and order** — what must land before what
5. **Cross-cutting** — testing, security, docs, ops for that phase
6. **References** — relative links to PRD sections, feature specs, contracts, architecture docs

## Status legend (README)

| Marker | Meaning |
|--------|---------|
| `- [x]` | Implemented and covered by tests or verified manually |
| `- [ ]` | Not started or incomplete |
| `Done` | All epics in the phase complete |
| `Partial` | Some epics complete, others outstanding |
| `Planned` | Nothing implemented yet |

## Ground truth for initial status

Derived from the current tree (not aspirational):

- **HTTP surface:** 109 contract routes in `internal/adapters/inbound/routes/api.go`; 19 wired, the rest return 501 via `notImplementedHandler`.
- **Implemented operations:** `health.live/ready/version`, `auth.login/refresh/logout`, `auth.password.forgot/reset/reset_token_validity`, `me.get`, `me.password.update`, `admin.users.list`, `admin.posts.create/publish`, `public.posts.list/get`, `public.categories.list/get`, `public.tags.list`.
- **Migrations:** `00001`–`00004` create users, roles, permissions, user_roles, role_permissions, posts, categories, tags, post_tags, comments, audit_logs, outbox_events, refresh_sessions, password_reset_tokens, casbin_rules.
- **Platform:** multi-dialect DB (`postgres`/`mysql`/`sqlserver`) + Cloud SQL connector; Redis; JWT; Casbin enforcer; multi-broker Watermill messaging with `app worker` scaffold and Compose `messaging` profile.
- **Not started:** media/S3, comments endpoints and moderation, SSE streams, analytics ingest/dashboards, settings, impersonation, post versioning, post SEO endpoints, newsletter, profile/privacy/avatar, outbox publisher and real consumers, scheduler, real mailer.

`comments` and `outbox_events` tables exist but have no service or HTTP layer — the backlog records those as schema-only.

## Conventions

- Relative Markdown links only, per [relative-link-usage-rules.md](../../guides/relative-link-usage-rules.md)
- Task lines are imperative and verifiable ("Add `POST /api/v1/admin/media` handler backed by S3 adapter")
- Reference operation IDs and file paths so tasks are unambiguous
- No emoji

## Success criteria

1. Six phase files plus a README index exist under `docs/tasks/`
2. Statuses match the current tree, including 501-stub reality
3. Every phase links to its authoritative specs with working relative links
4. Root README and the agent docs map cross-reference the backlog; no separate `docs/product/roadmap.md`
5. `(relative-link check removed with scripts/)` passes

## Outcome

All seven files written. Every one of the 109 contract operation IDs appears exactly once
across the phase files; the 19 wired operations are the only ones ticked. Link validation
passes with zero violations.
