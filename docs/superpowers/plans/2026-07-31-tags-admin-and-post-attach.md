# Tags Admin + Post Attach Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship admin tag create/update/merge/delete and create-or-link tag attach on post create/update, with public list routed through a real tag service.

**Architecture:** New hexagonal module `internal/core/tag` (domain/ports/service). GORM `TagRepository` gains CRUD, merge transaction, and `ReplacePostTags`. `PostService.WithTags` resolves names and rewrites `post_tags`. OpenAPI adds admin tag paths and replaces unused `tag_ids` with `tags: string[]`.

**Tech Stack:** Go 1.26.5, GORM, Gin, existing role/Casbin gates, OpenAPI + `make routes` / redocly bundle.

## Global Constraints

- Core must not import Gin/GORM/Redis/AWS/Watermill.
- Follow [2026-07-31-tags-admin-and-post-attach-design.md](../specs/2026-07-31-tags-admin-and-post-attach-design.md) exactly.
- OpenAPI source of truth: edit `paths/` + `components/` + `openapi.yaml` before handlers; never hand-edit `routes_gen.go`.
- No new migration (`tags` / `post_tags` already exist).
- Delete in-use tags → 409 `tag_in_use`; merge reassigns then deletes source.
- Post `tags` omit vs `[]` vs names: pointer semantics on `UpdateInput.Tags` / create arg.
- Envelope `{ ok, data, meta, error }`; relative Markdown links only.
- AI commits only when the user asks; if committing, include `Co-Authored-By: Composer <noreply@example.com>`.

## File map

| Path | Responsibility |
|------|----------------|
| `internal/core/tag/domain/tag.go` | `Tag`, errors |
| `internal/core/tag/ports/ports.go` | Repository + Service |
| `internal/core/tag/service/service.go` | Business rules |
| `internal/core/tag/service/service_test.go` | TDD |
| `internal/adapters/outbound/persistence/tag_repository.go` | GORM impl + `PostTagModel` |
| `internal/core/post/domain/post.go` | `UpdateInput.Tags *[]string` |
| `internal/core/post/ports/ports.go` | `TagLinker`; Service signatures |
| `internal/core/post/service/service.go` | `WithTags`, create/update attach |
| `internal/core/post/service/service_test.go` | Attach tests |
| `components/schemas/Categories.yaml` | `TagCreateRequest`, `TagUpdateRequest`, `TagMergeRequest`, `EnvelopeTagResponse` |
| `components/schemas/posts.yaml` | Replace `tag_ids` with `tags`; add to update + Post schema |
| `paths/tags.yaml` (new) or extend `paths/categories.yaml` | Admin tag ops + keep public list |
| `openapi.yaml` | Path `$ref`s |
| `contracts/openapi.bundle.deref.yaml` | Regenerated |
| `internal/adapters/inbound/http/v1/routes_gen.go` | Regenerated |
| `internal/adapters/inbound/http/handlers_tags.go` | Admin + public tag handlers |
| `internal/adapters/inbound/http/handlers_content.go` | Post create/update tags; drop repo listTags |
| `internal/adapters/inbound/http/handlers_tags_test.go` | HTTP tests |
| `internal/adapters/inbound/http/router.go` | Wire deps |
| `internal/bootstrap/app.go` | Construct TagService, `Posts.WithTags` |
| `docs/tasks/phase-2-content-core.md`, `docs/backend/api.md`, `CHANGELOG.md` | Docs |
| Spec status | Mark `implemented` when done |

---

### Task 1: Tag domain, ports, and service (TDD)

**Files:**
- Create: `internal/core/tag/domain/tag.go`
- Create: `internal/core/tag/ports/ports.go`
- Create: `internal/core/tag/service/service.go`
- Create: `internal/core/tag/service/service_test.go`

**Interfaces:**
- Produces:

