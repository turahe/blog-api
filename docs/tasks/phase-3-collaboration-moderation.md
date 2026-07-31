# Phase 3 — Collaboration and Moderation

## Goal

Open the platform to reader participation: comments, a moderation workflow, richer audit
logging, and notification hooks.
Index: [README.md](./README.md).

## Status

**Planned** — the `comments` and `audit_logs` tables exist from migration `00001`, but there is
no comment core module, repository, or handler. All 13 comment operations and all 3 notification
operations still return `501`.

## Epic: comment model and service

- [x] `comments` table with post, author, parent, and status columns
- [ ] Comment domain and ports under `internal/core/comment`
- [ ] Comment service covering create, edit window, soft delete, and threading depth limits
- [ ] Comment repository in `internal/adapters/outbound/persistence`
- [ ] Migration for upvotes and flag counters if the schema does not already carry them
- [ ] Reconcile the existing `comments` columns against [model.md](../backend/model.md) and
      add a follow-up migration for any gaps

## Epic: public and self comment endpoints

- [ ] `public.posts.comments.list` — `GET /api/v1/posts/{id}/comments`
- [ ] `public.posts.comments.create` — `POST /api/v1/posts/{id}/comments`
- [ ] `public.comments.get` — `GET /api/v1/comments/{id}`
- [ ] `public.comments.flag` — `POST /api/v1/comments/{id}/flag`
- [ ] `self.comments.list` — `GET /api/v1/me/comments`
- [ ] `self.comments.patch` — `PATCH /api/v1/comments/{id}`
- [ ] `self.comments.delete` — `DELETE /api/v1/comments/{id}`
- [ ] `self.comments.upvote` — `POST /api/v1/comments/{id}/upvote`
- [ ] Ownership checks so `self.*` operations cannot touch another user's comment
- [ ] Rate limiting on create, flag, and upvote

## Epic: moderation workflow

- [ ] `admin.comments.list` — `GET /api/v1/admin/comments` with status and post filters
- [ ] `admin.comments.get` — `GET /api/v1/admin/comments/{id}`
- [ ] `admin.comments.moderate` — `POST /api/v1/admin/comments/{id}/moderate`
- [ ] `admin.comments.bulk_moderate` — `POST /api/v1/admin/comments/bulk-moderate`
- [ ] `admin.comments.delete` — `DELETE /api/v1/admin/comments/{id}`
- [ ] `admin.comments.stats` — `GET /api/v1/admin/comments/stats`
- [ ] Casbin permissions for `comment.moderate` and `comment.delete`, seeded into `casbin_rules`
- [ ] Explicit state machine for pending / approved / rejected / spam with allowed transitions
- [ ] Optional spam heuristics or third-party check, or a documented decision to defer

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

1. The comment core module blocks every comment endpoint.
2. Moderation needs seeded Casbin permissions before admin routes can be guarded.
3. Notifications depend on comment events existing to react to.
4. SSE fan-out across replicas depends on the Phase 4 broker being wired to real handlers.
5. The audit writer should land before moderation, so moderation actions are recorded from day one.

## Cross-cutting

- [ ] Update `paths/comments.yaml` and `paths/notifications.yaml` first, then `make routes`
- [ ] Handler tests for ownership and moderation authorization failures
- [ ] Service tests for threading depth and edit-window expiry
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
