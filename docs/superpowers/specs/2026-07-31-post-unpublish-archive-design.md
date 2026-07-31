# Post Unpublish + Archive Design

Date: 2026-07-31
Status: implemented (user waived interactive approval)
Scope: Admin post unpublish and archive transitions.

## Goal

Let staff take published posts offline (`unpublish` → `draft`) and move posts into `archived`, completing the Phase 2 “Post unpublish / archive transition” backlog item.

## Decisions

| Topic | Choice |
| --- | --- |
| Unpublish | `published` → `draft`; keep `PublishedAt` (history); bump `Version` |
| Archive | any live status → `archived`; keep `PublishedAt`; bump `Version` |
| Invalid transition | Unpublish from non-published → `400 validation_error` |
| Soft-deleted | Treat as not found (same as Publish/Update) |
| Reopen | Existing `Publish` already moves archived/draft → published |
| Permissions | Unpublish reuses `post.publish`; archive uses new `post.archive` (seed admin/editor) |
| Endpoints | `POST .../unpublish`, `POST .../archive` (mirror publish) |
| Events/outbox | Deferred (Publish also does not emit yet) |

## Non-goals

- Soft delete / restore
- Nested revisions / outbox events
- Scheduled publish changes
- Changing public list filters (already published-only)

## Contract

| operationId | Method / path | Gate |
| --- | --- | --- |
| `admin.posts.unpublish` | `POST /api/v1/admin/posts/{id}/unpublish` | `post.publish` |
| `admin.posts.archive` | `POST /api/v1/admin/posts/{id}/archive` | `post.archive` |

Responses: `EnvelopePostResponse` on 200; `400` / `403` / `404`.

## Service

```go
Unpublish(ctx, id) (Post, error) // requires StatusPublished
Archive(ctx, id) (Post, error)   // any non-deleted status → archived (idempotent if already archived)
```

## Testing

- Service: unpublish happy path + wrong status; archive; soft-deleted → not found
- HTTP: handlers map errors; router role/permission gates for archive