```go
// domain
var (
	ErrNotFound = errors.New("tag not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("tag in use")
)

type Tag struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}

// ports.Repository
List(ctx context.Context) ([]tagdomain.Tag, error)
GetByID(ctx context.Context, id uuid.UUID) (tagdomain.Tag, error)
GetBySlug(ctx context.Context, slug string) (tagdomain.Tag, error)
Create(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error)
Update(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error)
SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
CountPosts(ctx context.Context, tagID uuid.UUID) (int64, error)
// MergeInto: transaction — reassign post_tags source→target (skip dupes), delete source tag
MergeInto(ctx context.Context, sourceID, targetID uuid.UUID) error
Delete(ctx context.Context, id uuid.UUID) error
ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error
ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error)

// ports.Service / *service.Service
List, Create(name, slug string), Update(id, name, slug *string),
Merge(sourceID, intoID uuid.UUID), Delete(id),
ResolveOrCreate(names []string) ([]tagdomain.Tag, error)
```

- Service also needs `IDGenerator` / `Clock` (same pattern as post/media).

- [ ] **Step 1: Write failing tests** in `service_test.go` with a fake repo covering:

1. `Create` slugifies name; rejects empty name
2. `Create` when `SlugTaken` → `ErrConflict`
3. `Update` rename conflict → `ErrConflict`
4. `Delete` when `CountPosts > 0` → `ErrInUse`; when 0 → calls `Delete`
5. `Merge` same id → validation error; success calls `MergeInto`
6. `ResolveOrCreate` dedupes by slug, creates missing, returns existing

Minimal fake:

```go
type fakeRepo struct {
	byID    map[uuid.UUID]tagdomain.Tag
	bySlug  map[string]tagdomain.Tag
	counts  map[uuid.UUID]int64
	merged  [][2]uuid.UUID
	deleted []uuid.UUID
}

// implement ports.Repository; MergeInto records pair and deletes source from maps
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test -count=1 ./internal/core/tag/service/
```

Expected: package or symbols missing / FAIL

- [ ] **Step 3: Implement domain, ports, service**

Slugify (ASCII-focused, match post style):

```go
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '_' || r == '-':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
```

`ResolveOrCreate`:

```go
func (s *Service) ResolveOrCreate(ctx context.Context, names []string) ([]tagdomain.Tag, error) {
	seen := map[string]struct{}{}
	out := make([]tagdomain.Tag, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > 64 {
			return nil, fmt.Errorf("%w: name too long", ErrValidation)
		}
		slug := slugify(name)
		if slug == "" {
			return nil, fmt.Errorf("%w: invalid tag name", ErrValidation)
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		if existing, err := s.repo.GetBySlug(ctx, slug); err == nil {
			out = append(out, existing)
			continue
		} else if !errors.Is(err, tagdomain.ErrNotFound) {
			return nil, err
		}
		created, err := s.Create(ctx, name, slug)
		if err != nil {
			return nil, err
		}
		out = append(out, created)
	}
	return out, nil
}
```

`Delete`:

```go
n, err := s.repo.CountPosts(ctx, id)
// ...
if n > 0 {
	return tagdomain.ErrInUse
}
return s.repo.Delete(ctx, id)
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test -count=1 ./internal/core/tag/...
```

- [ ] **Step 5: Commit only if user asks**

---

### Task 2: GORM tag repository

**Files:**
- Modify: `internal/adapters/outbound/persistence/tag_repository.go`

**Interfaces:**
- Consumes: `tagdomain.Tag` shapes from Task 1
- Produces: full `ports.Repository` implementation (adapter may keep local `Tag` DTO or map to domain — prefer mapping to `tagdomain.Tag` at the boundary if handlers will use the service only; repo used only by tag service + post linker via same repo)

Implement methods on `*TagRepository`. Add:

```go
type PostTagModel struct {
	PostID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TagID  uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (PostTagModel) TableName() string { return "post_tags" }
```

`MergeInto` (single transaction):

