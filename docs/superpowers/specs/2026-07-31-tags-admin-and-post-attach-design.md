# Tags Admin + Post Attach Design

Date: 2026-07-31
Status: approved
Scope: Admin tag create/update/merge/delete, and create-or-link attach on post create/update.

## Goal

Give staff a curated tag catalog (create, rename, merge, delete) and let post writers attach tags by name, creating missing tags automatically. Public tag list remains available.

## Decisions

| Topic | Choice |
| --- | --- |
| Slice | Admin tags + post attach/detach (full open Phase 2 tag items) |
| Attach input | `tags: string[]` names (create-or-link); replace semantics |
| Merge | Reassign all `post_tags` from source → target, then hard-delete source |
| Delete | Reject while any posts still use the tag (409); no soft delete |
| Who may create | Anyone with `post.create` / `post.update` via post payloads; dedicated admin create for curation |
| Architecture | Hexagonal `internal/core/tag` + post service linker (`WithTags`), matching media |

## Non-goals

- Soft-delete / alias / redirect rows for tags
- Public `GET /tags/{slug}` (unless already needed; not in this slice)
- Redis caching for tag reads
- Changing media asset string-array `tags` (unrelated freeform labels)
- Category admin CRUD
- Tag permissions beyond role gates (`tag.*` seed optional; default admin/editor for catalog ops)

## Architecture

```text
HTTP handlers
  → TagService (list, create, update, merge, delete, ResolveOrCreate)
  → PostService.WithTags(TagLinker) on create/update when tags present
  → TagRepository / PostTag writes (GORM)
```

### Module: `internal/core/tag`

| Layer | Responsibility |
| --- | --- |
| `domain` | `Tag` entity; errors `ErrNotFound`, `ErrConflict`, `ErrInUse` |
| `ports` | Repository + Service interfaces |
| `service` | Validation, slugify, merge transaction orchestration, resolve-or-create |

### Post integration

- `PostService` optional dependency: resolve names → IDs and `ReplacePostTags(postID, tagIDs)`.
- Create/update accept optional `Tags *[]string` (pointer so omit vs empty is distinguishable).
- Omit → leave `post_tags` unchanged; empty slice → clear; non-empty → replace after resolve-or-create.
- Ownership and `post.create` / `post.update` rules unchanged from admin posts design.

### Persistence

- Existing tables `tags` and `post_tags` (migration `00001`) — **no new migration**.
- Expand `TagRepository`: Create, Update, GetByID, GetBySlug, List, SlugTaken, CountPosts, ReassignPosts, Delete, ReplacePostTags.
- `ON DELETE CASCADE` on `post_tags.tag_id` remains a safety net; application delete still refuses when `CountPosts > 0`.

### HTTP wiring

- Move `public.tags.list` off direct repo usage onto `TagService.List`.
- Wire admin tag handlers in router with `admin`/`editor` roles (or `tag.create` / `tag.update` / `tag.delete` if seeded — prefer role gate consistent with other catalog ops until permissions exist).

## Contract

### Admin tags

| operationId | Method / path |
| --- | --- |
| `admin.tags.create` | `POST /api/v1/admin/tags` |
| `admin.tags.update` | `PATCH /api/v1/admin/tags/{id}` |
| `admin.tags.merge` | `POST /api/v1/admin/tags/{id}/merge` |
| `admin.tags.delete` | `DELETE /api/v1/admin/tags/{id}` |

**Create body**

```yaml
TagCreateRequest:
  type: object
  required: [name]
  properties:
    name: { type: string, minLength: 1, maxLength: 64 }
    slug: { type: string }  # optional; default from name
```

**Update body** — at least one of `name`, `slug`.

**Merge body**

```yaml
TagMergeRequest:
  type: object
  required: [into_id]
  properties:
    into_id: { type: string, format: uuid }
```

Path `{id}` is the **source** tag (merged away). `into_id` is the **target** that remains.

Responses use existing `Tag` and `EnvelopeTagList` in `components/schemas/Categories.yaml`. Add `EnvelopeTagResponse` (single-tag envelope) for create/update/merge. Errors: `400`, `401`/`403`, `404`, `409`.

### Posts — replace `tag_ids` with names

OpenAPI today exposes `tag_ids: uuid[]` on create and is unused. Replace with:

```yaml
tags:
  type: array
  items:
    type: string
    minLength: 1
    maxLength: 64
```

Apply to `PostCreateRequest` and `PostUpdateRequest` (update: omit = unchanged; `[]` = clear).

Post **create/update** responses must include resolved `tags: Tag[]`. Public/admin **list** responses may omit `tags` in this slice (avoid N+1); public get may omit too unless already loading joins.

## Domain rules

1. **Name:** trim; reject empty; max 64 runes.
2. **Slug:** optional on create; else kebab-case from name (`^[a-z0-9]+(?:-[a-z0-9]+)*$`); uniqueness → 409.
3. **ResolveOrCreate:** trim, drop empties, dedupe by normalized slug; find by slug else create; preserve first-seen display name casing for new rows.
4. **Merge:** source ≠ target; both exist; in one DB transaction: insert missing `(post_id, target)` from source links, delete source links, delete source tag.
5. **Delete:** `CountPosts > 0` → 409 `tag_in_use`; else hard delete.

## Error mapping

| Case | Code | HTTP |
| --- | --- | --- |
| Validation | `validation_error` | 400 |
| Missing tag / post | `not_found` | 404 |
| Slug conflict | `conflict` | 409 |
| Tag in use | `tag_in_use` | 409 |
| Merge same id / invalid | `validation_error` or `conflict` | 400/409 |
| Post ownership denial | treat as not found | 404 |

## Testing

- Tag service unit tests: create, rename conflict, merge reassigns and deletes source, delete blocked/allowed, ResolveOrCreate idempotent.
- Post service: tags omit vs replace vs clear; linker invoked only when `Tags != nil`.
- HTTP: admin create/update/merge/delete; post create/update with `tags`; public list still works; 409 on in-use delete.

## Docs / backlog

- Tick Phase 2 items: admin tag create/merge/delete; attach/detach on post create/update.
- Update [api.md](../../backend/api.md) and [CHANGELOG.md](../../../CHANGELOG.md).
- Leave category admin and Redis cache unchecked.

## Implementation order (high level)

1. Tag domain/ports + service (TDD)
2. Expand GORM tag repository
3. OpenAPI paths/schemas + `make routes`
4. HTTP handlers + bootstrap/router
5. Post service/handlers attach
6. Docs and backlog ticks
