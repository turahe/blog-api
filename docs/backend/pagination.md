# Cursor-based Pagination

## Overview

The backend exposes every paginated list through a **shared pagination package**
(`internal/shared/pagination/`) that supports two modes:

1. **Cursor-based keyset pagination** (preferred) — uses opaque, signed cursors
   and `(sort_col_tuple) <op> (?, …)` row-value-tuple seeks to avoid the
   `OFFSET` performance cliff on deep pages. Works best for datasets with
   100,000+ rows.
2. **Legacy offset pagination** (`page` / `perPage`) — kept as a fallthrough
   path; internally rewritten to `LIMIT ? OFFSET ?` after the same parameter
   validation and envelope writer.

The response envelope for a paginated list **always** contains both cursor
fields and Laravel-style length-aware meta fields so frontends can pick either
protocol without changing deserialization.

## Request Parameters

All paginated endpoints accept the parameters below. Additional
endpoint-specific filters (`status`, `q`, `slug`, …) are documented per route.

| Name          | Type    | Default     | Description                                                                                                                                      |
| ------------- | ------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `after`       | string  | —           | Opaque **forward** cursor. Reads `limit` items *after* the item encoded in the cursor. Incompatible with `before` and `page`.                    |
| `before`      | string  | —           | Opaque **backward** cursor. Reads `limit` items *before* the item encoded in the cursor and reverses display order. Incompatible with `after`.   |
| `limit`       | integer | `20`        | Items per page. Alias: `perPage`. Always clamped to the endpoint's `MaxPerPage` (default `100`).                                                 |
| `perPage`     | integer | `20`        | Alias for `limit`. Only used when `limit` is not provided.                                                                                       |
| `includeTotal`| boolean | `false`     | When `true`, the repository runs an extra `COUNT(*)` with the same filters and sets `total`. Leave off (the default) to save a DB round-trip.    |
| `page`        | integer | `1`         | **Legacy offset mode.** Switches ParseRequest to `ModeOffset`; ignored when `after` or `before` is provided.                                     |

Precedence (first non-empty wins):

1. `after` → cursor, `Forward=true`
2. `before` → cursor, `Forward=false`
3. `page` or legacy PageRequest with `Page>0` → `ModeOffset`
4. otherwise → cursor mode on first page (no seek WHERE clause).

## Cursor Lifetime (TTL)

Cursors are issued with an embedded `issued_at` timestamp and rejected once
older than the endpoint's `CursorConfig.TTL`.

- Default: **`DefaultTTL = 24h`** (declared in
  `internal/shared/pagination/config.go:90`).
- Individual endpoints can override `CursorConfig.TTL` with a shorter or
  longer duration.
- TTL is validated inside `DecodeCursor` *after* signature verification, so a
  malformed or tampered timestamp still fails closed with a signature error.

Practical guidance: frontends should cache next/previous cursors for a session
but **not** persist them across days. A 410-style response is not returned;
expired cursors produce HTTP 400 with `pagination.cursor.expired` so the
caller can re-read the first page transparently.

## HMAC Signer

Every cursor produced by `EncodeCursor` is a **base64url(raw)** encoding of:

```
json({ kind, issued_at, fields: {...} }) || "|" || hex(hmac_sha256(payload))
```

`DecodeCursor` performs the reverse: base64url decode → split on the final `|`
→ verify the HMAC under either signing key → reject on mismatch, wrong kind,
expired TTL, or missing sort field / wrong runtime sort-field type.

### Environment configuration

The signer is loaded lazily on the first cursor op via
`pagination.GlobalSigner()` (`internal/shared/pagination/signer.go:65`). Key
resolution:

| Variable                        | Purpose                                                                                            |
| ------------------------------- | -------------------------------------------------------------------------------------------------- |
| `PAGINATION_HMAC_KEY`           | **Primary** signing key. Always used for new cursors. Highest precedence.                          |
| `APP_KEY`                       | Fallback primary key when `PAGINATION_HMAC_KEY` is not set.                                        |
| `PAGINATION_HMAC_KEY_PREVIOUS`  | **Verification-only** secondary key. Accepts cursors signed by the old key during key rotation.    |

If **neither** `PAGINATION_HMAC_KEY` nor `APP_KEY` is set, `NewSignerFromEnv`
returns an error and the first call panics the process — the API never
operates with unsigned cursors.

### Key rotation

To rotate the signing secret without invalidating every in-flight cursor:

1. Set `PAGINATION_HMAC_KEY_PREVIOUS = <old value>`.
2. Set `PAGINATION_HMAC_KEY = <new value>` (or replace `APP_KEY` accordingly).
3. Deploy. Both old and new cursors verify for `DefaultTTL` (24 h).
4. After `DefaultTTL` has elapsed, clear `PAGINATION_HMAC_KEY_PREVIOUS`.

## Error Code Reference

