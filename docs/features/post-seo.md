# Post SEO Configuration

## Feature Summary

Each post has dedicated SEO configuration fields so editors can customize how the post appears in search engines and social shares. The editor UI exposes these fields in a dedicated SEO panel and includes search and social preview widgets to help editors understand the effect of their changes before publishing.

## In Scope

- meta title, meta description, meta keywords
- editable slug for clean URLs, plus uniqueness validation
- SEO preview of how the post appears in search engine results (title, description, slug/path display)
- Open Graph (OG) tags: `og:title`, `og:description`, `og:image`, `og:url`, `og:type`, and site-level defaults overrides per post
- Twitter Card tags: `twitter:card`, `twitter:title`, `twitter:description`, `twitter:image`, `twitter:creator`
- canonical URL support per post with fallback to default canonical rules
- noindex / nofollow toggles per post; mapped to `X-Robots-Tag` and `robots` meta when published
- integration with the Post editor UI and version history feature: edits to SEO fields produce changelog entries and historical revisions
- responsive editor UI for SEO panel; works on mobile, tablet, and desktop screen widths

## Out of Scope

- advanced schema.org JSON-LD markup generators (can be added later, default schema per template is fine)
- SEO auditing tooling that runs in the backend (can be added later)

## User Roles

- `author` — set SEO settings for their own posts
- `editor` — set SEO settings for all posts
- `admin` — set SEO settings for all posts and manage site-level SEO defaults via settings
- `superadmin` — same as admin, plus visibility into impersonation-attributed changes

## Permissions

- `post.seo.edit` — allowed to modify SEO fields for a post
- `post.seo.view` — allowed to view SEO fields on a post (default: editors/authors of the post; admins view any)
- `post.slug.edit` — allowed to edit post slug; separate permission since slugs are high-impact for URLs
- `settings.seo.manage` — admin/settings-level defaults (see `docs/features/settings-management.md`)

## SEO Fields Per Post

- `seo_title`
  - max length guideline (default 60 characters, soft warning)
  - default fallback: `title + " | " + site_title_template`
- `seo_description`
  - max length guideline (default 160 characters, soft warning)
  - default fallback: post `excerpt` if present, else site description
- `seo_keywords`
  - array of strings (max items; default 20)
- `slug`
  - editable before and after publish (with permission check)
  - uniqueness validated across posts (can allow duplicates when combined with category/date scope)
- `og_title`
  - overrides `seo_title` for OG when set
- `og_description`
  - overrides `seo_description` for OG when set
- `og_image_id`
  - FK -> `media_assets.id` (nullable); preferred social share image for OG
- `og_url`
  - override for `og:url` if different from default canonical URL
- `twitter_card`
  - enum: `summary`, `summary_large_image`, `app`, `player`
- `twitter_title`
  - overrides title for Twitter card
- `twitter_description`
  - overrides description for Twitter card
- `twitter_image_id`
  - FK -> `media_assets.id` (nullable); share image for Twitter
- `twitter_creator`
  - @username for Twitter creator
- `canonical_url`
  - string or null; if not set, defaults derived from canonical rules per post
- `robots_noindex`
  - boolean; default false
- `robots_nofollow`
  - boolean; default false

## SEO Preview Widgets

Editor panel provides:

- **Search result preview**:
  - displays rendered title, URL/slug line, description
  - truncation indicators based on character/width guidelines
  - mobile and desktop preview toggles
- **Open Graph preview**:
  - shows OG title, description, image thumbnail, site name
- **Twitter card preview**:
  - summary or summary_large_image
- **Previews update live as fields are edited**; server-side helper can also render preview when requested but the frontend can do it immediately.

## Editor UI and Responsive Design

SEO panel integrates into the post editor alongside content/settings/versions tabs:

- On desktop: side panel alongside editor with horizontal layout or tabbed SEO tab
- On tablet/phone: SEO settings collapse beneath the editor
- Accessibility: labels, aria-live previews, keyboard navigable, high contrast

## Meta Rendering Rules

When a published post is rendered on the frontend (or rendered server-side via this backend):

- Render `<title>` from `seo_title` fallback chain: post.seo_title → post.title + site template
- Render `<meta name="description">` from `seo_description` fallback chain: post.seo_description → post.excerpt → site description
- Render `<meta name="keywords">` only when `seo_keywords` is non-empty
- Render OG tags:
  - `og:title`, `og:description` use OG overrides first, then SEO fallbacks, then content fallbacks
  - `og:image` resolved from `og_image_id` → `cover_image_media_id` → default social image from settings
  - `og:url` defaults to canonical url if set, else post permalink
  - `og:type = article`, plus optional article metadata (published_time, modified_time, author, tags, section)
- Render Twitter card tags:
  - `twitter:card` = post.twitter_card or site default
  - title/description/image fallbacks similar to OG
  - `twitter:creator` when set
- Render `<link rel="canonical">` per post: canonical_url if set, else default canonical URL rule
- Render `<meta name="robots">` or response header `X-Robots-Tag` combining noindex/nofollow:
  - `noindex` if robots_noindex true
  - `nofollow` if robots_nofollow true
- SEO tags for drafts/unpublished posts should default to noindex unless explicitly overridden by an admin setting.

## Validation Rules

- `seo_title` length warning at 60 chars, hard max 200
- `seo_description` length warning at 160 chars, hard max 500
- `seo_keywords` max 20 items, each trimmed, length max 60 chars
- `slug`:
  - allowed characters: lowercase letters, numbers, hyphens; length 1-200
  - uniqueness validated per scope
  - reject reserved slugs
- `canonical_url`: must be absolute URL and on the same origin unless a setting allows cross-origin canonical (default disabled)
- `og_image_id` and `twitter_image_id`: if present, FK must exist and content-type be image/*
- `twitter_creator`: if set, must match @username pattern

## Changelog and Version History Integration

Editing any SEO field:

- triggers new post revision entry
- changelog lists changed SEO fields
- restorable via version history
- if post slug changes, an event is emitted to redirect caches and/or URL redirect rules can be generated (optional redirect rule stored per implementation policy).

## End-to-End Test Expectations

- saving SEO settings updates stored values and appears in preview correctly
- editing slug validates uniqueness; duplicate slug returns validation error
- publishing post causes frontend or SSR to render title/description/keywords, canonical, OG, Twitter card, and robots meta correctly
- setting noindex/nofollow results in proper meta tag or response header
- SEO field changes are captured in post revision history with a changelog
- restoring a revision restores SEO fields from that revision
- editor UI preview and SEO panel responsive on mobile/tablet/desktop; access labels and keyboard navigation work

## Documentation for Content Editors

Content editor guide includes:

- how to fill SEO fields
- best practices for titles, descriptions, keywords, canonical URLs
- when to use noindex/nofollow
- how social share previews work
- how slug affects URLs and SEO
- how revision history can help revert bad SEO changes
