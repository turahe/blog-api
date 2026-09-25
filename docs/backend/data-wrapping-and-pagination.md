# Data Wrapping and Pagination

List endpoints use a Laravel-style length-aware paginator shape inside the project envelope.

Non-list responses stay `{ ok, data, meta, error }` with `meta.requestId` only.
Paginated lists add `links` and expand `meta`.

## Paginated success shape

```json
{
  "ok": true,
  "code": 2000201,
  "data": [
    {
      "id": 1,
      "name": "Eladio Schroeder Sr.",
      "email": "therese28@example.com"
    },
    {
      "id": 2,
      "name": "Liliana Mayert",
      "email": "evandervort@example.com"
    }
  ],
  "links": {
    "first": "http://example.com/users?page=1",
    "last": "http://example.com/users?page=1",
    "prev": null,
    "next": null
  },
  "meta": {
    "requestId": "…",
    "currentPage": 1,
    "from": 1,
    "lastPage": 1,
    "path": "http://example.com/users",
    "perPage": 15,
    "to": 10,
    "total": 10
  }
}
```

Numeric `code` packing is documented in [response-codes.md](response-codes.md).

## Rules

| Field | Meaning |
| --- | --- |
| `data` | Array of resources for the current page (not `{ items: [...] }`) |
| `links.first` / `links.last` | Absolute URLs for page 1 and the last page; other filters preserved |
| `links.prev` / `links.next` | Absolute URLs or `null` at the ends |
| `meta.currentPage` | 1-based page index |
| `meta.perPage` | Page size |
| `meta.total` | Total matching rows |
| `meta.lastPage` | `ceil(total / perPage)` (minimum 1) |
| `meta.from` / `meta.to` | 1-based inclusive item indices on this page, or `null` when empty |
| `meta.path` | Absolute path without the query string |
| `meta.requestId` | Correlation id (project addition; not part of Laravel’s default meta) |

## Query parameters

- `page` — 1-based (default `1`)
- `perPage` — page size (endpoint-specific defaults/caps)

## Implementation

Handlers call `successPaginated` in
[response.go](../../internal/adapters/inbound/http/response.go).
OpenAPI schemas: `PaginationLinks`, `PaginationMeta`, and `PaginatedEnvelope` in
the envelope schemas documented in [api-contracts.md](../architecture/api-contracts.md).

Errors still use `{ ok: false, meta, error }` with no `links`.