All pagination sentinels are declared in
`internal/shared/pagination/errors.go`. Handlers call
`pagination.ErrorCode(err)` + `pagination.ErrorCause(err)` and write an HTTP
400 with the code/message via `responses.Failure()`.

| Sentinel error               | `ErrorCode()` value           | HTTP | Meaning                                                                   |
| ---------------------------- | ----------------------------- | ---- | ------------------------------------------------------------------------- |
| `ErrCursorMalformed`         | `pagination.cursor.malformed` | 400  | Base64 decoding failed or the outer shape is not `payload\|hex-mac`.      |
| `ErrCursorInvalidSignature`  | `pagination.cursor.tampered`  | 400  | HMAC verification failed under both current + previous keys.             |
| `ErrCursorExpired`           | `pagination.cursor.expired`   | 400  | `issued_at + TTL < now`. Cursor is older than the endpoint allows.       |
| `ErrCursorWrongKind`         | `pagination.cursor.wrong_kind`| 400  | Cursor was issued for endpoint A but presented to endpoint B.            |
| `ErrCursorMissingField`      | `pagination.cursor.missing_field` | 400 | Decoded payload lacks one of the sort fields declared in `CursorConfig`. |
| `ErrCursorFieldType`         | `pagination.cursor.field_type`| 400  | Sort field value has the wrong runtime type (string vs int64, etc.).      |
| `ErrCursorUnsupported`       | `pagination.cursor.unsupported`| 400 | Endpoint is `OffsetModeOnly` but the caller sent `after`/`before`.       |

Frontends that want a unified UX can group every non-`expired` case as "please
reload the list from page one". `expired` can be retried automatically without
alerting the user.

## Laravel-style Validation Errors

Pagination inputs (`limit`, `page`, `includeTotal`, …) are parsed by
`ParseRequest`, which returns the cursor sentinels above (HTTP 400) on the
cursor-specific failure modes and the standard **Gin binding validation
errors** (HTTP 422) for route-level body/query binding.

Validation errors follow the project-wide Laravel-compatible envelope defined
in `internal/adapters/inbound/http/responses/envelope.go:28`:

```json
{
  "ok": false,
  "code": 4220001,
  "error": {
    "code": "validation_error",
    "message": "The given data was invalid.",
    "details": null
  },
  "meta": { "requestId": "req_01JXYZ" },
  "message": "The given data was invalid.",
  "errors": {
    "includeTotal": ["The includeTotal field must be true or false."],
    "limit":        ["The limit field must be between 1 and 100."]
  }
}
```

Key properties:

- Top-level `message` (string) and `errors` (`map[string][]string`) mirror
  Laravel 10+ — a frontend can read either `error.message` / `error.details`
  or the canonical Laravel fields.
- `errors` entries are always arrays (one or more messages per field) even
  when a single rule fails, matching Laravel's form validator output.
- Binding errors produced by `requests.BindJSON` and
  `requests.BindQuery` also share this shape.

## Response Shape

### Cursor-mode example (`GET /api/v1/admin/newsletter/subscribers?after=<cursor>&limit=2`)

```json
{
  "ok": true,
  "code": 2000000,
  "data": [
    { "id": "...", "email": "a@example.com", "status": "active", "createdAt": "2026-01-02T10:00:00Z" },
    { "id": "...", "email": "b@example.com", "status": "active", "createdAt": "2026-01-02T09:00:00Z" }
  ],
  "links": {
    "first": "/api/v1/admin/newsletter/subscribers?limit=2",
    "prev":  "/api/v1/admin/newsletter/subscribers?before=<prevCursor>&limit=2",
    "next":  "/api/v1/admin/newsletter/subscribers?after=<nextCursor>&limit=2",
    "last": null
  },
  "meta": {
    "requestId": "req_01JXYZ",
    "limit":           2,
    "hasNextPage":     true,
    "hasPreviousPage": true,
    "nextCursor":      "<nextCursor>",
    "previousCursor":  "<prevCursor>",
    "currentPage":     null,
    "lastPage":        null,
    "from":            null,
    "to":              null,
    "total":           null
  }
}
```

Notes:

- `links.last` is emitted only when `includeTotal=true` (the count is needed
  to compute the legacy last-page URL). `total` / `currentPage` / `lastPage` /
  `from` / `to` are always `null` in pure cursor mode.
- `nextCursor` is present if and only if `hasNextPage` is true; same for
  `previousCursor` / `hasPreviousPage`. The `links.next` URL carries the
  same cursor value for callers that want link-traversal semantics.

### Legacy offset-mode example (`GET /api/v1/admin/users?page=2&perPage=3&includeTotal=true`)

