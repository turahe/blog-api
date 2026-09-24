# Post SEO Backend

## Hexagonal Placement

- Domain package under `internal/core/post/seo`
  - value objects: `PostSEOConfig`, `RobotsFlags`, `SocialCardConfig`, `CanonicalURL`, `Slug`
  - domain errors: invalid slug, duplicate slug, invalid canonical, invalid image reference, seo limit exceeded
- Ports
  - inbound: `PostSEOPort` (update seo, generate preview, render meta tags response)
  - outbound: `PostSEORepository` (persist seo), `SlugUniquenessChecker`, `SEORenderHelper`
- Service:
  - `PostSEOService`
    - `UpdatePostSEO(ctx, post_id, actor, seo_patch)`
    - `GetSEO(ctx, post_id)`
    - `PreviewSEO(ctx, seo_draft)` — returns rendered search / OG / Twitter preview payload for live editor preview
    - `RenderSEOMeta(ctx, post_id)` — returns structured meta dictionary for frontend SSR/API

Adapters:

- inbound HTTP under admin/posts SEO endpoints
- outbound repo via GORM table `post_seo` (or embedded JSONB in posts; see Database Notes below)

## Storage Model

Two recommended approaches; use whichever is consistent with your schema choices:

Option A: separate `post_seo` table with one-to-one link to posts.

Table: `post_seo`

- id (bigint identity PK), uuid (unique public id)
- post_id (FK -> posts.id, unique, indexed)
- seo_title
- seo_description
- seo_keywords JSONB (array of strings)
- og_title
- og_description
- og_image_id nullable FK -> media_assets.id
- og_url nullable
- twitter_card enum summary, summary_large_image, app, player, default null (use site default)
- twitter_title
- twitter_description
- twitter_image_id nullable FK -> media_assets.id
- twitter_creator
- canonical_url nullable
- robots_noindex boolean default false
- robots_nofollow boolean default false
- created_at
- updated_at

Option B: store SEO fields directly on posts table for simplicity, especially if there are not many fields.

This document assumes Option A is preferred (keeps posts table cleaner), but Option B works too if desired.

Indexes:

- `unique(post_id)` on post_seo
- `index(posts.slug)` unique constraint on posts (if not already there)
- `index(og_image_id)` and `index(twitter_image_id)` if needed for joins

## Validation Rules

- `seo_title`: max 200 chars; warning at 60 chars (soft validation returned in response warnings array, not hard error)
- `seo_description`: max 500 chars; warning at 160 chars
- `seo_keywords`: max 20 items; each trimmed, max 60 chars, no duplicate empty strings
- `slug`:
  - regex allowlist: `^[a-z0-9]+(?:-[a-z0-9]+)*$`; max 200 chars, min 1
  - uniqueness check across posts (with optional scope like category_id if you implement per-category duplicate slugs)
  - reserved slugs rejected (admin, api, health, etc.)
  - on change: optionally emit `blog.post.slug_changed` event for redirect management
- `canonical_url`:
  - absolute URL, allowlisted origin or setting-allowed hosts
  - length limits
