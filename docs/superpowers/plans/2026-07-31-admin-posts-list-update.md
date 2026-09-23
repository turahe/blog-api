# Admin Posts List + Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `admin.posts.list` with filters/pagination and add `admin.posts.update` (PATCH) with author-vs-admin/editor ownership, without revisions or status transitions.

**Architecture:** Extend `internal/core/post` (domain filter/input types + `PostService.ListAdmin` / `Update`) and GORM `PostRepository.ListAdmin` / slug conflict check. Gin handlers resolve unrestricted scope from roles `admin`|`editor`, enforce Casbin `post.read` / `post.update`, and map errors to the envelope. OpenAPI is updated first for list query params + new PATCH path.

**Tech Stack:** Go 1.26.5, GORM, Gin, existing Casbin RBAC, OpenAPI path/schema YAML + `make routes`.

## Global Constraints

- Core must not import Gin/GORM/Redis/AWS.
- Follow [2026-07-31-admin-posts-list-update-design.md](../specs/2026-07-31-admin-posts-list-update-design.md) exactly.
- OpenAPI source of truth: edit `paths/` + `components/` + `openapi.yaml` `$ref` before handlers; `npx redocly bundle` + `make routes`; never hand-edit `routes_gen.go`.
- Soft-deleted posts excluded; non-owned updates return not-found (privacy).
- No `post_revisions`, unpublish/archive, or tag attach in this plan.
- Status enum in contract: use `draft|scheduled|published|archived` (replace `review`).
- Envelope `{ ok, data, meta, error }`; relative Markdown links only.
- AI commits only when the user asks; if committing, include `Co-Authored-By: Composer <noreply@example.com>`.

## File map

| Path | Responsibility |
|------|----------------|
| `internal/core/post/domain/post.go` | `AdminListFilter`, `UpdateInput` / `OptionalCategoryID`, `ErrConflict` |
| `internal/core/post/ports/ports.go` | Extend Repository + Service interfaces |
| `internal/core/post/service/service.go` | `ListAdmin`, `Update` |
| `internal/core/post/service/service_test.go` | TDD with fake repo |
| `internal/adapters/outbound/persistence/post_repository.go` | `ListAdmin`, `SlugTaken` |
| `paths/posts.yaml` | List filters; PATCH update |
| `components/schemas/posts.yaml` | `PostUpdateRequest` |
| `openapi.yaml` | `$ref` for `/api/v1/admin/posts/{id}` |
| `contracts/openapi.bundle.deref.yaml` | Regenerated via redocly |
| `internal/adapters/inbound/routes/api.go` | Regenerated |
| `internal/adapters/inbound/http/handlers_content.go` | List + update handlers |
| `internal/adapters/inbound/http/handlers_posts_admin_test.go` | HTTP tests with fakes |
| `internal/adapters/inbound/http/router.go` | Wire ops + permissions |
| `docs/tasks/phase-2-content-core.md` | Tick list + update |
| `CHANGELOG.md` | Entry |
| Spec status | Mark implemented when done |

---

### Task 1: Domain types and ports

**Files:**
- Modify: `internal/core/post/domain/post.go`
- Modify: `internal/core/post/ports/ports.go`

**Interfaces:**
- Produces domain:

```go
var ErrConflict = errors.New("conflict")

type AdminListFilter struct {
	Page          int
	PerPage       int
	Status        string
	AuthorID      *uuid.UUID
	CategoryID    *uuid.UUID
	Query         string
	ScopeAuthorID *uuid.UUID // if set, force AuthorID = this
}

// OptionalCategoryID: Present=false means omit; Present=true applies Value (nil clears).
type OptionalCategoryID struct {
	Present bool
	Value   *uuid.UUID
}

type UpdateInput struct {
	Title      *string
	Slug       *string
	Excerpt    *string
	Content    *string
	CategoryID OptionalCategoryID
}
```

- Repository adds: `ListAdmin(ctx, filter) (ListResult, error)`, `SlugTaken(ctx, slug string, excludeID uuid.UUID) (bool, error)`
- Service adds: `ListAdmin`, `Update` signatures from design

- [ ] **Step 1: Add types and interface methods**

Keep existing `ListFilter` / public methods unchanged.

- [ ] **Step 2: Compile**

Run: `go test -count=1 ./internal/core/post/...`

Expected: compile may fail until Task 2/3 implement methods if interfaces require them — implement stubs on `PostService`/`PostRepository` returning `errors.New("not implemented")` only if needed to compile; prefer completing Task 2–3 in order without stubs.

- [ ] **Step 3: Commit only if user asks**

---

### Task 2: Post service ListAdmin + Update (TDD)

**Files:**
- Create: `internal/core/post/service/service_test.go`
- Modify: `internal/core/post/service/service.go`

