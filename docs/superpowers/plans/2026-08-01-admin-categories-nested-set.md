# Admin Categories + Nested Set Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship admin category list/create/update/delete/move with nested-set columns maintained by full-tree rebuild, plus public list/get returning nest fields and `image_id`.

**Architecture:** Extend hexagonal `internal/core/category`. Adjacency (`parent_id`) is source of truth; after each structural write the service DFS-rebuilds `lft`/`rgt`/`depth`/`sort_order` and persists via `ReplaceTreeBounds` in one transaction. OpenAPI adds admin category paths; handlers mirror tags RBAC (`category.*`).

**Tech Stack:** Go 1.26.5, GORM, Gin, goose SQL migrations, Casbin/role gates, OpenAPI + `make routes` / redocly bundle.

## Global Constraints

- Core must not import Gin/GORM/Redis/AWS/Watermill.
- Follow [2026-08-01-admin-categories-nested-set-design.md](../specs/2026-08-01-admin-categories-nested-set-design.md) exactly.
- OpenAPI source of truth: edit `paths/` + `components/` + `openapi.yaml` before handlers; never hand-edit `routes_gen.go`.
- Nested-set strategy is **full rebuild**, not gap-shift.
- Delete with posts or children → `409 category_in_use`; no soft delete.
- `image_id` store/return only — no media expansion / `?include=image`.
- Reparent only via `admin.categories.move`, not PATCH.
- Envelope `{ ok, data, meta, error }`; relative Markdown links only.
- Local infra via Docker Compose (`make infra-up` / `make migrate-up`) unless docs say otherwise.
- AI commits only when the user asks; if committing, include `Co-Authored-By: Composer <noreply@example.com>`.

## File map

| Path | Responsibility |
|------|----------------|
| `internal/platform/migrations/sql/00007_category_nested_set.sql` | Add `lft`/`rgt`/`depth`/`sort_order`; backfill |
| `internal/core/category/domain/category.go` | Fields + errors |
| `internal/core/category/ports/ports.go` | Repository + Service |
| `internal/core/category/service/service.go` | CRUD/move + rebuild |
| `internal/core/category/service/service_test.go` | TDD |
| `internal/adapters/outbound/persistence/category_repository.go` | GORM + tx + bounds |
| `components/schemas/Categories.yaml` | Create/Update/Move request schemas |
| `paths/categories.yaml` | Public + admin paths |
| `openapi.yaml` | Path `$ref`s for admin routes |
| `contracts/openapi.bundle.deref.yaml` | Regenerated |
| `internal/adapters/inbound/http/v1/routes_gen.go` | Regenerated |
| `internal/adapters/inbound/http/handlers_categories.go` | Admin + public category handlers |
| `internal/adapters/inbound/http/handlers_categories_test.go` | HTTP tests |
| `internal/adapters/inbound/http/handlers_content.go` | Remove old category handlers / leave posts |
| `internal/adapters/inbound/http/router.go` | Wire admin ops |
| `internal/bootstrap/app.go` | Construct service with ID/clock |
| `internal/platform/seed/seed.go` | `category.create|update|delete` |
| `docs/tasks/phase-2-content-core.md`, `docs/backend/api.md`, `CHANGELOG.md` | Docs |
| Spec status | Mark `implemented` when done |

---

### Task 1: Nested-set migration

**Files:**
- Create: `internal/platform/migrations/sql/00007_category_nested_set.sql`

**Interfaces:**
- Produces: columns `categories.lft`, `rgt`, `depth`, `sort_order` (all `integer NOT NULL`)
- Consumes: existing `categories` rows with optional `parent_id`

- [ ] **Step 1: Write migration**

```sql
-- +goose Up
ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS lft integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rgt integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS depth integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0;

-- Flat backfill ordered by name (valid nested-set numbers). parent_id may
-- disagree with lft/rgt until CategoryService.RebuildAll runs at bootstrap
-- (Task 5) and rewrites bounds from adjacency.
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY name ASC, id ASC) AS n
    FROM categories
)
UPDATE categories c
SET
    lft = (o.n * 2 - 1),
    rgt = (o.n * 2),
    depth = 0,
    sort_order = (o.n - 1)::integer
FROM ordered o
WHERE c.id = o.id;

-- +goose Down
ALTER TABLE categories
    DROP COLUMN IF EXISTS lft,
    DROP COLUMN IF EXISTS rgt,
    DROP COLUMN IF EXISTS depth,
    DROP COLUMN IF EXISTS sort_order;
```

