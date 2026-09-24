# Phase 3 — Collaboration and Moderation

## Goal

Open the platform to reader participation: comments, a moderation workflow, richer audit
logging, and notification hooks.
Index: [README.md](./README.md).

## Status

**Partial** — the comment core, repository, and all 8 public and self-service comment
operations are wired (migration `00009_comments.sql`), with ownership checks and Redis rate
limits. Moderation, audit writing, and notifications are still open; the 6 admin comment and
3 notification operations return `501`.

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
- [ ] Render `content` to sanitized `content_html` (allow-list markdown); clients must escape
      `content` until then

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
- [ ] Captcha (`turnstile_response`) on guest create when a challenge setting is enabled
- [ ] Per-post comment policy (disabled, read-only, authenticated-only)

## Epic: moderation workflow

- [ ] `admin.comments.list` — `GET /api/v1/admin/comments` with status and post filters
- [ ] `admin.comments.get` — `GET /api/v1/admin/comments/{id}`
- [ ] `admin.comments.moderate` — `POST /api/v1/admin/comments/{id}/moderate`
- [ ] `admin.comments.bulk_moderate` — `POST /api/v1/admin/comments/bulk-moderate`
- [ ] `admin.comments.delete` — `DELETE /api/v1/admin/comments/{id}`
- [ ] `admin.comments.stats` — `GET /api/v1/admin/comments/stats`
- [ ] Casbin permissions for `comment.moderate` and `comment.delete`, seeded into `casbin_rules`
- [ ] Explicit state machine for pending / approved / flagged / rejected / spam with allowed transitions
- [ ] Moderation columns (`moderation_reviewed_by`, `moderation_reason`) and an append-only
      `comment_moderation_log`
- [x] Honeypot field marks bot submissions as `spam` without telling the client
- [ ] Optional spam heuristics or third-party check (`spam_engine`, `spam_score`, `spam_verdict`),
      or a documented decision to defer

Spec: [comments-and-moderation.md](../features/comments-and-moderation.md)

## Epic: audit logging

- [x] `audit_logs` table
- [ ] Audit writer service invoked from admin mutations
- [ ] Record actor, action, subject type and ID, before/after diff, IP, and user agent
- [ ] `admin.users.activity.list` — `GET /api/v1/admin/users/{id}/activity`
- [ ] `me.activity.list` — `GET /api/v1/me/activity`
- [ ] Retention policy and pruning job for audit rows
- [ ] Ensure audit writes never block the request path on failure

## Epic: notification hooks

- [ ] Notification domain and storage — see [notification.md](../features/notification.md)
- [ ] `me.notifications.list` — `GET /api/v1/me/notifications`
- [ ] `me.notifications.read` — `POST /api/v1/me/notifications/{id}/read`
- [ ] `me.notifications.stream` — `GET /api/v1/me/notifications/stream` (SSE)
- [ ] Emit notifications on comment reply, comment moderation, and post publish
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
- [ ] Bind admin comment and notification handlers the same way
- [x] Handler tests for ownership failures (`handlers/comments_test.go`) and comment route auth modes
- [ ] Handler tests for moderation authorization failures
- [x] Service tests for threading depth and edit-window expiry (`comment/service/service_test.go`)
- [ ] Repository tests against a real database for flag dedupe, upvote toggle, and reply counts
      (covered today by a manual end-to-end smoke only)
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
