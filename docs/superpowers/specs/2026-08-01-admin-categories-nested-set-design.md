# Admin Categories + Nested Set Design

Date: 2026-08-01
Status: implemented
Scope: Admin category create/update/delete/move/list, nested-set columns via full-tree rebuild, optional `image_id` storage without media expansion.

## Goal

Give staff a curated category tree (create, rename, delete, reparent/reorder) backed by nested-set columns, while public category list/get keep working and expose nest metadata plus `image_id`.

## Decisions

| Topic | Choice |
| --- | --- |
| Slice | Admin category CRUD + move; nested-set migration |
| Nest strategy | Adjacency (`parent_id`) is source of truth; rebuild all `lft`/`rgt`/`depth`/`sort_order` after structural writes |
| Delete | Block if any posts reference the category **or** it has children → `409 category_in_use` |
| Cover | Store/return `image_id` only; no `image` object / `?include=image` |
| Reorder / reparent | Single `POST .../move` with `parent_id` + optional `before_id` |
| Metadata update | `PATCH` for name/slug/description/image_id only; reparent via move |
| Auth | Seed `category.create` / `category.update` / `category.delete`; admin/editor gate like tags |
| Architecture | Extend hexagonal `internal/core/category`; rebuild algorithm in service; GORM repo for adjacency + bulk bounds write |

## Non-goals

- Media asset expansion on category responses
- Soft-delete categories
- Incremental gap-shift nested-set updates
- Category merge
- Redis caching for category reads
- Changing post `category_id` assignment semantics
- Nested-set for comments/media in this slice

## Architecture

```text
HTTP handlers
  → CategoryService (list, get, create, update, delete, move)
  → CategoryRepository (adjacency CRUD + CountPosts/CountChildren + ReplaceTreeBounds)
  → Migration adds lft/rgt/depth/sort_order; image_id already exists (00006)
```

### Module: `internal/core/category`

| Layer | Responsibility |
| --- | --- |
| `domain` | `Category` entity including nest + `ImageID`; errors `ErrNotFound`, `ErrConflict`, `ErrInUse`, validation error |
| `ports` | Repository + Service interfaces |
| `service` | Validation, slugify, cycle checks, DFS rebuild, orchestration |

### Rebuild algorithm

1. Load all categories (adjacency).
2. Group children by `parent_id`; order siblings by current `sort_order`, then `name`, then `id`.
3. DFS from roots (`parent_id` null): assign `depth`, consecutive `lft`/`rgt`, rewrite sibling `sort_order` to `0..n-1`.
4. Persist via `ReplaceTreeBounds` in the **same transaction** as the adjacency mutation (create/move/delete).

Metadata-only `Update` does not rebuild unless later extended; nest fields remain unchanged.

### Persistence

- Migration (next goose number after `00006`): add `lft`, `rgt`, `depth`, `sort_order` as `NOT NULL` integers; backfill existing rows with a valid flat/root nested-set assignment in the migration `Up`.
- Map `image_id` on `CategoryModel` (column already present).
- Repository expands beyond `List` / `GetBySlug`: `GetByID`, `Create`, `Update`, `Delete`, `SlugTaken`, `CountPosts`, `CountChildren`, `ReplaceTreeBounds`, list ordered by `lft`.

### HTTP wiring

- Keep `public.categories.list` / `public.categories.get` on `CategoryService`.
- Wire admin handlers with permission keys when Roles present; else `admin`/`editor` role gate.
- Seed permissions for admin + editor roles.

## Contract

### Admin categories

| operationId | Method / path |
| --- | --- |
| `admin.categories.list` | `GET /api/v1/admin/categories` |
| `admin.categories.create` | `POST /api/v1/admin/categories` |
| `admin.categories.update` | `PATCH /api/v1/admin/categories/{id}` |
| `admin.categories.delete` | `DELETE /api/v1/admin/categories/{id}` |
| `admin.categories.move` | `POST /api/v1/admin/categories/{id}/move` |

**Create body**

```yaml
CategoryCreateRequest:
  type: object
  required: [name]
  properties:
    name: { type: string, minLength: 1, maxLength: 128 }
    slug: { type: string }
    description: { type: string, nullable: true }
    parent_id: { type: string, format: uuid, nullable: true }
    image_id: { type: string, format: uuid, nullable: true }
    before_id: { type: string, format: uuid, nullable: true }
```

**Update body** — at least one of `name`, `slug`, `description`, `image_id`. Reject `parent_id` on PATCH (`validation_error`); clients use move.

**Move body**

```yaml
CategoryMoveRequest:
  type: object
  required: [parent_id]
  properties:
    parent_id: { type: string, format: uuid, nullable: true }  # null = root
    before_id: { type: string, format: uuid, nullable: true }  # null = append last
```

Responses reuse `Category` / `EnvelopeCategoryResponse` / `EnvelopeCategoryList` in `components/schemas/Categories.yaml`. Delete → `204`. Errors: `400`, `401`/`403`, `404`, `409`.

### Public categories

- Keep `public.categories.list` / `public.categories.get`.
- Populate `lft`, `rgt`, `depth`, `sort_order`, `image_id` on responses.
- List ordered by `lft ASC`.
- Align list `data` to `{ items: [...] }` per `EnvelopeCategoryList` (fix raw-array drift).
- Do not implement `image` expansion or `?include=image` in this slice.

## Domain rules

1. **Name:** trim; reject empty; max 128 runes.
2. **Slug:** optional on create; else kebab-case from name (`^[a-z0-9]+(?:-[a-z0-9]+)*$`); uniqueness → `409 conflict`.
3. **Parent:** on create/move, `parent_id` must exist when non-null.
4. **before_id:** when set, must exist and share the target parent; insert immediately before that sibling.
5. **Move cycle:** reject moving a node under itself or any descendant.
6. **Delete:** `CountChildren > 0` or `CountPosts > 0` → `409 category_in_use`; else hard delete + rebuild.
7. **image_id:** accept nullable UUID; no media readiness validation in this slice.

## Error mapping

| Case | Code | HTTP |
| --- | --- | --- |
| Validation (empty name, bad slug, bad parent/before_id, cycle, PATCH reparent) | `validation_error` | 400 |
| Missing category | `not_found` | 404 |
| Slug conflict | `conflict` | 409 |
| Delete blocked (posts or children) | `category_in_use` | 409 |

## Testing

- Category service unit tests (fake repo): create under parent + before_id; move reparent/reorder; cycle reject; delete blocked/allowed; rebuild produces contiguous nested-set; slug conflict.
- HTTP tests: create `201`, delete `409`, move into subtree `400`, list envelope `{ items }`, auth gate on admin ops.
- Migration: existing categories backfilled with valid `lft`/`rgt`/`depth`/`sort_order`.

## Docs / backlog

- Tick Phase 2: admin category create/update/delete/reorder; note nesting delivered via nested-set rebuild (leave separate “confirm nesting” item resolved or retitled).
- Update [api.md](../../backend/api.md) and [CHANGELOG.md](../../../CHANGELOG.md).
- Leave Redis cache and media `include=image` unchecked.

## Implementation order (high level)

1. Migration + domain/ports fields
2. Service rebuild + CRUD/move (TDD)
3. Expand GORM category repository
4. OpenAPI paths/schemas + `make routes`
5. HTTP handlers + seed permissions + bootstrap/router
6. Fix public list envelope; docs and backlog ticks