**Interfaces:**
- Consumes: `ports.Repository` (fake in tests)
- Produces: working `ListAdmin` / `Update`

- [ ] **Step 1: Write failing tests**

Cover at minimum:

1. `ListAdmin` clamps page/per_page; when `ScopeAuthorID` set, effective author filter is scope (ignore conflicting `AuthorID`)
2. `Update` own post as restricted actor → success, `Version` increments
3. `Update` other author's post when `unrestricted=false` → `ErrNotFound`
4. `Update` with invalid slug → `ErrValidation`
5. `Update` when `SlugTaken` true → `ErrConflict`
6. Empty update (no fields) → `ErrValidation`
7. Soft-deleted post → `ErrNotFound`

Use an in-memory fake repo implementing `ports.Repository`.

- [ ] **Step 2: Run — expect fail**

Run: `go test -count=1 ./internal/core/post/service/ -run 'ListAdmin|Update'`

- [ ] **Step 3: Implement**

```go
func (s *PostService) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	if filter.Page < 1 { filter.Page = 1 }
	if filter.PerPage < 1 || filter.PerPage > 100 { filter.PerPage = 20 }
	if filter.ScopeAuthorID != nil {
		filter.AuthorID = filter.ScopeAuthorID
	}
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	filter.Query = strings.TrimSpace(filter.Query)
	return s.repo.ListAdmin(ctx, filter)
}

func (s *PostService) Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error) {
	// load, ownership, apply fields, slugTaken, version++, update
}
```

Export `var ErrConflict = postdomain.ErrConflict` alias in service package for HTTP `errors.Is`, or map `postdomain.ErrConflict` directly in handlers.

- [ ] **Step 4: Run — expect pass**

Run: `go test -count=1 ./internal/core/post/service/`

- [ ] **Step 5: Commit only if user asks**

---

### Task 3: GORM ListAdmin + SlugTaken

**Files:**
- Modify: `internal/adapters/outbound/persistence/post_repository.go`

**Interfaces:**
- Implements `ListAdmin` and `SlugTaken` on `*PostRepository`

- [ ] **Step 1: Implement ListAdmin**

```go
func (r *PostRepository) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	q := r.db.WithContext(ctx).Model(&PostModel{}).Where("deleted_at IS NULL")
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.AuthorID != nil {
		q = q.Where("author_id = ?", *filter.AuthorID)
	}
	if filter.CategoryID != nil {
		q = q.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		q = q.Where("title ILIKE ? OR slug ILIKE ?", like, like)
	}
	// Count, Order created_at DESC, Limit/Offset, mapPost
}
```

- [ ] **Step 2: Implement SlugTaken**

```go
func (r *PostRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&PostModel{}).
		Where("slug = ? AND id <> ? AND deleted_at IS NULL", slug, excludeID).
		Count(&n).Error
	return n > 0, err
}
```

- [ ] **Step 3: Compile**

Run: `go test -count=1 ./internal/adapters/outbound/persistence/ ./internal/core/post/...`

- [ ] **Step 4: Commit only if user asks**

---

### Task 4: OpenAPI + route generation

**Files:**
- Modify: `paths/posts.yaml`
- Modify: `components/schemas/posts.yaml`
- Modify: `openapi.yaml`
- Regenerate: `contracts/openapi.bundle.yaml` (gitignored), `contracts/openapi.bundle.deref.yaml`, `routes_gen.go`

**Interfaces:**
- Produces: `admin.posts.update` in `routes_gen.go` with `GroupAdmin`, `AuthRequired`

- [ ] **Step 1: Fix list status enum + add filters**

In `paths/posts.yaml` under `admin.posts.list` parameters:

- Change status enum `review` → `scheduled`
- Add `author_id` (uuid), `category_id` (uuid), `q` (string)

- [ ] **Step 2: Add PostUpdateRequest schema**

In `components/schemas/posts.yaml` (near `PostCreateRequest`):

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

- [ ] **Step 3: Add PATCH path**

In `paths/posts.yaml` add (separate path key):

```yaml
/api/v1/admin/posts/{id}:
  patch:
    tags: [Admin / Posts]
    summary: Update post content fields
    operationId: admin.posts.update
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
            $ref: ../components/schemas/posts.yaml#/PostUpdateRequest
    responses:
      '200':
        description: Updated
        content:
          application/json:
            schema:
              $ref: ../components/schemas/posts.yaml#/EnvelopePostResponse
      '400': { $ref: ../components/responses.yaml#/ValidationError }
      '401': { $ref: ../components/responses.yaml#/UnauthorizedError }
      '403': { $ref: ../components/responses.yaml#/ForbiddenError }
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Slug conflict
```

- [ ] **Step 4: Register in openapi.yaml**

