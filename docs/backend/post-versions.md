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

## Implementation

- **Storage:** migration `00024_post_revisions.sql`. Snapshot columns follow the table above, with
  these differences:
  - Category and cover are stored as `category_uuid` and `cover_image_media_uuid`, without foreign
    keys, so a revision survives the referenced row's deletion.
  - `changed_fields` is a `jsonb` array.
  - `seo_snapshot` stays `{}` until the SEO epic fills it.
  - `impersonation_session_id` and `updated_at` are not stored: rows are append-only, and the
    impersonation epic adds its own columns.
- **Revision types:** `create`, `update`, `publish`, `unpublish`, `archive`, `delete` (moved to
  trash), `undelete` (restored from trash), and `restore`. Media replacement records an `update`.
- **Capture:** `PostService` records one revision in the same transaction as every post write:
  create, update, media replace, publish, unpublish, archive, delete, undelete, and restore.
  - A failed revision write rolls the post write back.
  - The revision is written after the post row, so the row lock orders concurrent writers.
  - `unique(post_id, revision_number)` backs this up and maps to `409 post.version_conflict`.
  - Posts created before this migration get revision 1 on their next write, with no diff.
- **Attribution:** `author_id` is the acting user.
  - Admin post handlers run under `withPostEditor`, which puts the signed-in user and request id
    in the request context.
  - Writes without a user, such as scheduled publishing, store `author_id = null`.
- **Diff:**
  - `{from, to}` for title, slug, excerpt, status, comment policy, category, cover, and SEO.
  - `{from_length, to_length}` for content, since the snapshot holds the full text.
  - `{added, removed}` for tags (names) and media (asset, kind, and sort order), or
    `{reordered: true}` when only the media order changed.
  - The changelog is generated from the diff, for example "Updated title and content" or
    "Restored revision 3; changed slug".
- **Restore:** copies title, slug, excerpt, content, comment policy, category, cover, tags, and
  media. The post keeps its status and `published_at`.
  - The response lists anything that could not be restored under `skipped`:
    - categories and tags that were deleted;
    - media that is deleted or no longer ready;
    - the slug, when another live post holds it (the current slug stays).
  - `restore_note` (at most 1000 characters) is stored as `editor_note`.
- **Access:**
  - `post.revisions.view` (list and get) and `post.revisions.restore` are seeded for admin,
    editor, and author.
  - `post.revisions.view_all` is seeded for admin and editor; it covers posts the caller does not
    author. Without it, another author's post returns 404.
  - Revisions of trashed posts are not reachable over HTTP until the post is undeleted.
- **Lists:** newest first. Filters are `author_id`, `from_date`, and `to_date` (RFC 3339, or
  `YYYY-MM-DD`, where `to_date` covers the whole day), plus `include_diff`. List rows omit the
  snapshot; `GET …/revisions/{id or number}` returns it.
- **Retention:** revisions are kept until an admin runs `app revisions prune --keep N`, which keeps
  the newest N (at least 1) of every post. Pruning a restore's source sets
  `restore_from_revision_id` to null.