```json
{
  "ok": true,
  "code": 2000000,
  "data": [
    { "id": "...", "username": "bob"   },
    { "id": "...", "username": "carol" },
    { "id": "...", "username": "dan"   }
  ],
  "links": {
    "first": "/api/v1/admin/users?page=1&perPage=3&includeTotal=true",
    "prev":  "/api/v1/admin/users?page=1&perPage=3&includeTotal=true",
    "next":  "/api/v1/admin/users?page=3&perPage=3&includeTotal=true",
    "last":  "/api/v1/admin/users?page=12&perPage=3&includeTotal=true"
  },
  "meta": {
    "requestId":       "req_01JXYZ",
    "limit":           3,
    "currentPage":     2,
    "lastPage":        12,
    "from":            4,
    "to":              6,
    "total":           36,
    "hasNextPage":     true,
    "hasPreviousPage": true,
    "nextCursor":      null,
    "previousCursor":  null
  }
}
```

When offset mode is selected, the package still writes `hasNextPage` /
`hasPreviousPage` booleans for free so frontends don't have to re-derive them
from `currentPage` / `lastPage`.

## Frontend JavaScript Example

Cursor-mode, paginated walk of an endpoint using `after`. Works for any
cursor-aware list (posts, comments, subscribers, notifications, media, …).

```ts
type Page<T> = {
  ok: boolean;
  data: T[];
  meta: {
    hasNextPage: boolean;
    nextCursor?: string | null;
  };
  links?: { next?: string | null };
};

/**
 * Fetch every item of a paginated collection by chasing `meta.nextCursor`.
 * Halts when `meta.hasNextPage === false` or after `maxPages` requests as a
 * DoS guard. Pass `filters` without the pagination keys — they are appended
 * per request.
 */
async function* walkAll<T>(
  baseUrl: string,
  filters: Record<string, string | number | boolean> = {},
  { pageSize = 50, maxPages = 100 } = {}
): AsyncGenerator<T, void, undefined> {
  let after: string | null = null;
  for (let page = 0; page < maxPages; page++) {
    const params = new URLSearchParams(
      Object.entries(filters).map(([k, v]) => [k, String(v)])
    );
    params.set("limit", String(pageSize));
    if (after) params.set("after", after);

    const res = await fetch(`${baseUrl}?${params.toString()}`, {
      headers: { Accept: "application/json", Authorization: `Bearer ${API_TOKEN}` },
    });
    if (!res.ok) {
      const body = (await res.json().catch(() => ({}))) as {
        message?: string;
        errors?: Record<string, string[]>;
      };
      throw new Error(
        body?.message ?? `Request failed with HTTP ${res.status}`
      );
    }
    const json = (await res.json()) as Page<T>;
    for (const item of json.data) yield item;
    if (!json.meta.hasNextPage) return;
    after = json.meta.nextCursor ?? null;
    if (!after) return;
  }
  throw new Error(`walkAll: exceeded ${maxPages} pages; stop to avoid unbounded work.`);
}

// Usage: dump every active subscriber (up to the page guard).
for await (const sub of walkAll<Subscriber>("/api/v1/admin/newsletter/subscribers", {
  status: "active",
  includeTotal: false,
})) {
  console.log(sub.email, sub.createdAt);
}
```

## Repository Contract (for developers)

When adding a new paginated endpoint, follow the checklist:

1. **Domain filter**: embed `pagination.PageRequest` and alias the page type
   to `pagination.PageResult[Item]`.
2. **Service layer**: accept the filter, clamp `filter.Limit` to
   `[cfg.DefaultPerPage, cfg.MaxPerPage]`, forward to the repository.
3. **Repository `ListXxx`**:
   - declare a package-level `xCfg = pagination.CursorConfig{Kind, Sort: [..., id Tiebreak]}`
   - `DecodeCursor(cfg, pr.Cursor)` → `ValidateCursor(cfg, decoded)`
   - `BuildSeek(cfg, pr, cursorFields)` → apply `seek.WhereClause`, `seek.BindVars`, `seek.OrderClause`, `seek.LimitFetch`; add `OFFSET pr.Offset` only for `ModeOffset`
   - run `LIMIT seek.LimitFetch` (always +1) then reverse if `seek.ReverseDisplay`
   - `TruncatePage(items, pr.Limit, pr.Forward, hadCursor)` → `(page, hasNext, hasPrev)`
   - if non-empty + `hasNext` / `hasPrev`, `EncodeCursor(cfg, SortValues(cfg, edgeRow, accessor))`
4. **Handler**:
   - `pagination.ParseRequest(c, cfg)`; on error write HTTP 400 using
     `ErrorCode(err)` / `ErrorCause(err)`.
   - call service, then `responses.SuccessPaginatedResult[T](c, 200, {Service, Result, Data})`.
   - document the 6 standard `@Param` query entries (after, before, limit,
     perPage, includeTotal, page) in the godoc swagger block.

See `internal/adapters/outbound/persistence/notification_repository.go:100` or
`internal/adapters/outbound/persistence/newsletter_repository.go:274` for
worked examples of the raw-SQL and GORM variants.
