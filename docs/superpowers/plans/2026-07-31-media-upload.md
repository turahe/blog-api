# Media Upload MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admin media upload via presigned PUT + explicit complete: `media_assets` schema, hexagonal media module, S3-compatible adapter, and OpenAPI/handlers for `admin.media.create` / `admin.media.complete`.

**Architecture:** `media.Service` validates policy and owns pending→ready lifecycle; `ObjectStorage` port issues presigned PUTs and Heads objects; GORM repository persists assets; Gin handlers map operation IDs with Casbin `media.create`. Core stays free of Gin/GORM/AWS SDK.

**Tech Stack:** Go 1.26.5, AWS SDK v2 (`aws-sdk-go-v2` + `service/s3` + `presign`), GORM, Gin, Goose SQL migrations, MinIO (Compose).

## Global Constraints

- Core (`internal/core/**`) must not import Gin, GORM, Redis, Watermill, or AWS SDK.
- Follow [2026-07-31-media-upload-design.md](../specs/2026-07-31-media-upload-design.md) exactly; no list/delete/transform/malware/outbox in this plan.
- OpenAPI is source of truth: update `paths/` + `components/` before handlers; then `make contracts` and `make routes`. Never hand-edit `routes_gen.go`.
- Envelope responses: `{ ok, data, meta, error }` with stable `error.code`.
- Relative Markdown links only.
- `make test` / `go test` stay scoped to `./cmd/... ./internal/...` (avoid `data/` volume scan).
- Pin exact AWS SDK module versions in `go get` (no `@latest`).
- AI commits only when the user asks; if committing, include `Co-Authored-By: Composer <noreply@example.com>`.

## File map

| Path | Responsibility |
|------|----------------|
| `internal/platform/config/config.go` | `S3_*` + `MEDIA_*` fields, validation, allowlist parse |
| `internal/platform/config/media_test.go` | Config / policy tests |
| `internal/platform/migrations/sql/00005_media_assets.sql` | `media_assets` table |
| `internal/core/media/domain/media.go` | Asset entity, status, disk enums |
| `internal/core/media/ports/ports.go` | Repository + ObjectStorage + Service interfaces |
| `internal/core/media/service/service.go` | PresignUpload + CompleteUpload |
| `internal/core/media/service/service_test.go` | Fake-backed unit tests |
| `internal/adapters/outbound/storage/s3.go` | AWS SDK v2 S3-compatible adapter |
| `internal/adapters/outbound/persistence/media_repository.go` | GORM media repo |
| `paths/media.yaml` | Contract: create = presign JSON; add complete |
| `components/schemas/Media.yaml` | Presign request/response schemas |
| `internal/adapters/inbound/http/handlers_media.go` | HTTP handlers |
| `internal/adapters/inbound/http/router.go` | Wire operation IDs + RBAC |
| `internal/adapters/inbound/http/deps.go` (or Dependencies struct) | Media service field |
| `internal/bootstrap/app.go` | Construct storage + media service |
| `.env.example`, `docs/deployment/config.md` | Document vars |
| `docs/backend/media.md` | Presign workflow note |
| `docs/tasks/phase-2-content-core.md` | Tick upload MVP items |
| `CHANGELOG.md` | Feature entry |
| `docs/superpowers/specs/2026-07-31-media-upload-design.md` | Mark implemented when done |

---

### Task 1: Media and S3 configuration

**Files:**
- Modify: `internal/platform/config/config.go`
- Create: `internal/platform/config/media_test.go`
- Modify: `.env.example`
- Modify: `docs/deployment/config.md`

**Interfaces:**
- Produces:
  - Config fields: `S3Endpoint`, `S3Region`, `S3Bucket`, `S3AccessKey`, `S3SecretKey`, `S3PublicBaseURL`, `S3Disk`, `S3ForcePathStyle bool`, `MediaAllowedMIMETypes []string`, `MediaMaxUploadBytes int64`, `MediaPresignTTL time.Duration`
  - `func (c Config) MediaEnabled() bool` — true when bucket + access key + secret are non-empty
  - `func (c Config) ValidateMedia() error` — if MediaEnabled, disk must be one of `minio|s3|r2|do_spaces`; allowlist non-empty; max bytes > 0; TTL > 0
  - `func ParseMIMEList(raw string) []string`

- [ ] **Step 1: Write the failing tests**

