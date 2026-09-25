# Phase 3 — Collaboration and Moderation

## Goal

Open the platform to reader participation: comments, a moderation workflow, richer audit
logging, and notification hooks.
Index: [README.md](./README.md).

## Status

**Partial** — the comment core, repository, and all 8 public and self-service comment
operations are wired (migration `00009_comments.sql`), with ownership checks, Redis rate
limits, sanitized `content_html` (migration `00017_comment_content_html.sql`), and per-post
comment policies (migration `00018_post_comment_policy.sql`). Guest comments can require a
Cloudflare Turnstile token (`TURNSTILE_SECRET_KEY`). The 6 admin moderation operations are wired behind `comment.moderate` /
`comment.delete` with an append-only moderation log (migration `00010_comment_moderation.sql`).
Audit logging is wired (migration `00016_audit_log_columns.sql`, async writer, activity
endpoints, retention pruning). The notification inbox is wired (migration
`00019_notifications.sql`): replies, moderation outcomes, and publications create in-app
notices, listed and marked read under `/me/notifications`. The SSE stream still returns `501`.

## Epic: comment model and service

- [x] `comments` table with post, author, parent, and status columns
- [x] Comment domain and ports under `internal/core/comment`
- [x] Comment service covering create, edit window, soft delete, and threading depth limits
- [x] Comment repository in `internal/adapters/outbound/persistence/comment_repository.go`
- [x] Migration for upvotes and flag counters (`comment_upvotes`, `comment_flags`, cached
      `upvote_count` / `flag_count`)
- [x] Reconcile the `comments` columns against [ERD.md](../backend/ERD.md): `00009` renames
      `user_id`/`body` to `author_id`/`content` and adds guest identity, `ip_hash`, `user_agent`,
      `depth` (0–5, DB-checked), `edited_at`, `deleted_by`, and the `flagged` status. `reply_count`
      is computed on read. Moderation and spam columns are tracked under the moderation epic.
- [x] Render `content` to sanitized `content_html` (allow-list markdown; goldmark plus
      bluemonday, migration `00017_comment_content_html.sql`); clients must still escape `content`

## Epic: public and self comment endpoints

- [x] `public.posts.comments.list` — `GET /api/v1/posts/{id}/comments` (roots, or `?parent_id=` replies)
- [x] `public.posts.comments.create` — `POST /api/v1/posts/{id}/comments` (optional auth; guests via `COMMENTS_GUEST_ENABLED`)
- [x] `public.comments.get` — `GET /api/v1/comments/{id}` (with first page of replies)
- [x] `public.comments.flag` — `POST /api/v1/comments/{id}/flag` (deduped per user or IP+UA hash)
- [x] `self.comments.list` — `GET /api/v1/me/comments`
- [x] `self.comments.patch` — `PATCH /api/v1/comments/{id}` (inside `COMMENTS_EDIT_WINDOW`)
- [x] `self.comments.delete` — `DELETE /api/v1/comments/{id}` (soft delete, placeholder kept)
- [x] `self.comments.upvote` — `POST /api/v1/comments/{id}/upvote` (toggle)
- [x] Ownership checks so `self.*` operations cannot touch another user's comment
- [x] Rate limiting on create, flag, and upvote (`COMMENTS_CREATE_PER_MINUTE`, `COMMENTS_ACTIONS_PER_MINUTE`)
- [x] Captcha (`turnstile_response`) on guest create, enabled by `TURNSTILE_SECRET_KEY`
      (Cloudflare Turnstile; `400 comments.spam.challenge_invalid`, `503` when unreachable)
- [x] Per-post comment policy (`posts.comment_policy`: open, authenticated, read_only, disabled;
      disabled hides existing comments; migration `00018_post_comment_policy.sql`)

## Epic: moderation workflow

- [x] `admin.comments.list` — `GET /api/v1/admin/comments` with status and post filters
      (defaults to the pending + flagged queue, oldest first)
- [x] `admin.comments.get` — `GET /api/v1/admin/comments/{id}` (identity fields, flags, moderation log)
- [x] `admin.comments.moderate` — `POST /api/v1/admin/comments/{id}/moderate`
      (`approve` / `reject` / `spam` / `restore`; `409 comment.invalid_transition` otherwise)
- [x] `admin.comments.bulk_moderate` — `POST /api/v1/admin/comments/bulk-moderate`
      (up to 500 ids, all-or-nothing; blocking ids returned in `error.details.ids`)
- [x] `admin.comments.delete` — `DELETE /api/v1/admin/comments/{id}` (removes the row, or scrubs
      content and author when replies exist so the `parent_id` cascade cannot delete them)