**Required follow-through:** Task 2 exposes `RebuildAll(ctx)`; Task 5 invokes it at bootstrap so any existing `parent_id` trees become consistent immediately after migrate.

- [ ] **Step 2: Apply locally**

```bash
make migrate-up
```

Expected: migration `00007` applied without error.

- [ ] **Step 3: Commit only if user asks**

---

### Task 2: Category domain, ports, and service (TDD)

**Files:**
- Modify: `internal/core/category/domain/category.go`
- Modify: `internal/core/category/ports/ports.go`
- Modify: `internal/core/category/service/service.go`
- Create: `internal/core/category/service/service_test.go`

**Interfaces:**
- Produces:

```go
// domain
var (
	ErrNotFound = errors.New("category not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("category in use")
)

type Category struct {
	ID          uuid.UUID
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
	ImageID     *uuid.UUID
	Lft         int
	Rgt         int
	Depth       int
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ports.Repository
List(ctx context.Context) ([]categorydomain.Category, error) // ORDER BY lft ASC
GetByID(ctx context.Context, id uuid.UUID) (categorydomain.Category, error)
GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
Create(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error)
Update(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error)
Delete(ctx context.Context, id uuid.UUID) error
SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
CountPosts(ctx context.Context, categoryID uuid.UUID) (int64, error)
CountChildren(ctx context.Context, categoryID uuid.UUID) (int64, error)
ReplaceTreeBounds(ctx context.Context, cats []categorydomain.Category) error
// WithinTx runs fn with a Repository bound to the same DB transaction.
WithinTx(ctx context.Context, fn func(ctx context.Context, r Repository) error) error

// ports.Service / *service.CategoryService
List(ctx) ([]Category, error)
GetBySlug(ctx, slug string) (Category, error)
Create(ctx, in CreateInput) (Category, error)
Update(ctx, id uuid.UUID, in UpdateInput) (Category, error)
Delete(ctx, id uuid.UUID) error
Move(ctx, id uuid.UUID, parentID *uuid.UUID, beforeID *uuid.UUID) (Category, error)
RebuildAll(ctx) error

type CreateInput struct {
	Name        string
	Slug        string // optional; empty → slugify name
	Description *string
	ParentID    *uuid.UUID
	ImageID     *uuid.UUID
	BeforeID    *uuid.UUID
}

type UpdateInput struct {
	Name        *string
	Slug        *string
	Description *string // pointer to string; use sentinel or *string — empty string clears description
	ImageID     **uuid.UUID // optional: prefer separate ImageIDSet bool + *uuid.UUID; see Step 3
}
```

Prefer for update image clear semantics:

```go
type UpdateInput struct {
	Name           *string
	Slug           *string
	Description    *string
	ImageID        *uuid.UUID // when ImageIDProvided
	ImageIDProvided bool
}
```

Service needs `IDGenerator` / `Clock` like tags.

- [ ] **Step 1: Write failing tests** in `service_test.go` with a fake repo covering:

1. `Create` slugifies name; rejects empty name; `SlugTaken` → `ErrConflict`
2. `Create` with `parent_id` + `before_id` places sibling order; rebuild yields contiguous `lft`/`rgt` and correct `depth`
3. `Update` rename conflict → `ErrConflict`; metadata only (parent unchanged)
4. `Move` into own descendant → validation error; successful reparent rebuilds
5. `Delete` when `CountPosts > 0` or `CountChildren > 0` → `ErrInUse`; when 0 → deletes + rebuild
6. `RebuildAll` assigns stable order for a known fixture tree

Minimal fake:

```go
type fakeRepo struct {
	byID   map[uuid.UUID]categorydomain.Category
	posts  map[uuid.UUID]int64
	// WithinTx just calls fn(ctx, f)
}

// implement ports.Repository; ReplaceTreeBounds updates lft/rgt/depth/sort_order on maps
```

Pure rebuild helper under test (same package):

```go
func rebuildBounds(cats []categorydomain.Category) []categorydomain.Category
```

