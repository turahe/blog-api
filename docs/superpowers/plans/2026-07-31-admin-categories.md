# Admin Categories CRUD + Reorder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship admin category create, update, delete, and sibling reorder, with public list/get still served by `CategoryService`.

**Architecture:** Expand hexagonal `internal/core/category` (domain/ports/service). Add `sort_order` migration. GORM `CategoryRepository` gains CRUD + reorder. OpenAPI adds admin category paths. HTTP handlers + seed permissions mirror tags admin.

**Tech Stack:** Go 1.26.5, GORM, Gin, Casbin/role gates, OpenAPI + `make routes` / redocly bundle.

## Global Constraints

- Core must not import Gin/GORM/Redis/AWS/Watermill.
- Follow [2026-07-31-admin-categories-design.md](../specs/2026-07-31-admin-categories-design.md) exactly.
- OpenAPI source of truth: edit `paths/` + `components/` + `openapi.yaml` before handlers; never hand-edit `routes_gen.go`.
- Delete with children → 409 `category_has_children`; posts detach via FK SET NULL.
- No nested-set math in this slice.
- Envelope `{ ok, data, meta, error }`; relative Markdown links only.
- AI commits include `Co-Authored-By: Cursor Grok 4.5 <noreply@example.com>`.

## File map

| Path | Responsibility |
|------|----------------|
| `internal/platform/migrations/sql/00007_category_sort_order.sql` | `sort_order` column + index |
| `internal/core/category/domain/category.go` | Entity + errors + UpdateInput helpers |
| `internal/core/category/ports/ports.go` | Repository + Service |
| `internal/core/category/service/service.go` | Business rules |
| `internal/core/category/service/service_test.go` | TDD |
| `internal/adapters/outbound/persistence/category_repository.go` | GORM impl |
| `components/schemas/Categories.yaml` | Create/Update/Reorder request schemas |
| `paths/categories.yaml` | Admin paths |
| `openapi.yaml` | Path + schema `$ref`s |
| `contracts/` + `routes_gen.go` | Regenerated |
| `internal/adapters/inbound/http/handlers_categories.go` | Admin + public handlers |
| `internal/adapters/inbound/http/handlers_categories_test.go` | HTTP tests |
| `internal/adapters/inbound/http/handlers_content.go` | Remove old category handlers / `categoryJSON` once moved |
| `internal/adapters/inbound/http/router.go` | Wire admin ops |
| `internal/bootstrap/app.go` | `categoryservice.New(repo, ids, clock)` |
| `internal/platform/seed/seed.go` | `category.*` perms |
| `docs/tasks/phase-2-content-core.md`, `docs/backend/api.md`, `CHANGELOG.md` | Docs |

---

### Task 1: Domain, ports, and service (TDD)

**Files:**
- Modify: `internal/core/category/domain/category.go`
- Modify: `internal/core/category/ports/ports.go`
- Modify: `internal/core/category/service/service.go`
- Create: `internal/core/category/service/service_test.go`

- [ ] Write failing service tests for Create (slugify, conflict, parent missing, append sort_order), Update (partial, clear parent/image, cycle), Delete (has children, success), Reorder (exact sibling set)
- [ ] Expand domain with `SortOrder`, `ImageID`, `ErrConflict`, `ErrHasChildren`; optional UUID helpers for update
- [ ] Expand ports Repository/Service
- [ ] Implement service with IDGenerator + Clock; keep List/GetBySlug
- [ ] `go test -count=1 ./internal/core/category/...`
- [ ] Commit

### Task 2: Migration + repository

**Files:**
- Create: `internal/platform/migrations/sql/00007_category_sort_order.sql`
- Modify: `internal/adapters/outbound/persistence/category_repository.go`

- [ ] Add migration Up/Down for `sort_order` + index
- [ ] Map `ImageID`, `SortOrder` on `CategoryModel`
- [ ] Implement GetByID, Create, Update, SlugTaken, CountChildren, ListChildrenIDs, MaxSortOrder, Reorder, Delete; List ordered by sort_order, name
- [ ] Commit

### Task 3: OpenAPI contracts + routes

**Files:**
- Modify: `components/schemas/Categories.yaml`
- Modify: `paths/categories.yaml`
- Modify: `openapi.yaml`

- [ ] Add `CategoryCreateRequest`, `CategoryUpdateRequest`, `CategoryReorderRequest`
- [ ] Add admin paths with operationIds from design
- [ ] Register `$ref`s in `openapi.yaml`
- [ ] `make contracts && make routes`
- [ ] Commit

### Task 4: HTTP handlers, seed, bootstrap, router

**Files:**
- Create: `handlers_categories.go`, `handlers_categories_test.go`
- Modify: `handlers_content.go`, `router.go`, `router_test.go`, `app.go`, `seed.go`

- [ ] Move public + add admin handlers; `mapCategoryError`
- [ ] Seed permissions; wire bootstrap + router gates
- [ ] Handler + role-gate tests
- [ ] `go test -count=1 ./internal/adapters/inbound/http/ ./internal/core/category/...`
- [ ] Commit

### Task 5: Docs + backlog

- [ ] Tick Phase 2 admin category boxes; update api.md / CHANGELOG; mark design implemented
- [ ] `make lint` / targeted tests
- [ ] Commit
