# Admin Posts List + Update Design

Date: 2026-07-31
Status: implemented
Scope: Wire `admin.posts.list` and add `admin.posts.update` (PATCH) with role-aware ownership.

## Goal

Let authenticated staff list posts in the admin API (with filters and pagination) and edit post content fields without publishing, archiving, or creating revision snapshots.

## Decisions

| Topic | Choice |
| --- | --- |
| Slice | List + PATCH update only |
| List visibility | Authors: own posts only; `admin` / `editor` roles: all posts |
| Update ownership | Same as list |
| Permissions | List → `post.read`; update → `post.update` (plus ownership) |
| Revisions | Deferred — bump `posts.version` / `updated_at` only |
| Status changes | Out of scope (keep `admin.posts.publish`) |
| Architecture | Extend existing `internal/core/post` service + GORM repo + HTTP handlers |

## Non-goals

- Unpublish / archive transitions
- Soft delete / restore
- `post_revisions` table or snapshotting
- SEO / revisions endpoints
- Tag attach/detach on update (unless already present; do not add in this slice)
- Changing author_id

## Contract

### Existing — extend `admin.posts.list`

`GET /api/v1/admin/posts`

Keep: `page`, `per_page`, `status`.

Add query params:

| Param | Type | Notes |
| --- | --- | --- |
| `author_id` | uuid | Optional; ignored for author-scoped callers (forced to self) |
| `category_id` | uuid | Optional |
| `q` | string | Optional; match title/slug (ILIKE) |

Response: existing `EnvelopePostList` with `meta.page`, `meta.per_page`, `meta.total`.

Soft-deleted rows (`deleted_at IS NOT NULL`) are excluded.

Status filter values must align with persisted statuses: `draft`, `scheduled`, `published`, `archived`.  
(Contract currently lists `review` — replace with `scheduled` to match migration/`postdomain.Status`.)

### New — `admin.posts.update`

`PATCH /api/v1/admin/posts/{id}`

- `operationId`: `admin.posts.update`
- Auth: required (admin group)
- Permission: `post.update` + ownership rule
- Request body (all optional; at least one required):

```yaml
PostUpdateRequest:
  type: object
  minProperties: 1
  properties:
    title: { type: string, minLength: 1, maxLength: 200 }
    slug: { type: string }
    excerpt: { type: string }
    content: { type: string }
    category_id: { type: string, format: uuid, nullable: true }
```

- Responses: `200` `EnvelopePostResponse`; `400` validation; `401`/`403`; `404` not found (missing or soft-deleted); `409` slug conflict

## Authorization

```text
caller has required permission (post.read | post.update)
  │
  ├─ role in {admin, editor} → unrestricted scope
  └─ otherwise → author_id = caller user id
```

- Author calling list with another `author_id` → ignore client value; force self.
- Author updating another user's post → `403` `forbidden` (or `404` to avoid existence leak — prefer **404** for non-owned IDs to match privacy posture on admin resources).
- Missing permission → `403` via existing `requirePermission`.

Role names come from the existing role lookup used by HTTP (`ListRoleNames`); do not invent a new RBAC permission for “list all posts”.

## Domain / service

### ListAdmin

```go
type AdminListFilter struct {
  Page, PerPage int
  Status        string // optional
  AuthorID      *uuid.UUID // optional; may be forced by scope
  CategoryID    *uuid.UUID
  Query         string
  ScopeAuthorID *uuid.UUID // if non-nil, restrict to this author
}
```

`PostService.ListAdmin(ctx, filter) (ListResult, error)`  
Normalizes page/per_page like public list; applies scope before repo call.

### Update

```go
type UpdateInput struct {
  Title      *string
  Slug       *string
  Excerpt    *string
  Content    *string
  CategoryID **uuid.UUID // nil = omit; non-nil pointer to nil = clear; pointer to uuid = set
}
```

`PostService.Update(ctx, id, actorID uuid.UUID, unrestricted bool, in UpdateInput) (Post, error)`

Rules:

1. Load by ID; soft-deleted → `ErrNotFound`
2. If `!unrestricted` and `post.AuthorID != actorID` → `ErrNotFound` (privacy)
3. Apply provided fields; validate title non-empty if set; slug via existing slug pattern if set
4. Slug uniqueness: if changing slug and another non-deleted post has it → `ErrConflict` (new sentinel) or wrap `ErrValidation` with stable message — prefer **`ErrConflict`** mapped to HTTP 409
5. `Version++`, `UpdatedAt = now`
6. Persist via `Repository.Update`

## Persistence

Extend `PostRepository`:

- `ListAdmin(ctx, filter) (ListResult, error)` — filters + count + page
- Reuse `GetByID` / `Update` (already maps `cover_image_media_id`)

No new migration.

## HTTP

| Operation | Handler wiring |
| --- | --- |
| `admin.posts.list` | `requirePermission(..., "post.read")` then list handler |
| `admin.posts.update` | `requirePermission(..., "post.update")` then update handler |

Handlers resolve `unrestricted` via role lookup (`admin`/`editor`). Pass actor from `currentUserID`.

Envelope errors:

| Case | HTTP | code |
| --- | --- | --- |
| validation | 400 | `validation_error` |
| not found / not owned | 404 | `not_found` |
| slug conflict | 409 | `conflict` |
| missing auth | 401 | `unauthorized` |
| missing permission | 403 | `forbidden` |

## Testing

- Service: author scope forces author filter; unrestricted list; update own OK; update other as author → not found; slug conflict; version increments
- HTTP: list 200 + meta; update 200; validation 400; conflict 409 (fake service)

## Docs

- Tick list + update in [phase-2-content-core.md](../../tasks/phase-2-content-core.md)
- Note in [api.md](../../backend/api.md) briefly if needed
- [CHANGELOG.md](../../../CHANGELOG.md) entry

## Open follow-ups (later)

- Unpublish / archive
- Soft delete / restore
- Revision snapshots on update
- Tag attach on create/update
