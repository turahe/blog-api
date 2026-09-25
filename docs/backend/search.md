# Post Search

Full-text search over published posts uses PostgreSQL, behind the `postports.Searcher` port
(adapter: `persistence.PostSearch`). Swapping in an external engine means a new adapter plus a
worker consumer on `blog.post.revision.created`, which fires on every post write.

## Request

`GET /api/v1/posts?q=<query>` switches the public post list to search. `q` is trimmed, runs of
whitespace are collapsed, and it may be at most 200 characters (`400 validation_error`
otherwise). `page`, `perPage` (max 100), `categoryId`, and `tagId` work as on the plain list.
Without a search backend (the migration was not applied) the answer is `503 search.unavailable`.

`q` uses PostgreSQL web search syntax (`websearch_to_tsquery`):

| Input | Meaning |
| --- | --- |
| `go generics` | both words |
| `"go generics"` | the phrase |
| `go or rust` | either word |
| `go -rust` | `go` without `rust` |

Only published, non-deleted posts match. Results are cached like the plain list (Redis `posts`
family, invalidated by every post write).

## Index

`posts.search_vector` is a stored generated column, so it changes in the same statement as the
post and there is no indexing lag or worker dependency. It is weighted:

| Weight | Source |
| --- | --- |
| A | `title` |
| B | `excerpt` |
| C | first 100,000 characters of `content` (a tsvector is capped at 1 MB) |

A GIN index (`posts_search_vector_idx`) serves the match. Tags and category names are not
indexed; use `tagId` or `categoryId` to narrow results.

## Language

`SEARCH_LANGUAGE` (default `simple`) is the text search configuration. `simple` lowercases words
without stemming, so it works for any language but `running` does not match `run`. A language
configuration such as `english` or `indonesian` stems words and drops stop words.

The configuration is baked into the column. To change it, set `SEARCH_LANGUAGE` and run
`app search reindex`, which drops and re-adds the column and its index in one transaction. That
rewrites the `posts` table under an exclusive lock, so post reads and writes wait until it
finishes; run it in a quiet period. Restart `app serve` afterwards: `serve` reads the indexed
language at startup and always queries with it, logging a warning while it differs from
`SEARCH_LANGUAGE`.

## Relevance

Results are ordered by `ts_rank(search_vector, query, 1)`, which weighs title matches above
excerpt matches above content matches and divides by the log of the document length, so a short
post that matches beats a long one that mentions the term once. Ties fall back to newest
`publishedAt`, then id.

## Response

Each item is the usual post object plus:

```json
"search": {
  "rank": 0.0607927,
  "title": "All about <mark>generics</mark>",
  "snippet": "… type parameters make <mark>generics</mark> practical … "
}
```

- `rank` is only meaningful for ordering within one query.
- `title` is the whole title with every match marked.
- `snippet` is up to two fragments of 15–35 words from `content`, joined by ` … `. When only
  the title or excerpt matched, it is the start of the content without marks. Content is
  markdown, so a snippet can contain raw markdown syntax.
- Both are HTML-escaped; the only markup is `<mark>`, so clients may insert them as HTML.