```go
package config

import (
	"testing"
	"time"
)

func TestParseMIMEList(t *testing.T) {
	t.Parallel()
	got := ParseMIMEList("image/png, image/jpeg ,image/webp")
	if len(got) != 3 || got[0] != "image/png" || got[2] != "image/webp" {
		t.Fatalf("got %#v", got)
	}
}

func TestValidateMediaDisabledOK(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	if err := cfg.ValidateMedia(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMediaRequiresDisk(t *testing.T) {
	t.Parallel()
	cfg := Config{
		S3Bucket: "blog-media", S3AccessKey: "k", S3SecretKey: "s",
		S3Disk: "ftp", MediaAllowedMIMETypes: []string{"image/png"},
		MediaMaxUploadBytes: 10, MediaPresignTTL: time.Minute,
	}
	if err := cfg.ValidateMedia(); err == nil {
		t.Fatal("expected disk error")
	}
}

func TestLoadMediaDefaults(t *testing.T) {
	t.Setenv("S3_BUCKET", "blog-media")
	t.Setenv("S3_ACCESS_KEY", "minioadmin")
	t.Setenv("S3_SECRET_KEY", "minioadmin")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MediaEnabled() {
		t.Fatal("expected MediaEnabled")
	}
	if cfg.MediaMaxUploadBytes != 10<<20 {
		t.Fatalf("max=%d", cfg.MediaMaxUploadBytes)
	}
	if len(cfg.MediaAllowedMIMETypes) < 4 {
		t.Fatalf("allowlist=%v", cfg.MediaAllowedMIMETypes)
	}
}
```

- [ ] **Step 2: Run tests — expect fail**

Run: `go test -count=1 ./internal/platform/config/ -run 'Media|MIME'`
Expected: FAIL (types/methods missing)

- [ ] **Step 3: Implement config fields**

In `Load()`, add:

```go
S3Endpoint:            env("S3_ENDPOINT", "http://127.0.0.1:9000"),
S3Region:              env("S3_REGION", "auto"),
S3Bucket:              env("S3_BUCKET", ""),
S3AccessKey:           env("S3_ACCESS_KEY", ""),
S3SecretKey:           env("S3_SECRET_KEY", ""),
S3PublicBaseURL:       env("S3_PUBLIC_BASE_URL", ""),
S3Disk:                strings.ToLower(env("S3_DISK", "minio")),
S3ForcePathStyle:      boolEnv("S3_FORCE_PATH_STYLE", true),
MediaAllowedMIMETypes: ParseMIMEList(env("MEDIA_ALLOWED_MIME_TYPES", "image/jpeg,image/png,image/webp,image/gif")),
MediaMaxUploadBytes:   int64(integer("MEDIA_MAX_UPLOAD_BYTES", 10<<20)),
MediaPresignTTL:       duration("MEDIA_PRESIGN_TTL", 15*time.Minute),
```

Call `cfg.ValidateMedia()` from `Load()` alongside Redis/messaging validation.

```go
func ParseMIMEList(raw string) []string { /* splitCSV-style */ }

func (c Config) MediaEnabled() bool {
	return strings.TrimSpace(c.S3Bucket) != "" &&
		strings.TrimSpace(c.S3AccessKey) != "" &&
		strings.TrimSpace(c.S3SecretKey) != ""
}

func (c Config) ValidateMedia() error {
	if !c.MediaEnabled() {
		return nil
	}
	switch c.S3Disk {
	case "minio", "s3", "r2", "do_spaces":
	default:
		return fmt.Errorf("unsupported S3_DISK %q", c.S3Disk)
	}
	if len(c.MediaAllowedMIMETypes) == 0 {
		return errors.New("MEDIA_ALLOWED_MIME_TYPES must not be empty when media is enabled")
	}
	if c.MediaMaxUploadBytes < 1 {
		return errors.New("MEDIA_MAX_UPLOAD_BYTES must be positive")
	}
	if c.MediaPresignTTL <= 0 {
		return errors.New("MEDIA_PRESIGN_TTL must be positive")
	}
	return nil
}
```

Update `.env.example` and `docs/deployment/config.md` (remove “S3 unused” from Compose-only table; document MEDIA_*).

- [ ] **Step 4: Run tests — expect pass**

Run: `go test -count=1 ./internal/platform/config/`

- [ ] **Step 5: Commit only if user asks**

---

### Task 2: `media_assets` migration

**Files:**
- Create: `internal/platform/migrations/sql/00005_media_assets.sql`

**Interfaces:**
- Produces: Goose Up/Down creating `media_assets` per design spec columns

- [ ] **Step 1: Write migration**