Sibling order: group by parent; sort by `SortOrder`, then `Name`, then `ID`. Roots = `ParentID == nil`.

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test -count=1 ./internal/core/category/service/
```

Expected: FAIL (missing symbols / failing assertions)

- [ ] **Step 3: Implement domain, ports, service**

Slugify — copy tag style (ASCII kebab). Name max **128** runes.

Structural write pattern:

```go
func (s *CategoryService) Create(ctx context.Context, in CreateInput) (categorydomain.Category, error) {
	// validate name/slug/parent/before_id
	return s.mutate(ctx, func(all []categorydomain.Category, now time.Time) ([]categorydomain.Category, categorydomain.Category, error) {
		cat := categorydomain.Category{ /* new id, parent, name, slug, image, timestamps */ }
		all = append(all, cat)
		all, err := placeBefore(all, cat.ID, in.ParentID, in.BeforeID)
		if err != nil {
			return nil, categorydomain.Category{}, err
		}
		all = rebuildBounds(all)
		return all, find(all, cat.ID), nil
	}, persistCreate)
}
```

Simpler concrete approach for implementers:

```go
func (s *CategoryService) withRebuild(ctx context.Context, mutate func(all []categorydomain.Category) ([]categorydomain.Category, uuid.UUID, error)) (categorydomain.Category, error) {
	var out categorydomain.Category
	err := s.repo.WithinTx(ctx, func(ctx context.Context, r ports.Repository) error {
		all, err := r.List(ctx)
		if err != nil {
			return err
		}
		// List may order by lft; for empty nest columns treat as unordered adjacency — still OK
		all, focusID, err := mutate(clone(all))
		if err != nil {
			return err
		}
		all = rebuildBounds(all)
		// Persist adjacency changes implied by mutate:
		// Create path: r.Create(newRow without relying on bounds), then ReplaceTreeBounds(all)
		// Move path: r.Update(node with new ParentID), then ReplaceTreeBounds(all)
		// Delete path: r.Delete(id), then ReplaceTreeBounds(all without id)
		// Implement by having mutate return an op enum OR do Create/Update/Delete inside mutate via r
		out = find(all, focusID)
		return r.ReplaceTreeBounds(ctx, all)
	})
	return out, err
}
```

**Implement `mutate` so Create/Update/Delete against `r` happen inside `WithinTx` before `ReplaceTreeBounds`.** Example Create:

```go
err := s.repo.WithinTx(ctx, func(ctx context.Context, r ports.Repository) error {
	all, err := r.List(ctx)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	cat := categorydomain.Category{
		ID: s.ids.New(), Name: name, Slug: slug, Description: desc,
		ParentID: in.ParentID, ImageID: in.ImageID,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := r.Create(ctx, cat); err != nil {
		return err
	}
	all = append(all, cat)
	all, err = applyBefore(all, cat.ID, in.ParentID, in.BeforeID)
	if err != nil {
		return err
	}
	all = rebuildBounds(all)
	if err := r.ReplaceTreeBounds(ctx, all); err != nil {
		return err
	}
	out = findByID(all, cat.ID)
	return nil
})
```

`applyBefore`: among siblings with same `ParentID`, set `SortOrder` so the moved/created id sits immediately before `beforeID` (or last if nil). Then `rebuildBounds` rewrites final `SortOrder`/`Lft`/`Rgt`/`Depth`.

Cycle check on Move: if `parentID != nil`, load that node; reject if `parentID == id` or `parent.Lft > node.Lft && parent.Rgt < node.Rgt` **after** ensuring bounds exist — or walk ancestors via `ParentID` in the in-memory list (preferred; works before rebuild).

Delete:

```go
if children, _ := r.CountChildren(ctx, id); children > 0 {
	return categorydomain.ErrInUse
}
if posts, _ := r.CountPosts(ctx, id); posts > 0 {
	return categorydomain.ErrInUse
}
```

Validation error: `var ErrValidation = errors.New("validation error")` in service package (like tags).

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test -count=1 ./internal/core/category/...
```

Expected: PASS

- [ ] **Step 5: Commit only if user asks**

---

### Task 3: GORM category repository

**Files:**
- Modify: `internal/adapters/outbound/persistence/category_repository.go`

**Interfaces:**
- Consumes: `categorydomain.Category` + `ports.Repository` from Task 2
- Produces: full GORM implementation including `WithinTx` / `ReplaceTreeBounds`

- [ ] **Step 1: Expand model and methods**

```go
type CategoryModel struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Name        string
	Slug        string
	Description *string
	ParentID    *uuid.UUID `gorm:"type:uuid;column:parent_id"`
	ImageID     *uuid.UUID `gorm:"type:uuid;column:image_id"`
	Lft         int        `gorm:"column:lft"`
	Rgt         int        `gorm:"column:rgt"`
	Depth       int        `gorm:"column:depth"`
	SortOrder   int        `gorm:"column:sort_order"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r *CategoryRepository) List(ctx context.Context) ([]categorydomain.Category, error) {
	var models []CategoryModel
	if err := r.db.WithContext(ctx).Order("lft ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	// map…
}

func (r *CategoryRepository) WithinTx(ctx context.Context, fn func(ctx context.Context, repo ports.Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, &CategoryRepository{db: tx})
	})
}

