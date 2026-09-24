# Post Version History Backend

## Hexagonal Placement

- Domain package under `internal/core/post/revisions`
  - entity `PostRevision`
  - value objects: `RevisionType`, `RevisionDiff`, `RevisionChangelog`
  - errors: revision not found, restore blocked, permission denied
- Ports
  - inbound: `PostRevisionPort` (list, get, restore, create revision snapshot)
  - outbound: `PostRevisionRepository`, `PostRevisionAuditSink`, `EventPublisher`
- Service
  - `PostRevisionService`
    - `CreateRevision(ctx, post, author, note?)`
    - `ListRevisions(ctx, post_id, filter)`
    - `GetRevision(ctx, revision_id_or_number)`
    - `RestoreRevision(ctx, post_id, revision_id, actor)`
- Adapters
  - inbound HTTP under admin/posts
  - outbound repository via GORM table `post_revisions`
  - events via Watermill outbox

## Storage Model

### Table post_revisions

Recommended fields:

- id (bigint identity PK), uuid (unique public id)
- post_id (FK -> posts.id, indexed)
- revision_number (integer, per-post sequence; unique(post_id, revision_number))
- revision_type enum: create, update, restore, publish, archive
- title (snapshot at revision time)
- slug (snapshot at revision time)
- excerpt (snapshot at revision time)
- content (snapshot at revision time)
- status (snapshot at revision time)
- author_id (user_id of the modifier/editor)
- category_id_snapshot (FK snapshot)
- cover_image_media_id_snapshot
- media_snapshot_jsonb (post_media rows snapshot at revision time)
- tags_snapshot_jsonb (tag ids + names at revision time)
- seo_snapshot_jsonb (SEO fields at revision time)
- diff_jsonb: per-field diffs (old/new, optional per field)
- changelog_text: auto-generated human-readable summary
- editor_note: optional free text note at save time
- restore_from_revision_id (nullable FK -> post_revisions.id) for restore revisions
- impersonator_id (nullable; if the revision was made during impersonation)
- impersonation_session_id (nullable)
- request_id (nullable for correlation)
- created_at
- updated_at

### Indexes

- index(post_id, created_at desc)
- unique(post_id, revision_number)
- index(author_id)
- index(restore_from_revision_id)

Post revision append-only; rows never mutated.

## Revision Creation Rules

1. `PostService.Create` creates revision_type=create with revision_number=1
2. `PostService.Update` creates revision_type=update with incremented revision_number
3. Publish/Archive transitions create a revision (type publish/archive) if content or meta actually changes (implementation choice: always create for audit completeness)
4. Restore creates a new revision with type `restore`, content/seo copied from the previous revision, and `restore_from_revision_id` set
5. Every revision stores a complete snapshot plus optional diff_jsonb for fast display

## Diff & Changelog Generation

- `diff_jsonb` records per-field old/new for:
  - title, slug, excerpt, content
  - status, category_id, cover_image_media_id
  - media array changes (added/removed media_asset_ids)
  - tag changes
  - SEO fields changes (seo_title, seo_description, seo_keywords, og/twitter fields, canonical_url, robots flags)
- `changelog_text` is generated from the diff: human-readable list of what changed.

## Endpoints

All under `/api/v1/admin/posts/{post_id}/...`.

### `GET /api/v1/admin/posts/{post_id}/revisions`

List revisions for a post.

Parameters:
- page, per_page
- author_id filter
- date range (from_date, to_date)
- include_diff bool (default true)

Response: paginated list of revision summaries (id, revision_number, author, type, created_at, changelog_text, restore_from_revision_id)

Security: requires authentication, requires `post.revisions.view` or appropriate ownership permissions.

### `GET /api/v1/admin/posts/{post_id}/revisions/{revision_id_or_number}`

Get a single revision, including snapshots and diffs.

Response: full revision object with seo/media/tags snapshots + diff.

### `POST /api/v1/admin/posts/{post_id}/revisions/{revision_id_or_number}/restore`

Restore a previous revision as a new revision.

Request body:

```json
{
  "restore_note": "optional human-readable reason"
}
```

Response: returns the newly created revision (type=restore) and the updated post payload.

Restore rules:
- requires `post.revisions.restore` permission
- restore only applies to the content/seo/meta fields, not the post ID itself
- restore does not change post status to published automatically unless explicitly requested; default is to preserve current status or leave to draft if policy says so. Implementation choice: preserve current status.
- emits events

## Events

- `blog.post.revision.created`
  - post_id, revision_id, revision_number, author_id, revision_type, changed_fields
- `blog.post.revision.restored`
  - post_id, restored_from_revision_id, new_revision_id, actor_id

Both events use transactional outbox.

## RBAC

- `post.revisions.view` -> list/get revisions for a post (with ownership checks for authors)
- `post.revisions.restore` -> perform restore operations
- `post.revisions.view_all` -> view revisions across all posts (admin/editor roles)

## Queries

Common queries:

- list revisions by post_id ordered by revision_number desc
- get revision by post_id + number (friendly URL)
- get diff by revision id
- find restores from a given revision id for audit

Cache guidance:

- keep post cache invalidated whenever a revision is created or restored; revisions themselves can be cached by id for short TTL since they are immutable after creation.

## Testing Plan

- revision creation: create/update produces a revision with correct snapshot + diff
- list revisions is filtered and paginated correctly
- get revision returns full snapshot
- restore creates a new revision and updates post content/seo/media/tags accordingly
- restore does not mutate the original revision row but points back via `restore_from_revision_id`
- permissions: author cannot restore/review revisions for another author's posts unless granted editor/admin permissions
- impersonator metadata is correctly attached to revision audit when applicable