```sql
-- +goose Up
CREATE TABLE media_assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_key text NOT NULL,
    original_filename text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    width integer,
    height integer,
    checksum_sha256 text,
    disk text NOT NULL
        CHECK (disk IN ('s3', 'r2', 'minio', 'do_spaces')),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'ready', 'failed')),
    uploaded_by uuid REFERENCES users(id) ON DELETE SET NULL,
    tags text[] NOT NULL DEFAULT '{}',
    presign_expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX media_assets_storage_key_unique ON media_assets (storage_key);
CREATE INDEX media_assets_uploaded_by_idx ON media_assets (uploaded_by);
CREATE INDEX media_assets_status_idx ON media_assets (status) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS media_assets;
```

- [ ] **Step 2: Apply locally (optional smoke)**

Run: `make migrate-up` (infra up)
Expected: migration applied without error

- [ ] **Step 3: Commit only if user asks**

---

### Task 3: Media domain and ports

**Files:**
- Create: `internal/core/media/domain/media.go`
- Create: `internal/core/media/ports/ports.go`

**Interfaces:**
- Produces domain types and port interfaces used by Task 4–6

- [ ] **Step 1: Create domain**

```go
package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

type MediaAsset struct {
	ID               uuid.UUID
	StorageKey       string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	Width            *int
	Height           *int
	ChecksumSHA256   *string
	Disk             string
	Status           string
	UploadedBy       *uuid.UUID
	Tags             []string
	PresignExpiresAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type PresignResult struct {
	Asset            MediaAsset
	UploadURL        string
	RequiredHeaders  map[string]string
	ExpiresAt        time.Time
}
```

- [ ] **Step 2: Create ports**

```go
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

type ObjectInfo struct {
	Size        int64
	ContentType string
	ETag        string
}

type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, headers map[string]string, err error)
	HeadObject(ctx context.Context, key string) (ObjectInfo, error)
}

type Repository interface {
	Create(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error)
	GetByID(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	Update(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error)
}

type Service interface {
	PresignUpload(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error)
	CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
}
```

- [ ] **Step 3: Commit only if user asks**

---

### Task 4: Media service (TDD with fakes)

**Files:**
- Create: `internal/core/media/service/service.go`
- Create: `internal/core/media/service/service_test.go`

**Interfaces:**
- Consumes: ports from Task 3; policy: allowlist `[]string`, maxBytes `int64`, ttl `time.Duration`, disk `string`
- Produces: `*Service` implementing `ports.Service`
- Sentinel errors (wrap/check with `errors.Is`): `ErrValidation`, `ErrNotFound`, `ErrUploadIncomplete`, `ErrUploadExpired`, `ErrStorage`

- [ ] **Step 1: Write failing service tests**

Use in-memory map repo + fake storage that records PresignPut and optionally omits keys from Head.

Cover at minimum:
1. Presign rejects disallowed MIME → `ErrValidation`
2. Presign rejects size > max → `ErrValidation`
3. Presign happy path → status pending, key prefix `media/`, upload URL from fake
4. Complete missing object → `ErrUploadIncomplete`
5. Complete after Head OK → status ready, size from Head
6. Complete already ready → same asset, no error
7. Complete after expiry while pending → `ErrUploadExpired`

- [ ] **Step 2: Run — expect fail**

Run: `go test -count=1 ./internal/core/media/service/`

- [ ] **Step 3: Implement service**

```go
type Service struct {
	repo      ports.Repository
	storage   ports.ObjectStorage
	ids       IDGenerator // uuid.New style interface already used in post service — reuse platform/system
	clock     Clock
	disk      string
	allowMIME map[string]struct{}
	maxBytes  int64
	presignTTL time.Duration
}

func (s *Service) PresignUpload(...) (mediadomain.PresignResult, error) {
	// sanitize filename (basename only, reject .. and empty)
	// check MIME + size
	// id := s.ids.New(); key := fmt.Sprintf("media/%s/%s", id, sanitized)
	// create pending row with PresignExpiresAt = now+ttl
	// url, headers := storage.PresignPut(...)
}

func (s *Service) CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	// get asset; not found / deleted → ErrNotFound
	// if ready → return
	// if pending && expired → ErrUploadExpired
	// head := storage.HeadObject; not found → ErrUploadIncomplete
	// validate head content-type + size against policy
	// set ready, size_bytes, content_type; Update
}
```

Filename sanitization: `filepath.Base`, strip path separators, reject empty / `.` / `..`.

- [ ] **Step 4: Run — expect pass**

Run: `go test -count=1 ./internal/core/media/service/`

- [ ] **Step 5: Commit only if user asks**

---