- `og_image_id` and `twitter_image_id`: must exist, content-type image/*
- `twitter_creator`: if set, regex `@[A-Za-z0-9_]{1,15}`
- `robots_noindex`, `robots_nofollow`: strict booleans

## Endpoints

### Admin endpoints

All under `/api/v1/admin/posts/{post_id}/seo`

#### `GET /api/v1/admin/posts/{post_id}/seo`

Requires auth + `post.seo.view` or ownership permission.

Response: current SEO config, plus any soft warnings for preview.

#### `PUT /api/v1/admin/posts/{post_id}/seo`

Full replace or patch semantics; recommended: partial update with explicit fields allowlist.

Request body:

```json
{
  "seo_title": "Post SEO Title",
  "seo_description": "Post SEO description",
  "seo_keywords": ["a", "b"],
  "slug": "my-new-post-slug",
  "og_title": "...",
  "og_description": "...",
  "og_image_id": "uuid or null",
  "og_url": "https://example.com/posts/my-new-post-slug",
  "twitter_card": "summary_large_image",
  "twitter_title": "...",
  "twitter_description": "...",
  "twitter_image_id": "uuid or null",
  "twitter_creator": "@handle",
  "canonical_url": "...",
  "robots_noindex": false,
  "robots_nofollow": false
}
```

Validation:
- allowlisted fields
- soft warnings returned in `meta.warnings` array

Response: updated SEO plus optional warnings array.

#### `POST /api/v1/admin/posts/{post_id}/seo/preview`

Live preview endpoint. Accepts a draft SEO object (maybe unsaved) and returns rendered search/OG/Twitter preview.

Request body: same fields as PUT (all optional) plus draft post title/excerpt etc.

Response:

```json
{
  "data": {
    "search_preview": {
      "title": "...",
      "url": "...",
      "description": "..."
    },
    "og_preview": {
      "title": "...",
      "description": "...",
      "image_url": "...",
      "url": "..."
    },
    "twitter_preview": {
      "card": "summary_large_image",
      "title": "...",
      "description": "...",
      "image_url": "...",
      "creator": "@handle"
    },
    "warnings": [
      {
        "field": "seo_title",
        "code": "too_long_recommended",
        "message": "SEO title is longer than the recommended 60 chars and may be truncated in search results"
      }
    ]
  },
  "meta": {
    "request_id": "..."
  },
  "error": null
}
```

### Public rendering helpers

#### `GET /api/v1/posts/{slug}/seo-meta`

Optional public endpoint that returns structured SEO meta for frontend SSR to render tags.

Security: public but only for published posts. Cached.

Response:

```json
{
  "data": {
    "title": "...",
    "meta_description": "...",
    "meta_keywords": "...",
    "canonical": "...",
    "robots": "noindex,nofollow",
    "open_graph": {
      "og:type": "article",
      "og:title": "...",
      "og:description": "...",
      "og:image": "...",
      "og:url": "...",
      "article:published_time": "...",
      "article:modified_time": "...",
      "article:tag": ["..."]
    },
    "twitter": {
      "twitter:card": "summary_large_image",
      "twitter:title": "...",
      "twitter:description": "...",
      "twitter:image": "...",
      "twitter:creator": "@..."
    }
  },
  "meta": {"request_id": "..."}
}
```

## Rendering Fallbacks

For any field not set explicitly, render according to fallback chain:

- Title: `post_seo.seo_title` → `post.title` + `settings.site.seo_title_template`
- Description: `seo_description` → `post.excerpt` → `settings.site.default_seo_description`
- OG image: `og_image_id` → `post.cover_image_media_id` → `settings.seo.default_social_share_media_asset_id`
- Canonical URL: `canonical_url` → default canonical pattern (e.g. `baseUrl + /posts/slug`)
- Twitter card: default to the site default Twitter card type if not set per post.

## Events

- `blog.post.seo.updated`
  - post_id, actor_id, changed_fields
  - used for cache invalidation, sitemap regenerators, redirect generators
- `blog.post.slug_changed`
  - post_id, old_slug, new_slug, actor_id
  - optional consumer to create redirect rules or update any other URL references.

Transactional outbox via Watermill.

## RBAC

- `post.seo.view` / `post.seo.edit`
- `post.slug.edit` (separate permission, combined into edit for most admins)

## Caching

- SEO meta endpoints (`/seo-meta`) cache aggressively; invalidate on:
  - post update
  - seo update
  - post status change
  - site default SEO settings changes

## Integration with Post Revision History

Every SEO update must:
- trigger a new revision (if integrated per `docs/backend/post-versions.md`)
- diff SEO fields and include them in changelog_text and diff_jsonb
- restorable as part of the full revision snapshot

## Testing

- validation: slug duplicate returns 422 with clear code, image FK violation returns 422, canonical URLs fail origin allowlist
- preview: all fallback chains produce correct output
- public seo-meta endpoint: only published posts return data; unpublished drafts return 404 or noindex depending on setting
- seo updates emit events and trigger cache invalidation
- restoring a revision restores seo snapshot exactly