```go
func (r *TagRepository) MergeInto(ctx context.Context, sourceID, targetID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// INSERT INTO post_tags (post_id, tag_id)
		// SELECT post_id, target FROM post_tags WHERE tag_id = source
		// ON CONFLICT DO NOTHING
		if err := tx.Exec(`
			INSERT INTO post_tags (post_id, tag_id)
			SELECT post_id, ? FROM post_tags WHERE tag_id = ?
			ON CONFLICT DO NOTHING`, targetID, sourceID).Error; err != nil {
			return err
		}
		if err := tx.Where("tag_id = ?", sourceID).Delete(&PostTagModel{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", sourceID).Delete(&TagModel{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return tagdomain.ErrNotFound
		}
		return nil
	})
}
```

`ReplacePostTags`: delete all for post, then batch insert.

`ListByPostID`: join `tags` via `post_tags` order by name.

`GetBySlug` / `GetByID`: map not found → `tagdomain.ErrNotFound`.

Change `List` return type to `[]tagdomain.Tag` **or** keep persistence `Tag` and have the service adapter wrap — simplest: change repository methods to return `tagdomain.Tag` and update bootstrap/handlers in Task 4 to stop using persistence.Tag for list.

Until Task 4, `listTagsHandler` still uses old `List() ([]Tag, error)`. Either:

- Keep `List() ([]Tag, error)` and add `ListDomain() ([]tagdomain.Tag, error)`, or
- Update list handler in Task 4 only and temporarily break compile — prefer implementing domain-typed methods and updating the one call site in Task 4.

- [ ] **Step 1: Implement repository methods**

- [ ] **Step 2: Compile**

```bash
go test -count=1 ./internal/adapters/outbound/persistence/ ./internal/core/tag/...
```

Expected: PASS (persistence may have no tests)

- [ ] **Step 3: Commit only if user asks**

---

### Task 3: OpenAPI + routes

**Files:**
- Modify: `components/schemas/Categories.yaml`
- Modify: `components/schemas/posts.yaml`
- Create or modify: `paths/tags.yaml` (prefer new file; move `/api/v1/tags` from `categories.yaml` into it)
- Modify: `openapi.yaml`
- Regenerate: `contracts/openapi.bundle.deref.yaml`, `internal/adapters/inbound/http/v1/routes_gen.go`

**Interfaces:**
- Produces operationIds: `admin.tags.create`, `admin.tags.update`, `admin.tags.merge`, `admin.tags.delete` (keep `public.tags.list`)

- [ ] **Step 1: Schemas**

Add to `Categories.yaml`:

```yaml
TagCreateRequest:
  type: object
  required: [name]
  properties:
    name: { type: string, minLength: 1, maxLength: 64 }
    slug: { type: string }
TagUpdateRequest:
  type: object
  minProperties: 1
  properties:
    name: { type: string, minLength: 1, maxLength: 64 }
    slug: { type: string }
TagMergeRequest:
  type: object
  required: [into_id]
  properties:
    into_id: { type: string, format: uuid }
EnvelopeTagResponse:
  allOf:
  - $ref: Common.yaml#/Envelope
  - type: object
    properties:
      data:
        $ref: Categories.yaml#/Tag
```

In `posts.yaml` `PostCreateRequest`: remove `tag_ids`; add:

```yaml
tags:
  type: array
  items:
    type: string
    minLength: 1
    maxLength: 64
```

Same `tags` on `PostUpdateRequest` (optional; with other fields still `minProperties: 1` — allow tags-only update by counting tags as a property).

On `Post` schema, add optional:

```yaml
tags:
  type: array
  items:
    $ref: Categories.yaml#/Tag
```

(Use correct relative `$ref` path from `posts.yaml`.)

- [ ] **Step 2: Paths**

`paths/tags.yaml`:

```yaml
/api/v1/tags:
  get:
    tags: [Public]
    summary: List tags
    security: []
    operationId: public.tags.list
    responses:
      '200':
        description: OK
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeTagList
/api/v1/admin/tags:
  post:
    tags: [Admin]
    summary: Create tag
    operationId: admin.tags.create
    security:
      - bearerAuth: []
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: ../components/schemas/Categories.yaml#/TagCreateRequest
    responses:
      '201':
        description: Created
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeTagResponse
      '400': { $ref: ../components/responses.yaml#/BadRequestError }
      '409':
        description: Slug conflict
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
/api/v1/admin/tags/{id}:
  parameters:
  - name: id
    in: path
    required: true
    schema: { type: string, format: uuid }
  patch:
    tags: [Admin]
    operationId: admin.tags.update
    security: [{ bearerAuth: [] }]
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: ../components/schemas/Categories.yaml#/TagUpdateRequest
    responses:
      '200':
        description: OK
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeTagResponse
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Conflict
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
  delete:
    tags: [Admin]
    operationId: admin.tags.delete
    security: [{ bearerAuth: [] }]
    responses:
      '204': { description: Deleted }
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Tag in use
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
/api/v1/admin/tags/{id}/merge:
  post:
    tags: [Admin]
    operationId: admin.tags.merge
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
            $ref: ../components/schemas/Categories.yaml#/TagMergeRequest
    responses:
      '200':
        description: Target tag after merge
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Categories.yaml#/EnvelopeTagResponse
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Conflict
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Common.yaml#/EnvelopeError
```

Remove duplicate `/api/v1/tags` from `paths/categories.yaml`. Point `openapi.yaml` at `./paths/tags.yaml` for public + admin tag paths. Register new component schemas in `openapi.yaml` if required by existing pattern.

- [ ] **Step 3: Bundle + routes**

```bash
npx redocly bundle openapi.yaml --dereferenced -o contracts/openapi.bundle.deref.yaml --ext=yaml
make routes
```

Expected: `routes_gen.go` includes the four admin tag ops.

- [ ] **Step 4: Commit only if user asks**

---

### Task 4: HTTP handlers + bootstrap/router

**Files:**
- Create: `internal/adapters/inbound/http/handlers_tags.go`
- Create: `internal/adapters/inbound/http/handlers_tags_test.go`
- Modify: `internal/adapters/inbound/http/handlers_content.go` (remove `listTagsHandler` using repo)
- Modify: `internal/adapters/inbound/http/router.go`
- Modify: `internal/bootstrap/app.go`

**Interfaces:**
- Consumes: `*tagservice.Service`
- Router `Dependencies.Tags` type changes from `*persistence.TagRepository` to `*tagservice.Service`

- [ ] **Step 1: Handlers**

Map errors:

```go
switch {
case errors.Is(err, tagservice.ErrValidation):
	failure(..., 400, "validation_error", ...)
case errors.Is(err, tagdomain.ErrNotFound):
	failure(..., 404, "not_found", ...)
case errors.Is(err, tagdomain.ErrConflict):
	failure(..., 409, "conflict", ...)
case errors.Is(err, tagdomain.ErrInUse):
	failure(..., 409, "tag_in_use", ...)
}
```

`tagJSON(t tagdomain.Tag) gin.H` with `id`, `name`, `slug`, `created_at`.

Delete → `204` empty body (or envelope if project always envelopes — check other deletes; media delete may use envelope; match media/admin delete pattern).

- [ ] **Step 2: Router**

```go
if deps.Tags != nil {
	implemented["public.tags.list"] = listTagsHandler(deps.Tags)
	create := adminCreateTagHandler(deps.Tags)
	update := adminUpdateTagHandler(deps.Tags)
	merge := adminMergeTagHandler(deps.Tags)
	del := adminDeleteTagHandler(deps.Tags)
	if deps.Roles != nil {
		gate := requireRoles(deps.Roles, "admin", "editor")
		implemented["admin.tags.create"] = chain(gate, create)
		// same for update, merge, delete
	} else {
		implemented["admin.tags.create"] = create
		// ...
	}
}
```

- [ ] **Step 3: Bootstrap**

```go
tagsRepo := persistence.NewTagRepository(db.GORM)
tagsSvc := tagservice.New(tagsRepo, uuidGen, clock)
// postsSvc.WithTags(tagsRepo) comes in Task 5 — TagRepository must satisfy postports.TagLinker
httpDeps.Tags = tagsSvc
```

- [ ] **Step 4: HTTP tests**

