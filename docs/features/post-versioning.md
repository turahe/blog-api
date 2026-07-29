# Post Version History

## Feature Summary

Every edit made to a blog post must be recorded as a versioned revision so editors can review history, compare versions, restore any past revision, and read a human-readable changelog of what changed between revisions.

This feature pairs with the post SEO configuration feature in `docs/features/post-seo.md` so content editors have the ability to edit posts content + SEO settings, review changes in one post editor, confident that any mistake can be reverted with the posthanged.

## In Scope

- create a new revision whenever a post is created or updated
- store every revision stores full snapshot of: title, slug, excerpt, content, SEO settings, media associations, cover image, categories tags), plus per-field diffs + change summary/changelog
- capture author of the user who created the revision
- allow editors and admins to list/view the full version list for a post, filter by author or revision number/date, view an old revision content with the current revision)
- allow restore of a previous revision as the active post; restore must create a new revision preserving all revisions are always append)
- changelog entry on publish, review and restore operations (create, update, restore)

## Out of Scope

- branching/merging multiple concurrent (first revision model only linear linear revision history)
- manual revision branches, diff UI highlights of rich-text rendered content-level HTML exact character-by-character diffs
- full-text searching revision content storage of large revisions)

## User Roles

- `author`
  - create revisions for their own posts
  - view and restore revisions for their own posts
- `editor`
  - edit/view/restore revisions for all posts
- `admin`
  - same as editor, plus ability to see audit meta changes (publishing, deletion, restore) for all posts
- `superadmin`
  - same as admin, plus ability to see impersonation-attributed revisions

## Permissions

Permission Keys

- `post.revisions.view`
- `post.revisions.restore`
- `post.revisions.view_all` (admin view any post; author/editor default to their access)

## Editor UI

Post editor includes:

- a sidebar panel "Versions" showing the revision history
- revision card in panel (compact, author, time, status, changelog summary)
- compare or open old (diff between selected and current revision number, time author status, changelog, content, media, category, tag, SEO)
- restore confirmation modal showing summary of changes that will be restored
- responsive layout switches to grid system responsive: panel collapsible on narrow screens)
- changelog summary)
- responsive accessible labels that describe content diff or summary) changelog entry; accessible semantic heading)

## Versioning Model

- create
- first revision for new post created
- every update creates next major/minor automatic; each save) in post updated)
- restore creates a new revision copying the restored content and marks it as type=restore; original revision never mutated)
- restore operation fails if the current published)
- store diffs per field title, slug, excerpt, content, seo settings, tags, media, category, cover)
- changelog per changelog summary, per-field differences; auto-generated and optionally manual changelog free-text note entered at save by the editor (optional))

## View Versions Changelog Requirements

Each revision changelog contains:

- revision number
- created_at timestamp
- author user id and display name
- revision type: create / update / restore / publish / archive auto-draft status transitions)
- changed_fields list
- diff summary human readable diff)
- optional manual edit note entered by editor
- restore_from_revision_id populated for restores)

## Retention Rules

- revisions are kept permanently or soft-deleted; never physically deleted until admin explicitly pruned for
- restore creates a new revision; old revision still visible in history

## Access & Governance

- revisions are never mutated; append-only
- authors see revisions only for their own posts; editors see all revisions
- restore creates events audited with who restored, from what revision, and when
- restore operations do not change the post's publish status automatically unless explicitly specified in the restore flow; default is to restore only content + SEO + meta as draft, and leave the post status

## End-to-End Test Expectations

- editing a post creates a new revision with correct author + snapshot + diff
- viewing revision list returns the expected revision
- restoring a revision produces a new revision restoring content and rolls the post content back; original revision id remains
- revision changelog matches changed fields
- authors cannot view/restore revisions for posts they don't own unless they have editor/admin permissions
- responsive version panel fits correctly on mobile, tablet, desktop; accessible via keyboard + screen readers