```yaml
  /api/v1/admin/posts/{id}:
    $ref: ./paths/posts.yaml#/~1api~1v1~1admin~1posts~1{id}
```

Place near other admin posts refs. Note: more specific paths like `.../publish` already exist as separate keys — OK.

- [ ] **Step 5: Bundle + routes**

```bash
npx redocly bundle openapi.yaml -o contracts/openapi.bundle.yaml --ext=yaml
npx redocly bundle openapi.yaml --dereferenced -o contracts/openapi.bundle.deref.yaml --ext=yaml
make routes
rg -n 'admin\.posts\.(list|update)' internal/adapters/inbound/routes/api.go
go test -count=1 ./routes/
```

Expected: `admin.posts.update` present; route count +1.

- [ ] **Step 6: Commit only if user asks**

---

### Task 5: HTTP handlers + router wiring

**Files:**
- Modify: `internal/adapters/inbound/http/handlers_content.go`
- Create: `internal/adapters/inbound/http/handlers_posts_admin_test.go`
- Modify: `internal/adapters/inbound/http/router.go`

**Interfaces:**
- Consumes: `*postservice.PostService`, `roleLookup` (already on deps as `Roles`)
- Helper: `func isUnrestrictedEditor(roles []string) bool` — true if any role is `admin` or `editor`

- [ ] **Step 1: Write HTTP tests (fake posts service if needed)**

Prefer testing handlers with a thin fake implementing only list/update via extending existing patterns — or call handlers with gin test context + stub `PostService` methods through a local interface if `*PostService` is concrete.

Practical approach: unit-test service thoroughly (Task 2); HTTP tests:

- Parse validation: empty PATCH body → 400 (handler-level)
- Map `ErrConflict` → 409 in a small `mapPostError` test via handler with injectable interface

If concrete `*PostService` blocks fakes, introduce:

```go
type postAdminAPI interface {
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error)
}
```

Handlers accept this interface (or keep `*PostService` and rely on service tests + one router smoke).

Minimum HTTP cases:

1. list success returns `meta.total`
2. update validation empty body → 400
3. update conflict → 409

- [ ] **Step 2: Implement handlers**

```go
func adminListPostsHandler(posts *postservice.PostService, roles roleLookup) gin.HandlerFunc
func adminUpdatePostHandler(posts *postservice.PostService, roles roleLookup) gin.HandlerFunc
```

List: parse page/per_page/status/author_id/category_id/q; resolve unrestricted; set `ScopeAuthorID` when restricted; `successWithMeta` with items via `postJSON`.

Update: bind JSON into pointers; build `UpdateInput` / `OptionalCategoryID` (JSON null for category_id → Present+nil Value; omit → Present false); call `Update`; map errors.

- [ ] **Step 3: Wire router**

```go
implemented["admin.posts.list"] = chain(requirePermission(deps.RBAC, "post.read"), adminListPostsHandler(deps.Posts, deps.Roles))
implemented["admin.posts.update"] = chain(requirePermission(deps.RBAC, "post.update"), adminUpdatePostHandler(deps.Posts, deps.Roles))
```

Fallback role chains like existing posts if RBAC nil.

- [ ] **Step 4: Run tests**

```bash
go test -count=1 ./internal/adapters/inbound/http/ ./internal/core/post/...
```

- [ ] **Step 5: Commit only if user asks**

---

### Task 6: Docs and backlog

**Files:**
- Modify: `docs/tasks/phase-2-content-core.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/specs/2026-07-31-admin-posts-list-update-design.md` — Status: `implemented`
- Optional: one sentence in `docs/backend/api.md`

- [ ] **Step 1: Update docs**

Tick:

- `admin.posts.list`
- Post update endpoint

Leave unpublish/archive/soft-delete/revisions unchecked.

- [ ] **Step 2: Validate links**

```bash
(relative-link check removed with scripts/) CHANGELOG.md
```

Expected: 0 violations.

- [ ] **Step 3: Commit only if user asks**

---

## Spec coverage checklist

| Spec requirement | Task |
| --- | --- |
| List filters + pagination + soft-delete exclusion | 2, 3, 5 |
| Author vs admin/editor scope | 2, 5 |
| PATCH update fields | 2, 4, 5 |
| Ownership → 404 | 2, 5 |
| Slug conflict → 409 | 2, 3, 5 |
| Status enum `scheduled` | 4 |
| No revisions | all |
| Docs / changelog | 6 |

## Self-review notes

- No placeholders; CategoryID uses `OptionalCategoryID` (clearer than `**uuid.UUID`).
- `openapi.yaml` must register `/api/v1/admin/posts/{id}` or PATCH will not appear in routes (same class of bug as media complete).
- committed OpenAPI bundles under `contracts/` (redocly lint) may still fail on pre-existing `nullable` noise — use bundle + `make routes` as in media work.