- [x] `admin.comments.stats` — `GET /api/v1/admin/comments/stats`
- [x] Casbin permissions for `comment.moderate` (admin, editor, moderator) and `comment.delete`
      (admin, moderator), seeded into `casbin_rules`; `00010` drops the unused `comments.moderate`
- [x] Explicit state machine for pending / approved / flagged / rejected / spam with allowed
      transitions (`comment/domain/moderation.go`); approving a flagged comment resets
      `flag_count`, and updates are conditional on the status read so concurrent moderators
      get `409` instead of overwriting each other
- [x] Moderation columns (`moderated_by`, `moderation_reason`, `moderated_at`) and an append-only
      `comment_moderation_log` with before/after snapshots and `notify_author`
- [x] Honeypot field marks bot submissions as `spam` without telling the client
- [x] Optional spam heuristics or third-party check (`spam_engine`, `spam_score`, `spam_verdict`),
      or a documented decision to defer — **deferred**: the honeypot, rate limits, and flag
      escalation feed the moderation queue; no `spam_*` columns until an engine is chosen, so
      the `spam_score` / `flagged_reason` list filters from the spec are not offered yet

Spec: [comments-and-moderation.md](../features/comments-and-moderation.md)

## Epic: audit logging

- [x] `audit_logs` table
- [x] Audit writer service invoked from admin mutations (`middleware.Audit`: one entry per
      successful admin and self-service mutation, auth events including rejected logins, and
      signed-in public writes; services annotate through `internal/core/audit`)
- [x] Record actor, action, subject type and ID, before/after diff, IP, and user agent
      (migration `00016_audit_log_columns.sql`; diffs for role changes, post status, comment
      moderation, and marketing consent; profile edits record field names only)
- [x] `admin.users.activity.list` — `GET /api/v1/admin/users/{id}/activity` (`user.activity.read_all`)
- [x] `me.activity.list` — `GET /api/v1/me/activity` (user-facing categories; IP reduced to its
      network, user agent to "browser on OS")
- [x] Retention policy and pruning job for audit rows (`AUDIT_RETENTION_DAYS`, default 395;
      `app audit prune` and an hourly prune in `app worker`)
- [x] Ensure audit writes never block the request path on failure (bounded queue and batch
      writer; overflow and insert failures are logged, dropped, and counted in
      `blog_audit_entries_dropped_total`)

## Epic: notification hooks

- [x] Notification domain and storage — see [notification.md](../features/notification.md)
      (`notifications` table with a per-user dedupe key)
- [x] `me.notifications.list` — `GET /api/v1/me/notifications`
- [x] `me.notifications.read` — `POST /api/v1/me/notifications/{id}/read`
- [ ] `me.notifications.stream` — `GET /api/v1/me/notifications/stream` (SSE)
- [x] Emit notifications on comment reply, comment moderation, and post publish
- [ ] SSE connection lifecycle: heartbeat, client disconnect cleanup, and proxy buffering notes
- [ ] Fan-out strategy for multiple API replicas, coordinated with the Phase 4 event bus

Specs: [notification.md](../features/notification.md),
[realtime-notifications-sse.md](../features/realtime-notifications-sse.md)

## Dependencies and order

1. The comment core module blocks every comment endpoint — done.
2. Moderation needs seeded Casbin permissions before admin routes can be guarded.
3. Notifications depend on comment events existing to react to.
4. SSE fan-out across replicas depends on the Phase 4 broker being wired to real handlers.
5. The audit writer should land before moderation, so moderation actions are recorded from day one.

## Cross-cutting

- [x] Bind public and self comment handlers in `routes.Register*` (`routes/comments.go`), annotate
      them, then `make swagger` + `make routes-check`
- [x] Bind admin comment handlers the same way (`routes/admin.go`)
- [ ] Bind notification handlers the same way
- [x] Handler tests for ownership failures (`handlers/comments_test.go`) and comment route auth modes
- [x] Handler tests for moderation authorization failures (`handlers/comments_admin_test.go`)
- [x] Service tests for threading depth and edit-window expiry (`comment/service/service_test.go`)
- [ ] Repository tests against a real database for flag dedupe, upvote toggle, reply counts,
      moderation rollback, and hard-delete scrub (covered today by manual end-to-end smokes only)
- [ ] Abuse-and-spam section added to [overview.md](../security/overview.md)
- [ ] SSE testing approach documented in [strategy.md](../testing/strategy.md)

## References

| Topic | Doc |
| --- | --- |
| API surface | [api.md](../backend/api.md) |
| Data models | [model.md](../backend/model.md) |
| Events | [events.md](../backend/events.md) |
| Notifications | [notification.md](../features/notification.md) |
| Validation rules | [validation.md](../backend/validation.md) |
| Security checklist | [checklist.md](../security/checklist.md) |