Fake tag service interface for create/delete/merge; assert 409 on in-use delete; 201 on create.

```bash
go test -count=1 ./internal/adapters/inbound/http/ -run Tag
```

Expected: PASS

- [ ] **Step 5: Commit only if user asks**

---

### Task 5: Post attach (service + handlers)

**Files:**
- Modify: `internal/core/post/domain/post.go` — add `Tags *[]string` to `UpdateInput`
- Modify: `internal/core/post/ports/ports.go` — `TagLinker` + update Service signatures
- Modify: `internal/core/post/service/service.go`
- Modify: `internal/core/post/service/service_test.go`
- Modify: `internal/adapters/inbound/http/handlers_content.go` — create/update request + response tags
- Modify: `internal/adapters/inbound/http/handlers_posts_admin_test.go` / create tests
- Modify: `internal/bootstrap/app.go` — `posts.WithTags(tagsRepo)`

**Interfaces:**

```go
// post/ports
type TagLinker interface {
	ResolveOrCreate(ctx context.Context, names []string) ([]tagdomain.Tag, error)
	ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error
	ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error)
}
```

Note: `ResolveOrCreate` lives on tag **service**, but `ReplacePostTags` / `ListByPostID` on repo. Options:

1. Put all three on `*tagservice.Service` (service delegates Replace/List to repo) — **preferred** so Post depends only on tag service.
2. Composite adapter in bootstrap.

Prefer (1): add to tag service:

```go
func (s *Service) ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error {
	return s.repo.ReplacePostTags(ctx, postID, tagIDs)
}
func (s *Service) ListByPostID(...) ([]tagdomain.Tag, error) {
	return s.repo.ListByPostID(ctx, postID)
}
```

Then `posts.WithTags(tagsSvc)`.

Signature changes:

```go
CreateDraft(ctx, authorID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error)

Update(...) (postdomain.Post, []tagdomain.Tag, error)
```

Empty-update validation: treat `in.Tags != nil` as a field present (tags-only PATCH allowed).

After successful create/update:

```go
if tags != nil {
	resolved, err := s.tags.ResolveOrCreate(ctx, *tags)
	// collect IDs, ReplacePostTags
	return post, resolved, nil
}
return post, s.tags.ListByPostID(ctx, post.ID) // if tags linker nil, return nil slice
```

If `s.tags == nil` and tags provided → validation error.

- [ ] **Step 1: Failing post service tests** — omit tags does not call Replace; `[]` clears; names resolve+replace

- [ ] **Step 2: Implement WithTags + CreateDraft/Update**

- [ ] **Step 3: Handlers** — parse `Tags *[]string` from JSON (`json.RawMessage` or pointer); include `tags` in response via `tagJSON` list

Update `postAdminAPI` / create handler return types accordingly.

- [ ] **Step 4: Tests**

```bash
go test -count=1 ./internal/core/post/... ./internal/adapters/inbound/http/
```

Expected: PASS

- [ ] **Step 5: Commit only if user asks**

---

### Task 6: Docs and backlog

**Files:**
- Modify: `docs/tasks/phase-2-content-core.md` — tick admin tag ops + attach/detach
- Modify: `docs/backend/api.md` — list new operations
- Modify: `CHANGELOG.md` — dated entry
- Modify: `docs/superpowers/specs/2026-07-31-tags-admin-and-post-attach-design.md` — Status: `implemented`

- [ ] **Step 1: Update docs**

- [ ] **Step 2: Validate relative links**

```bash
node scripts/docs/validate_relative_links.cjs docs contracts paths README.md
```

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
| `internal/core/tag` module | 1 |
| GORM expand + MergeInto txn | 2 |
| Admin OpenAPI ops + EnvelopeTagResponse | 3 |
| Replace `tag_ids` with `tags` strings | 3 |
| Public list via TagService | 4 |
| Admin handlers + admin/editor gate | 4 |
| Post create/update attach create-or-link | 5 |
| Delete in-use 409; merge reassign | 1+2+4 |
| Docs / phase-2 ticks | 6 |
| No migration | (all) |
