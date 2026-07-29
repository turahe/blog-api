# Media Asset Associations

## Purpose

This document standardizes how core entities (`users`, `posts`, `categories`) relate to `media_assets` for avatars, cover images, and attached post media. It defines relationships, FK constraints, validation, migration steps, API contracts, and query flows so documentation, API, tests, and migrations stay consistent.

## Relationships

### User ↔ MediaAsset (Avatar)

- cardinality: `users (1) -- (0..1) media_assets`
- ownership: a user owns at most one active avatar reference; one media asset may be reused across users only when the application explicitly allows it
- FK column: `users.avatar_id → media_assets.id`
- delete behavior: `ON DELETE SET NULL`
- semantics:
  - `avatar_id` is nullable (no avatar = legacy/placeholder only)
  - changing `avatar_id` does **not** delete the old `media_asset`
  - media deletion (soft/hard) clears referencing `avatar_id` through FK/rule

### Post ↔ MediaAsset (Attached Images)

- cardinality: `posts (1) -- (0..N) post_media -- (1) media_assets`
- FK columns in join table `post_media`:
  - `post_id → posts.id`
  - `media_asset_id → media_assets.id`
- delete behavior:
  - deleting a post cascades `post_media` rows
  - deleting a `media_asset` cascades or nullifies `post_media` references depending on policy; default recommended: `ON DELETE CASCADE` on join rows only
- join attributes:
  - `kind`: enum `cover | inline_image | attachment` (extendable)
  - `sort_order`: ordering for galleries/inline sequences
  - `created_at`: attach timestamp
- uniqueness:
  - composite unique recommended for `(post_id, media_asset_id, kind)` to avoid repeated same-kind duplicates when appropriate, or relaxed to allow ordering variants per product needs
- semantics:
  - a post may have many attachments (inline images, docs, cover)
  - the post’s primary cover image is stored either as `posts.cover_image_media_id` for direct O(1) cover lookup, or inferred from `post_media.kind=cover`; both must be kept consistent

### Post Cover Image (Convenience FK)

- FK column: `posts.cover_image_media_id → media_assets.id`
- delete behavior: `ON DELETE SET NULL`
- rule for consistency:
  - when `post_media` has a `kind=cover` entry, services should synchronize `posts.cover_image_media_id`
  - when only one of the two is allowed to be authoritative, pick `post_media` as source of truth and populate `cover_image_media_id` as a read-optimized denormalization

### Category ↔ MediaAsset (Cover Image)

- cardinality: `categories (1) -- (0..1) media_assets`
- FK column: `categories.image_id → media_assets.id`
- delete behavior: `ON DELETE SET NULL`
- semantics:
  - `image_id` nullable (category may have no cover)
  - soft-deleted or hard-deleted media clears the reference

## Referential Integrity Rules

- all FKs to `media_assets.id` must validate on write
- referenced media must be:
  - not soft-deleted (where soft delete is enforced)
  - belong to an allowed `content_type` set per field (avatar/image categories and post attachment categories enforce image content types)
- orphan `post_media` rows must not exist for deleted posts or media
- foreign keys and indexes in PostgreSQL:
  - `users.avatar_id` btree index + FK
  - `categories.image_id` btree index + FK
  - `posts.cover_image_media_id` btree index + FK
  - `post_media(post_id, sort_order)` for ordered retrieval
  - `post_media(media_asset_id)` to quickly find usages of an asset across posts
  - optionally, `post_media(post_id, kind)` when queries filter by kind

## Migration Guidance

For migrations executed via `app migrate up`:

1. Add nullable FK columns:
   - `users.avatar_id`
   - `categories.image_id`
   - `posts.cover_image_media_id`
2. Create join table `post_media` with FKs, `kind`, `sort_order`, `created_at`
3. Add indexes
4. Optional data backfill for legacy `avatar_url`/`cover_image_url` references if/when those are migrated into `media_assets` (documented data migration, not auto-inferred)
5. Add constraints for `kind` allowlist via check constraint or validation layer depending on project preference

Rollback (`app migrate down`):

1. drop indexes and join table
2. drop new FK columns
3. restore any prior defaults or invariants if needed.

## Validation

### User Avatar Validation

- `avatar_id` must be a valid UUID that points to an existing `media_assets` row
- `content_type` must be image/* whitelist (image/png, image/jpeg, image/webp, image/avif, etc.)
- if configured, uploaded_by permission and ownership checks can be enforced by services

### Post Attachments Validation

- every item in create/update payload must reference existing media_assets
- `kind` must be in allowlist
- `sort_order` must be an integer in safe range
- only one `kind=cover` per post (or explicitly allow multiple depending on schema rule)
- `cover_image_media_id` must either be null or equal to a `kind=cover` entry in `post_media` (consistency rule)

### Category Image Validation

- `image_id` must exist in media_assets when non-null
- restrict `content_type` to image/* allowlist

## API Contracts

### GET Endpoints

- `GET /api/v1/me` returns `avatar` object or `avatar_id` + optional href:
  - `avatar_id`
  - `avatar_url`
  - `avatar_transform_template` or links to available transforms
- `GET /api/v1/posts/:slug` or admin post read endpoint returns:
  - `cover_image_media_id` and/or `cover` media object
  - `media` array from `post_media` in `sort_order` with kind tags
- `GET /api/v1/categories` / `GET /api/v1/categories/:slug` returns:
  - `image_id` and/or `image` object

Include options to include/exclude media associations via query param `include=cover,media,avatar` to avoid N+1 and keep responses lean.

### Write Endpoints

- `PATCH /api/v1/me/profile` may accept `avatar_id` (null to clear)
- admin `POST/PATCH /api/v1/admin/posts` may accept:
  - `cover_image_media_id` scalar
  - `media` array with `media_asset_id`, `kind`, `sort_order`
- admin `POST/PATCH /api/v1/admin/categories` may accept `image_id`
- write endpoints must:
  - validate existence and content type
  - update join rows transactionally (replace semantics or append semantics clearly documented)
  - emit events for association changes

## Domain/Integration Events

- `blog.user.avatar_updated` — `{user_id, previous_media_asset_id, new_media_asset_id}`
- `blog.post.media_updated` — `{post_id, added:[...], removed:[...], cover_changed:bool}`
- `blog.category.image_updated` — `{category_id, previous_media_asset_id, new_media_asset_id}`
- `blog.media.asset_referenced` / `blog.media.asset_unreferenced` for governance or retention analytics (optional)

Use outbox pattern for publication consistency.

## Common Queries

- fetch user with avatar: join users on media_assets
- fetch post with cover + ordered attachments: join posts on post_media (order by sort_order) + media_assets
- fetch categories list with image: join categories on media_assets
- find all usages of a media_asset_id:
  - select from users where avatar_id = $1
  - select from categories where image_id = $1
  - select from posts where cover_image_media_id = $1
  - select from post_media where media_asset_id = $1

Cache guidance: materialize includes where necessary; invalidate relevant user/post/category cache entries when media associations change.

## Testing Coverage

- migration tests: up/down reversible and idempotent
- FK constraint tests:
  - delete a media_asset referenced as user avatar → avatar_id becomes null
  - delete a media_asset referenced as category image → image_id becomes null
  - delete a media_asset referenced in post_media → join row behavior follows policy
  - delete a post → post_media rows removed
- validation tests:
  - invalid avatar_id/image_id rejected with 422
  - invalid kind in post_media rejected
  - multiple covers disallowed (if rule enforced)
- API tests:
  - GET endpoints return expected media associations for user, post, category
  - write endpoints update associations and return correct updated state
  - include query param toggles media payload correctly