func (r *CategoryRepository) ReplaceTreeBounds(ctx context.Context, cats []categorydomain.Category) error {
	for _, cat := range cats {
		if err := r.db.WithContext(ctx).Model(&CategoryModel{}).
			Where("id = ?", cat.ID).
			Updates(map[string]any{
				"lft": cat.Lft, "rgt": cat.Rgt, "depth": cat.Depth, "sort_order": cat.SortOrder,
				"parent_id": cat.ParentID, "updated_at": cat.UpdatedAt,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *CategoryRepository) CountPosts(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Table("posts").Where("category_id = ?", categoryID).Count(&n).Error
	return n, err
}

func (r *CategoryRepository) CountChildren(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("parent_id = ?", categoryID).Count(&n).Error
	return n, err
}
```

Map `GetByID` / `GetBySlug` not found → `categorydomain.ErrNotFound`.

`Create` inserts full row (including nest fields from caller; may be zeros until `ReplaceTreeBounds` in same tx).

Import `ports` only in `WithinTx` signature — use `categoryports.Repository` alias to avoid cycle: persistence already imports domain; ports is fine.

- [ ] **Step 2: Compile**

```bash
go test -count=1 ./internal/adapters/outbound/persistence/ ./internal/core/category/...
```

Expected: PASS (persistence may have no tests)

- [ ] **Step 3: Commit only if user asks**

---

### Task 4: OpenAPI + routes

**Files:**
- Modify: `components/schemas/Categories.yaml`
- Modify: `paths/categories.yaml`
- Modify: `openapi.yaml`
- Regenerate: `contracts/openapi.bundle.deref.yaml`, `internal/adapters/inbound/http/v1/routes_gen.go`

**Interfaces:**
- Produces operationIds: `admin.categories.list`, `admin.categories.create`, `admin.categories.update`, `admin.categories.delete`, `admin.categories.move` (keep public list/get)

- [ ] **Step 1: Schemas** — append to `Categories.yaml`:

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
CategoryUpdateRequest:
  type: object
  minProperties: 1
  properties:
    name: { type: string, minLength: 1, maxLength: 128 }
    slug: { type: string }
    description: { type: string, nullable: true }
    image_id: { type: string, format: uuid, nullable: true }
CategoryMoveRequest:
  type: object
  required: [parent_id]
  properties:
    parent_id: { type: string, format: uuid, nullable: true }
    before_id: { type: string, format: uuid, nullable: true }
```

Register these components in `openapi.yaml` if other schemas are listed there (follow existing pattern for Tag* requests).

- [ ] **Step 2: Paths** — extend `paths/categories.yaml` with admin routes (keep public):

```yaml
/api/v1/admin/categories:
  get:
    tags: [Admin]
    summary: List categories (tree order)
    operationId: admin.categories.list
    security: [{ bearerAuth: [] }]
    responses:
      '200':
        description: OK
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeCategoryList
  post:
    tags: [Admin]
    summary: Create category
    operationId: admin.categories.create
    security: [{ bearerAuth: [] }]
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: ../components/schemas/Categories.yaml#/CategoryCreateRequest
    responses:
      '201':
        description: Created
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeCategoryResponse
      '400': { $ref: ../components/responses.yaml#/BadRequestError }
      '409':
        description: Conflict
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
/api/v1/admin/categories/{id}:
  parameters:
  - name: id
    in: path
    required: true
    schema: { type: string, format: uuid }
  patch:
    tags: [Admin]
    operationId: admin.categories.update
    security: [{ bearerAuth: [] }]
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: ../components/schemas/Categories.yaml#/CategoryUpdateRequest
    responses:
      '200':
        description: OK
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeCategoryResponse
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Conflict
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
  delete:
    tags: [Admin]
    operationId: admin.categories.delete
    security: [{ bearerAuth: [] }]
    responses:
      '204': { description: Deleted }
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Category in use
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
/api/v1/admin/categories/{id}/move:
  post:
    tags: [Admin]
    operationId: admin.categories.move
    security: [{ bearerAuth: [] }]
    parameters:
    - name: id
      in: path
      required: true
      schema: { type: string, format: uuid }
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: ../components/schemas/Categories.yaml#/CategoryMoveRequest
    responses:
      '200':
        description: Moved category
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeCategoryResponse
      '400': { $ref: ../components/responses.yaml#/BadRequestError }
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
```

Update `openapi.yaml`:

```yaml
  /api/v1/admin/categories:
    $ref: ./paths/categories.yaml#/~1api~1v1~1admin~1categories
  /api/v1/admin/categories/{id}:
    $ref: ./paths/categories.yaml#/~1api~1v1~1admin~1categories~1{id}
  /api/v1/admin/categories/{id}/move:
    $ref: ./paths/categories.yaml#/~1api~1v1~1admin~1categories~1{id}~1move
```

- [ ] **Step 3: Bundle + routes**

```bash
npx redocly bundle openapi.yaml --dereferenced -o contracts/openapi.bundle.deref.yaml --ext=yaml
make routes
```

Expected: `routes_gen.go` includes the five admin category ops.

- [ ] **Step 4: Commit only if user asks**

---

### Task 5: HTTP handlers, seed, router, bootstrap

**Files:**
- Create: `internal/adapters/inbound/http/handlers_categories.go`
- Create: `internal/adapters/inbound/http/handlers_categories_test.go`
- Modify: `internal/adapters/inbound/http/handlers_content.go` — remove `listCategoriesHandler` / `getCategoryHandler` / `categoryJSON` (moved)
- Modify: `internal/adapters/inbound/http/router.go`
- Modify: `internal/bootstrap/app.go`
- Modify: `internal/platform/seed/seed.go`

**Interfaces:**
- Consumes: `*categoryservice.CategoryService`
- Router keeps `Dependencies.Categories *categoryservice.CategoryService`

- [ ] **Step 1: Handlers**

```go
type categoryAPI interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
	Create(ctx context.Context, in categoryservice.CreateInput) (categorydomain.Category, error)
	Update(ctx context.Context, id uuid.UUID, in categoryservice.UpdateInput) (categorydomain.Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Move(ctx context.Context, id uuid.UUID, parentID, beforeID *uuid.UUID) (categorydomain.Category, error)
}

func categoryJSON(cat categorydomain.Category) gin.H {
	var parent any
	if cat.ParentID != nil {
		parent = cat.ParentID.String()
	}
	var image any
	if cat.ImageID != nil {
		image = cat.ImageID.String()
	}
	return gin.H{
		"id": cat.ID.String(), "name": cat.Name, "slug": cat.Slug,
		"description": cat.Description, "parent_id": parent, "image_id": image,
		"lft": cat.Lft, "rgt": cat.Rgt, "depth": cat.Depth, "sort_order": cat.SortOrder,
		"created_at": cat.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": cat.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func listCategoriesHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := cats.List(c.Request.Context())
		// …
		out := make([]gin.H, 0, len(items))
		for _, item := range items {
			out = append(out, categoryJSON(item))
		}
		success(c, nethttp.StatusOK, gin.H{"items": out})
	}
}
```

Error map:

```go
switch {
case errors.Is(err, categoryservice.ErrValidation):
	failure(..., 400, "validation_error", ...)
case errors.Is(err, categorydomain.ErrNotFound):
	failure(..., 404, "not_found", ...)
case errors.Is(err, categorydomain.ErrConflict):
	failure(..., 409, "conflict", ...)
case errors.Is(err, categorydomain.ErrInUse):
	failure(..., 409, "category_in_use", ...)
}
```

Delete → `c.Status(204)` (match tags/media empty delete if that is the project pattern; tags use 204).

**Move body:** require `parent_id` key present (null allowed):

```go
var raw map[string]json.RawMessage
if err := c.ShouldBindJSON(&raw); err != nil { /* 400 */ }
parentRaw, ok := raw["parent_id"]
if !ok {
	failure(..., 400, "validation_error", "parent_id required")
	return
}
var parentID *uuid.UUID
if string(parentRaw) != "null" {
	var s string
	_ = json.Unmarshal(parentRaw, &s)
	id, err := uuid.Parse(s)
	// …
	parentID = &id
}
```

- [ ] **Step 2: Router** — mirror tags:

```go
if deps.Categories != nil {
	implemented["public.categories.list"] = listCategoriesHandler(deps.Categories)
	implemented["public.categories.get"] = getCategoryHandler(deps.Categories)
	listAdmin := listCategoriesHandler(deps.Categories)
	create := adminCreateCategoryHandler(deps.Categories)
	update := adminUpdateCategoryHandler(deps.Categories)
	del := adminDeleteCategoryHandler(deps.Categories)
	move := adminMoveCategoryHandler(deps.Categories)
	if deps.RBAC != nil {
		implemented["admin.categories.list"] = chain(requirePermission(deps.RBAC, "category.create"), listAdmin) // or category.read if seeded; use category.create for list OR add category.read — prefer requirePermission(..., "post.read")? Spec: category.* — seed category.create/update/delete; for list use category.create OR any of them. Use: requireRoles / gate with permission "category.create" for list is odd. Better seed `category.read` for admin+editor OR reuse gate requirePermission for create on mutations and `category.create` for list. Spec lists only create/update/delete — use update permission for list, or role gate.
```

**Concrete RBAC (match tags list absence — tags have no admin list):**

- `admin.categories.list` → `requirePermission(..., "category.create")` is wrong.
- Seed also `category.read` for admin+editor **or** use role gate `admin`/`editor` for list when no dedicated read perm.

**Decide in implementation:** seed `category.read`, `category.create`, `category.update`, `category.delete`. Spec mentioned three keys; adding `category.read` is a small additive clarification for list. Document in CHANGELOG.

```go
	if deps.RBAC != nil {
		implemented["admin.categories.list"] = chain(requirePermission(deps.RBAC, "category.read"), listAdmin)
		implemented["admin.categories.create"] = chain(requirePermission(deps.RBAC, "category.create"), create)
		implemented["admin.categories.update"] = chain(requirePermission(deps.RBAC, "category.update"), update)
		implemented["admin.categories.delete"] = chain(requirePermission(deps.RBAC, "category.delete"), del)
		implemented["admin.categories.move"] = chain(requirePermission(deps.RBAC, "category.update"), move)
	} else if deps.Roles != nil {
		gate := requireRoles(deps.Roles, "admin", "editor")
		// chain gate for all five
	} else {
		// bare handlers
	}
}
```

- [ ] **Step 3: Bootstrap + seed**

```go
categories := categoryservice.New(categoriesRepo, uuidGen, clock)
_ = categories.RebuildAll(ctx) // after migrate; ignore error if desirable only on empty — prefer log and continue only if Err on empty list is nil
```

Only call `RebuildAll` when appropriate (e.g. always once at startup is OK for small trees).

Seed:

```go
"category.read", "category.create", "category.update", "category.delete",
```

on admin and editor (same as tags).

- [ ] **Step 4: HTTP tests**

Fake `categoryAPI`; assert create `201`, delete `409`/`category_in_use`, move into descendant `400`, list `data.items`, unauthorized without gate when Roles set.

```bash
go test -count=1 ./internal/adapters/inbound/http/ -run Categor
```

Expected: PASS

- [ ] **Step 5: Commit only if user asks**

---

### Task 6: Docs and backlog

**Files:**
- Modify: `docs/tasks/phase-2-content-core.md`
- Modify: `docs/backend/api.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/specs/2026-08-01-admin-categories-nested-set-design.md` — Status: `implemented`

- [ ] **Step 1: Update docs**

Phase 2 ticks:

- [x] Admin category create, update, delete, and reorder endpoints
- [x] Category tree / nesting via nested-set rebuild (retitle/remove the “confirm against PRD” open item)

`api.md`: list admin category ops + note nest fields on public category payloads; list envelope `{ items }`.

CHANGELOG: dated entry for admin categories + migration `00007`.

- [ ] **Step 2: Validate relative links**

```bash
node scripts/docs/validate_relative_links.cjs docs contracts paths README.md
```

Expected: exit 0

- [ ] **Step 3: Full test + build**

```bash
make test && make build
```

Expected: PASS

- [ ] **Step 4: Commit only if user asks**

---

## Spec coverage checklist

| Spec requirement | Task |
| --- | --- |
| Migration `lft`/`rgt`/`depth`/`sort_order` | 1 |
| Domain nest fields + errors | 2 |
| Rebuild-from-adjacency algorithm | 2 |
| Create/Update/Delete/Move service rules | 2 |
| `RebuildAll` for post-migrate consistency | 2+5 |
| GORM repo + `WithinTx` + counts | 3 |
| Admin OpenAPI ops + Move request | 4 |
| Public nest fields + `{ items }` list | 5 |
| RBAC `category.*` seed + router | 5 |
| Delete `409 category_in_use` | 2+5 |
| `image_id` only (no expansion) | 2+5 |
| Docs / phase-2 ticks | 6 |
