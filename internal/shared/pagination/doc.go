// Package pagination provides a shared, reusable keyset (cursor) pagination
// implementation for the blog API. It includes HMAC-signed opaque cursors
// with expiry and kind scoping, a single request parser replacing the
// previous per-handler page/perPage Atoi calls, SQL helpers for Postgres
// row-value seek queries, and response envelope helpers.
//
// Usage patterns are documented in docs/backend/pagination.md.
package pagination