### Task 5: S3-compatible ObjectStorage adapter

**Files:**
- Create: `internal/adapters/outbound/storage/s3.go`
- Create: `internal/adapters/outbound/storage/s3_test.go` (unit test with mocked options if feasible; otherwise compile-only + interface assert)
- Modify: `go.mod` / `go.sum` via `go get`

**Interfaces:**
- Consumes: `config.Config` S3 fields
- Produces: `func NewS3(ctx, cfg) (*Client, error)` implementing `ports.ObjectStorage`

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/aws/aws-sdk-go-v2@v1.36.3
go get github.com/aws/aws-sdk-go-v2/config@v1.29.14
go get github.com/aws/aws-sdk-go-v2/credentials@v1.17.67
go get github.com/aws/aws-sdk-go-v2/service/s3@v1.79.3
```

(Adjust to latest compatible pinned versions resolving cleanly at implement time.)

- [ ] **Step 2: Implement client**

```go
// NewS3 builds a path-style-capable S3 client for MinIO/R2/S3.
// PresignPut uses s3.NewPresignClient.PresignPutObject with ContentType.
// HeadObject maps NotFound to a typed error the service maps to ErrUploadIncomplete
// (or return err that service detects via errors.As on storage.ErrNotFound).
```

Export `var ErrObjectNotFound = errors.New("object not found")` from storage package; service maps it to `ErrUploadIncomplete`.

- [ ] **Step 3: Compile check**

Run: `go test -count=1 ./internal/adapters/outbound/storage/ ./internal/core/media/...`

- [ ] **Step 4: Commit only if user asks**

---

### Task 6: Media repository (GORM)

**Files:**
- Create: `internal/adapters/outbound/persistence/media_repository.go`
- Create: `internal/adapters/outbound/persistence/media_repository_test.go` optional (skip if no test DB harness; prefer service fakes)

**Interfaces:**
- Consumes: `*gorm.DB`
- Produces: `NewMediaRepository(db *gorm.DB) *MediaRepository` implementing `ports.Repository`

- [ ] **Step 1: Implement model + repo**

Map columns 1:1 with migration. Soft-deleted rows excluded from `GetByID` (`deleted_at IS NULL`). `Create` / `Update` set `updated_at`.

- [ ] **Step 2: Compile**

Run: `go test -count=1 ./internal/adapters/outbound/persistence/ ./internal/core/media/...`

- [ ] **Step 3: Commit only if user asks**

---

### Task 7: OpenAPI contract + route generation

**Files:**
- Modify: `paths/media.yaml`
- Modify: `components/schemas/Media.yaml`
- Regenerate: `internal/adapters/inbound/http/v1/routes_gen.go` via `make routes`

**Interfaces:**
- Produces operation IDs: `admin.media.create` (presign JSON), `admin.media.complete`

- [ ] **Step 1: Update schemas**

Add to `Media.yaml`:

```yaml
MediaPresignRequest:
  type: object
  required: [original_filename, content_type, size_bytes]
  properties:
    original_filename: { type: string, minLength: 1, maxLength: 255 }
    content_type: { type: string }
    size_bytes: { type: integer, minimum: 1 }
    tags:
      type: array
      items: { type: string }
MediaPresignData:
  type: object
  required: [media_id, storage_key, upload_url, required_headers, expires_at, disk]
  properties:
    media_id: { type: string, format: uuid }
    storage_key: { type: string }
    upload_url: { type: string, format: uri }
    required_headers:
      type: object
      additionalProperties: { type: string }
    expires_at: { type: string, format: date-time }
    disk: { type: string, enum: [s3, r2, minio, do_spaces] }
