# Response Codes

Every envelope includes a packed numeric `code` built by `BuildResponseCode`.

## Format

```
HTTP_STATUS_CODE (3 digits) + SERVICE_CODE (2 digits) + CASE_CODE (2 digits)
```

Example: `2010301` = HTTP `201` + Service `03` (Comments) + Case `01` (Success).

```go
code := BuildResponseCode(201, ServiceComments, CaseSuccess) // 2010301
```

## Envelope

```json
{
  "ok": true,
  "code": 2010301,
  "data": {},
  "meta": { "request_id": "…" },
  "error": null
}
```

Machine-readable string codes remain on `error.code` (e.g. `validation_error`).
The numeric `code` is for client branching and support triage.

## Service codes

| Code | Constant | Domain |
| --- | --- | --- |
| 00 | `ServicePlatform` | Health, generic/platform |
| 01 | `ServiceAuth` | Auth, sessions, password reset |
| 02 | `ServiceUsers` | Users, profiles, `me/*` |
| 03 | `ServiceComments` | Comments & moderation |
| 04 | `ServicePosts` | Posts, revisions, SEO |
| 05 | `ServiceMedia` | Media uploads & assets |
| 06 | `ServiceCategories` | Categories |
| 07 | `ServiceTags` | Tags |
| 08 | `ServiceNotifications` | Notifications & SSE |
| 09 | `ServiceAnalytics` | Analytics |
| 10 | `ServiceSettings` | Admin settings |
| 11 | `ServiceRBAC` | Roles, permissions, impersonation |
| 12 | `ServiceNewsletter` | Newsletter |

## Case codes

| Code | Constant | Meaning |
| --- | --- | --- |
| 01 | `CaseSuccess` | Success |
| 02 | `CaseValidation` | Validation / bad request |
| 03 | `CaseUnauthorized` | Authentication required / failed |
| 04 | `CaseForbidden` | Authorization denied |
| 05 | `CaseNotFound` | Resource missing |
| 06 | `CaseConflict` | Conflict / state clash |
| 07 | `CaseUnprocessable` | Domain rule failure (422) |
| 08 | `CaseRateLimited` | Rate limited |
| 09 | `CaseInternalError` | Unexpected / upstream failure |
| 10 | `CaseAccepted` | Accepted / queued (202) |
| 11 | `CaseNoContent` | Empty success / deleted |

## Implementation

- Builder and constants: [response_code.go](../../internal/adapters/inbound/http/response_code.go)
- Envelope writers: `successFor`, `failureFor`, `successPaginatedFor` in
  [response.go](../../internal/adapters/inbound/http/response.go)
- Prefer domain helpers (`successFor` / `failureFor`) over the platform defaults
  when the handler belongs to a known service.
