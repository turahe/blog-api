# Admin Categories CRUD + Reorder Design

Date: 2026-07-31
Status: approved (user waived interactive approval; proceed to implement)
Scope: Admin category create, update, delete, and sibling reorder. Public list/get keep working.

## Goal

Give staff a curated category catalog: create, rename/reparent, delete, and reorder siblings. Posts keep a single optional `category_id` (unchanged). Public category reads continue through `CategoryService`.

## Decisions

| Topic | Choice |
| --- | --- |
| Slice | Admin create / update / delete / reorder (Phase 2 open item) |
| Nesting | Optional `parent_id` only — no nested-set (`lft`/`rgt`/`depth`) in this slice |
| Sort | New `sort_order integer NOT NULL DEFAULT 0`; list order `sort_order ASC, name ASC` |
| Reorder | Sibling batch under a parent (or roots when `parent_id` null): set `sort_order` = index |
| Delete | Hard delete; **reject** if category has children (`409 category_has_children`); posts detach via existing `ON DELETE SET NULL` |
| Cover image | Optional `image_id` on create/update (nullable clear on update); no media readiness checks beyond UUID parse |
| Permissions | Seed `category.create` / `category.update` / `category.delete` for admin + editor; reorder uses `category.update` |
| Architecture | Expand existing `internal/core/category` (mirror tags admin) |

## Non-goals

- Full nested-set insert/move/delete and tree rebuild
- Category merge / soft delete / aliases
- `admin.categories.list` (public list suffices)
- Redis cache invalidation (caching epic still open)
- Public `include=image` hydration (already contracted; leave as follow-up if unimplemented)

## Architecture

```text
HTTP handlers
  → CategoryService (list, get, create, update, delete, reorder)
  → CategoryRepository (GORM)
```

### Module: `internal/core/category`

| Layer | Responsibility |
| --- | --- |
| `domain` | `Category` (+ `SortOrder`, `ImageID`); errors `ErrNotFound`, `ErrConflict`, `ErrHasChildren` |
| `ports` | Repository + Service interfaces |
| `service` | Validation, slugify, parent checks, cycle guard, reorder orchestration |

Inject `IDGenerator` + `Clock` like tags.

### Persistence

Migration `00007_category_sort_order.sql`:

```sql
ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS categories_parent_sort_idx
    ON categories (parent_id, sort_order);
```

`image_id` already exists from `00006`. Map it on the GORM model + domain.

### HTTP wiring

| operationId | Method / path | Gate |
| --- | --- | --- |
| `admin.categories.create` | `POST /api/v1/admin/categories` | `category.create` |
| `admin.categories.update` | `PATCH /api/v1/admin/categories/{id}` | `category.update` |
| `admin.categories.delete` | `DELETE /api/v1/admin/categories/{id}` | `category.delete` |
| `admin.categories.reorder` | `POST /api/v1/admin/categories/reorder` | `category.update` |

Fallback roles when RBAC unset: `admin`, `editor`.

## Contract

### Create body

```yaml
CategoryCreateRequest:
  type: object
  required: [name]
  properties:
    name: { type: string, minLength: 1, maxLength: 120 }
    slug: { type: string }
    description: { type: string, maxLength: 2000 }
    parent_id: { type: string, format: uuid, nullable: true }
    image_id: { type: string, format: uuid, nullable: true }
```

### Update body

At least one of `name`, `slug`, `description`, `parent_id`, `image_id`.
`parent_id` / `image_id`: omit = unchanged; JSON `null` = clear.

### Reorder body

```yaml
CategoryReorderRequest:
  type: object
  required: [ordered_ids]
  properties:
    parent_id: { type: string, format: uuid, nullable: true }
    ordered_ids:
      type: array
      minItems: 1
      items: { type: string, format: uuid }
```

All `ordered_ids` must be exactly the set of siblings under that parent (no extras, no missing). Response: `200` empty ok or list of updated categories — prefer `EmptyOk` for simplicity.

Responses reuse `EnvelopeCategoryResponse` for create/update. Errors: `400`, `401`/`403`, `404`, `409`.

## Service rules

1. **Slug** — same kebab-case rules as tags; default from name; uniqueness → `ErrConflict`.
2. **Parent** — if set, must exist; must not be self; must not create a cycle (walk ancestors).
3. **Create sort_order** — append: `max(sibling sort_order)+1` (or `0` if none).
4. **Delete** — `CountChildren > 0` → `ErrHasChildren`; else hard delete.
5. **Reorder** — verify every id belongs to the given parent (null parent = roots); assign `0..n-1`.

## Testing

- Service unit tests with fake repo (create/update/delete/reorder/parent/cycle/slug).
- HTTP handler tests with `categoryAPI` fake.
- Router role-gate test (author → 403).

## Docs / backlog

- Tick admin category create/update/delete/reorder in `docs/tasks/phase-2-content-core.md`.
- Leave nesting/nested-set checkbox open.
- Update `docs/backend/api.md` and `CHANGELOG.md` when wired.

## Out of scope follow-ups

- Nested-set columns and move semantics
- Cache invalidation on category mutation
- Admin list with filters