EnvelopeMediaPresignResponse:
  allOf:
  - $ref: Common.yaml#/Envelope
  - type: object
    properties:
      data: { $ref: Media.yaml#/MediaPresignData }
```

- [ ] **Step 2: Replace POST `/api/v1/admin/media` body**

`content: application/json` → `MediaPresignRequest`; response `201` → `EnvelopeMediaPresignResponse`.

- [ ] **Step 3: Add complete path**

```yaml
/api/v1/admin/media/{id}/complete:
  post:
    tags: [Admin / Media]
    summary: Finalize a pending media upload after the client PUT to storage
    operationId: admin.media.complete
    parameters:
    - name: id
      in: path
      required: true
      schema: { type: string, format: uuid }
    responses:
      '200':
        description: Asset ready
        content:
          application/json:
            schema:
              $ref: ../components/schemas/Media.yaml#/EnvelopeMediaResponse
      '400': { $ref: ../components/responses.yaml#/ValidationError }
      '401': { $ref: ../components/responses.yaml#/UnauthorizedError }
      '403': { $ref: ../components/responses.yaml#/ForbiddenError }
      '404': { $ref: ../components/responses.yaml#/NotFoundError }
      '409':
        description: Upload incomplete or expired
```

Ensure `contracts/openapi.yaml` already `$ref`s `paths/media.yaml` (it should).

- [ ] **Step 4: Validate and generate**

```bash
make contracts
make routes
go test -count=1 ./internal/adapters/inbound/http/v1/
```

Expected: `admin.media.complete` appears in `routes_gen.go` with `GroupAdmin`, `AuthRequired`.

- [ ] **Step 5: Commit only if user asks**

---

### Task 8: HTTP handlers + bootstrap wiring

**Files:**
- Create: `internal/adapters/inbound/http/handlers_media.go`
- Create: `internal/adapters/inbound/http/handlers_media_test.go`
- Modify: `internal/adapters/inbound/http` Dependencies struct (likely in `router.go` or `deps.go`)
- Modify: `internal/adapters/inbound/http/router.go` `handlerFor`
- Modify: `internal/bootstrap/app.go`

**Interfaces:**
- Consumes: `mediaports.Service`
- Maps: `admin.media.create`, `admin.media.complete` with `requirePermission(..., "media.create")`

- [ ] **Step 1: Write HTTP tests with fake service**

Table cases:
- presign 201 + `ok` + `data.upload_url`
- presign validation → 400 `validation_error`
- complete not found → 404
- complete incomplete → 409 `media.upload_incomplete`

- [ ] **Step 2: Run — expect fail**

Run: `go test -count=1 ./internal/adapters/inbound/http/ -run Media`

- [ ] **Step 3: Implement handlers**

Map service errors:

| Service error | HTTP | code |
| --- | --- | --- |
| `ErrValidation` | 400 | `validation_error` |
| `ErrNotFound` | 404 | `not_found` |
| `ErrUploadIncomplete` | 409 | `media.upload_incomplete` |
| `ErrUploadExpired` | 409 | `media.upload_expired` |
| `ErrStorage` / other | 502 | `storage_unavailable` |

Read `uploaded_by` from auth context (same pattern as post create).

- [ ] **Step 4: Bootstrap**

If `cfg.MediaEnabled()`: `storage.NewS3` + `persistence.NewMediaRepository` + `mediaservice.New` → pass into HTTP deps. If media disabled, leave handlers unwired (501) **or** return clear 503 — prefer leave unwired only when MediaEnabled is false; when enabled but Open fails, `NewRuntime` returns error.

- [ ] **Step 5: Run tests**

```bash
go test -count=1 ./internal/adapters/inbound/http/ ./internal/core/media/... ./internal/platform/config/
make lint
```

- [ ] **Step 6: Commit only if user asks**

---

### Task 9: Docs and backlog

**Files:**
- Modify: `docs/backend/media.md` — document presign + complete workflow; note malware/transform deferred
- Modify: `docs/tasks/phase-2-content-core.md` — tick storage adapter, media table, `admin.media.create` (as presign), upload validation; leave list/delete/transform unchecked; add note that create is presign not multipart
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/specs/2026-07-31-media-upload-design.md` — Status: `implemented`

- [ ] **Step 1: Update docs**

- [ ] **Step 2: Validate links**

```bash
node scripts/docs/validate_relative_links.cjs docs contracts paths README.md CHANGELOG.md
```

- [ ] **Step 3: Manual smoke (optional, local)**

```bash
make infra-up
# ensure bucket exists (mc mb or MinIO console)
make migrate-up
# source .env; go run ./cmd serve
# POST /api/v1/admin/media with bearer token → PUT upload_url → POST .../complete
```

- [ ] **Step 4: Commit only if user asks**

---

## Spec coverage checklist

| Spec requirement | Task |
| --- | --- |
| S3 + MEDIA config | 1 |
| `media_assets` migration | 2 |
| Domain / ports | 3 |
| PresignUpload + CompleteUpload | 4 |
| ObjectStorage S3 adapter | 5 |
| Repository | 6 |
| OpenAPI create/complete | 7 |
| Handlers + Casbin + bootstrap | 8 |
| Docs / changelog / backlog | 9 |
| Non-goals excluded | — (no tasks) |

## Self-review

- No TBD/placeholder steps.
- Error codes match design.
- Operation IDs and package paths consistent across tasks.
- TDD order on Tasks 1, 4, 8.
