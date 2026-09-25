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

- `seoTitle`: max 200 chars; warning at 60 chars (soft validation returned in response warnings array, not hard error)
- `seoDescription`: max 500 chars; warning at 160 chars
- `seoKeywords`: max 20 items; each trimmed, max 60 chars, no duplicate empty strings
- `slug`:
  - regex allowlist: `^[a-z0-9]+(?:-[a-z0-9]+)*$`; max 200 chars, min 1
  - uniqueness check across posts (with optional scope like category_id if you implement per-category duplicate slugs)
  - reserved slugs rejected (admin, api, health, etc.)
  - on change: optionally emit `blog.post.slug_changed` event for redirect management
- `canonicalUrl`:
  - absolute URL, allowlisted origin or setting-allowed hosts
  - length limits
- `ogImageId` and `twitterImageId`: must exist, content-type image/*
- `twitterCreator`: if set, regex `@[A-Za-z0-9_]{1,15}`
- `robotsNoindex`, `robotsNofollow`: strict booleans

## Endpoints

### Admin endpoints

All under `/api/v1/admin/posts/{postId}/seo`

#### `GET /api/v1/admin/posts/{postId}/seo`

Requires auth + `post.seo.view` or ownership permission.

Response: current SEO config, plus any soft warnings for preview.

#### `PUT /api/v1/admin/posts/{postId}/seo`

Full replace or patch semantics; recommended: partial update with explicit fields allowlist.

Request body:

```json
{
  "seoTitle": "Post SEO Title",
  "seoDescription": "Post SEO description",
  "seoKeywords": ["a", "b"],
  "slug": "my-new-post-slug",
  "ogTitle": "...",
  "ogDescription": "...",
  "ogImageId": "uuid or null",
  "ogUrl": "https://example.com/posts/my-new-post-slug",
  "twitterCard": "summary_large_image",
  "twitterTitle": "...",
  "twitterDescription": "...",
  "twitterImageId": "uuid or null",
  "twitterCreator": "@handle",
  "canonicalUrl": "...",
  "robotsNoindex": false,
  "robotsNofollow": false
}
```

Validation:
- allowlisted fields
- soft warnings returned in `meta.warnings` array

Response: updated SEO plus optional warnings array.

#### `POST /api/v1/admin/posts/{postId}/seo/preview`

Live preview endpoint. Accepts a draft SEO object (maybe unsaved) and returns rendered search/OG/Twitter preview.

Request body: same fields as PUT (all optional) plus draft post title/excerpt etc.

Response:

```json
{
  "data": {
    "searchPreview": {
      "title": "...",
      "url": "...",
      "description": "..."
    },
    "ogPreview": {
      "title": "...",
      "description": "...",
      "imageUrl": "...",
      "url": "..."
    },
    "twitterPreview": {
      "card": "summary_large_image",
      "title": "...",
      "description": "...",
      "imageUrl": "...",
      "creator": "@handle"
    },
    "warnings": [
      {
        "field": "seoTitle",
        "code": "too_long_recommended",
        "message": "SEO title is longer than the recommended 60 chars and may be truncated in search results"
      }
    ]
  },
  "meta": {
    "requestId": "..."
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
    "metaDescription": "...",
    "metaKeywords": "...",
    "canonical": "...",
    "robots": "noindex,nofollow",
    "openGraph": {
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
  "meta": {"requestId": "..."}
}
```

## Rendering Fallbacks

For any field not set explicitly, render according to fallback chain:

- Title: `post_seo.seo_title` → `post.title` + `settings.site.seo_title_template`
- Description: `seoDescription` → `post.excerpt` → `settings.site.default_seo_description`
- OG image: `ogImageId` → `post.cover_image_media_id` → `settings.seo.default_social_share_media_asset_id`
- Canonical URL: `canonicalUrl` → default canonical pattern (e.g. `baseUrl + /posts/slug`)
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

## Implementation

What shipped, where it differs from the design above:

- **Placement.** SEO lives in the post module rather than a separate `post/seo` package:
  - domain: `internal/core/post/domain/seo.go` (`SEO`, `SEOPatch`, `Validate`, `Warnings`, `RenderSEO`, `PreviewOf`);
  - service: `internal/core/post/service/seo.go` (`GetSEO`, `UpdateSEO`, `PreviewSEO`, `SEOMeta`), enabled with `PostService.WithSEO`.
- **Storage.** Option A: the `post_seo` table (migration `00025_post_seo.sql`). A row exists only once a post's SEO was edited, and empty text means "use the fallback". Image references are `ON DELETE SET NULL`.
- **Update semantics.** `PUT /admin/posts/{id}/seo` is a partial update:
  - omitted fields stay unchanged, `""` clears a text field, and `null` clears an image;
  - text is trimmed and whitespace-collapsed, and keywords are de-duplicated case-insensitively;
  - a patch that changes nothing writes nothing and emits no events.
- **Validation.** Every invalid field is returned together as `422` with `error.details` = `[{field, code, message}]`.
  - Codes: `too_long`, `too_many`, `invalid_format`, `markup_not_allowed`, `host_not_allowed`, `not_found`, `not_image`, `slug_taken`, `slug_reserved`.
  - Text containing `<` or `>` or control characters is rejected rather than escaped.
  - `canonicalUrl` and `ogUrl` must use the host of `site.canonical_base_url` (else `site.public_url`) or a host in `seo.canonical_allowed_hosts`. When neither base URL is set, any http(s) host is accepted.
  - Images must be ready, undeleted `image/*` media.
  - Title and description lengths over 60 and 160 characters are advisory `warnings` (`too_long_recommended`), not errors.
- **Slug.** Changing `slug` requires `post.slug.edit`; without it the request is `403`. Resubmitting the current slug is allowed. A slug change bumps the post version and emits `blog.post.slug_changed`. Redirect rules are left to consumers of that event.
- **Transaction.** The slug update, the `post_seo` upsert, the revision (type `update`), and the outbox events are written in one transaction.
  - Revisions snapshot SEO in `seo_snapshot` and diff it per field: `diff.seo = {field: {from, to}}`.
  - Restoring a revision restores its SEO, dropping images that no longer exist (they are listed in `skipped.media`).
- **Rendering.** `GET /posts/{slug}/seo-meta` returns rendered meta for published posts only; anything else is `404`.
  - Title: `seoTitle`, else `seo.title_template` applied to the plain-text post title.
  - Description: `seoDescription`, else the excerpt, else a 160-character plain-text content summary, else `seo.default_description`.
  - Canonical URL: `canonicalUrl`, else `{canonical base}/posts/{slug}`.
  - `og:image`: the OG image, else the cover image, else `seo.default_share_image_url`.
  - Twitter values fall back to the OG values.
  - Derived values have Markdown and HTML stripped.
  - `X-Robots-Tag` is sent when the post is `noindex` or `nofollow`.
  - Responses are cached in the posts read-cache family, which every post write invalidates.
- **Image URLs.** Signed storage URLs expire and would break cached social cards, so images use a stable URL:
  - with imgproxy configured: `{APP_PUBLIC_URL}/api/v1/media/{id}/transform?w=W&format=jpeg`, where W is the largest `MEDIA_TRANSFORM_WIDTHS` entry up to 1200;
  - otherwise: `S3_PUBLIC_BASE_URL/{storage_key}`;
  - with neither, no image URL is emitted.
- **Permissions.** `post.seo.view` covers GET and preview, `post.seo.edit` covers PUT, and `post.slug.edit` covers slug changes. All three are seeded for admin, editor, and author. Authors reach only their own posts; other posts are `404`.
