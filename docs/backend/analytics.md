# Analytics Backend

## Overview

Analytics measurement and dashboard APIs live behind hexagonal ports so providers can be swapped. The default implementation stores raw events in PostgreSQL for durability and aggregates them for dashboard queries. Redis is used for short-lived counters, real-time fan-out, and hot dashboard caching.

## Module Structure In Hexagonal Terms

- `internal/core/analytics/domain`
  - entities: `PageViewEvent`, `TimeSpentEvent`, `NavigationEvent`, `SearchEvent`, `SearchClickEvent`, `AggregateMetric`, `ConsentRecord`
  - value objects: time windows, metric keys, consent status, attribution buckets
- `internal/core/analytics/ports`
  - inbound: `AnalyticsIngestionPort`, `AnalyticsQueryPort`, `AnalyticsRealtimePort`, `ConsentManagementPort`
  - outbound: `AnalyticsEventRepository`, `AnalyticsAggregateRepository`, `ConsentRepository`, `AnalyticsCounterStore`, `RealtimePublisher`
- `internal/core/analytics/service`
  - ingestion with validation and consent checks
  - aggregation builders
  - query services for dashboard widgets
  - real-time fan-out triggers
- `internal/adapters/inbound/http`
  - public ingestion endpoints under `/api/v1/analytics/ingest/*` with consent gating
  - admin dashboard query endpoints under `/api/v1/admin/analytics/*`
  - admin realtime SSE stream endpoint
- `internal/adapters/outbound`
  - `persistence`: GORM repositories for raw events and rollups
  - `cache`: Redis counters and hot dashboard caches
  - `events`: Watermill publishers for async aggregation jobs and SSE fan-out

## Collected Data Points

Data points are collected only when consent is granted and are kept to the minimum required for dashboard features.

### Page Views

- consent id (pseudonymous, per-browser)
- session id (short-lived, rotated after timeout or logout)
- route / canonical path
- referrer (normalized, truncated if sensitive)
- approximate country / region from IP (when enabled and consistent with privacy rules)
- device type (desktop / tablet / mobile)
- occurred at timestamp

Note: store IP only temporarily (or masked/truncated) for geo lookup, never as a permanent analytics identifier unless explicitly required by policy.

### Time Spent

- page route
- started at
- ended at or last heartbeat
- focus state approximations (in focus / backgrounded)

### Navigation Patterns

- from route
- to route
- transition type (internal link, external link, back/forward, direct)
- session id
- occurred at

### Search Analytics

- normalized query
- total results returned for query
- filters applied (category, tag, date range)
- result position clicked
- clicked resource type and id (post, page, category, tag)
- time between query and click

## Storage Model

### Recommended Tables

- `analytics_consents`
  - id
  - consent_token
  - granted (boolean enum: `granted`, `rejected`, `withdrawn`)
  - scope (bitmask or JSONB flags)
  - granted_at
  - withdrawn_at
  - last_seen_at

Raw events (migration 00032). Every table has `uuid` (the client's dedupe key: a retried event
is ignored), `subject_uuid` (the consent subject, set only when it granted analytics),
`visitor_hash` (64 hex characters, see [Visitor identity](#visitor-identity)), and `session_id`
(the client's session). No IP address or user agent is stored. `subject_uuid` and `search_uuid`
have no foreign keys, so a batch never fails because a subject was erased or a click arrived
before its search.

- `analytics_page_views`: `path`, `referrer` (scheme, host, and path only), `country_code`,
  `device_type` (`desktop`, `tablet`, `mobile`), `browser` (family), `occurred_at`
- `analytics_time_spent`: one row per page view (`uuid` is the page view's). `path`,
  `focus_seconds` (0–14400, the highest heartbeat wins), `started_at`, `last_seen_at`
- `analytics_navigation`: `from_path` (null for an entry), `to_path`, `transition_type`
  (`internal`, `external`, `back_forward`, `direct`), `occurred_at`
- `analytics_searches`: `query` (normalised), `result_count`, `filters` (JSONB: `category`,
  `tag`, `from`, `to`), `occurred_at`
- `analytics_search_clicks`: `search_uuid`, `position` (1–1000), `resource_type` (`post`,
  `page`, `category`, `tag`), `resource_uuid`, `occurred_at`

Times are the server's receive time, never the client's clock.

### Rollups

Dashboards read rollups (migration 00033), never raw events. Each rollup row belongs to a
period: `grain` is `day`, `week` (ISO, Monday to Sunday), or `month`, and `period_start` is the
period's first local date in the `site.timezone` setting.

| Table | One row per period and | Measures |
| --- | --- | --- |
| `analytics_rollup_site` | (period) | views, visitors, sessions, bounces (sessions with one page view), focus seconds and time-spent rows, searches, zero-result searches, searches with a click, search clicks, consented visitors, new consented visitors |
| `analytics_rollup_pages` | path | views, visitors, entries, exits, focus seconds and rows |
| `analytics_rollup_referrers` | referrer host of each session's first view (`(direct)` without one) | sessions, visitors |
| `analytics_rollup_dimensions` | `country` (`(unknown)` without one), `device`, or `browser` value | views, visitors |
| `analytics_rollup_navigation` | from path (`(entrance)` for entries), to path, transition | transitions |
| `analytics_rollup_searches` | query | searches, visitors, zero results, searches with a click, clicks, seconds to first click (summed) |
| `analytics_rollup_search_positions` | clicked position | clicks |
| `analytics_rollup_search_results` | query and clicked resource | clicks |
| `analytics_rollup_cohorts` | local day consented visitors were first seen | size, and how many had a page view on day 1, 7, and 30 (NULL until that day ends) |

- **Unique visitors** are exact distinct visitor hashes for each grain, not sums of days. An
  anonymous visitor's hash changes daily, so one seen on three days of a week counts three times
  in that week; consented visitors count once.
- **Capped dimensions.** Paths, referrer hosts, transitions, and queries keep the top 1000 of
  each period by volume; the rest fold into one `(other)` row, so totals still add up. Clicked
  results keep the top 1000 and drop the rest.
- **Search clicks** count toward the period of their search, whenever they happen. A click
  whose search was never stored is ignored.
- **Retention cohorts** cover consented visitors only: an anonymous visitor has no identity that
  lasts past a day. `analytics_subject_first_seen` keeps each consented subject's first page
  view past raw-event retention, so a returning visitor is not counted as new again; it is
  deleted with the subject on erasure. `new_visitors` and `consented_visitors` in the site rollup
  give the new versus returning split.
- The rollups hold counts only, with no visitor hashes or subject ids.

#### Aggregation

The `analytics-rollup` job in `app scheduler` runs every 15 minutes:

1. It recomputes every period containing today or yesterday (the day, week, and month of each),
   and the cohorts of the last 31 days. Recomputing a period replaces all of its rows in one
   transaction, so runs are idempotent; an advisory lock per period keeps a concurrent backfill
   from interleaving.
2. **Late events.** Events are timed when the API receives them, so none can land in an older
   period; including yesterday covers events still buffered at midnight and a run that failed
   just before it. A period closes for good once it no longer contains today or yesterday.
3. **Time zone.** The zone the rollups were built in is stored in `analytics_rollup_state`. On
   the first run, or after `site.timezone` changes, the job rebuilds every period and cohort
   starting on or after the oldest raw event's local day. Older periods keep the boundaries of
   the previous zone, because their raw events are gone.

`app analytics rollup --from YYYY-MM-DD [--to YYYY-MM-DD]` recomputes the periods overlapping
those dates (read in the site zone) and the cohorts that change within them; use it after
restoring raw events or when the job failed for more than a day. Days before the oldest raw
event and after today are skipped. A week or month that started before the oldest raw event is
recomputed from the events that remain, so it can shrink.

Raw-event retention must keep every open period and the cohort window: the planned pruning job
(see [Jobs and Workers](#jobs-and-workers)) must not delete events from the current or previous
month or the last 31 days, whatever `analytics.raw_retention_days` says.

### Indexing

- raw events: btree on `occurred_at` (`started_at` for time spent), a partial index on
  `subject_uuid` for erasure, and `search_uuid` on clicks
- rollups: primary keys lead with `(grain, period_start)`, which every dashboard range read
  filters on

## API Endpoints

### Public Ingestion (Consent-Gated)

Each request carries one event and returns `202` with `{"id": "<uuid>"}`. Every event needs a
`session_id` (a UUID the client generates per tab and rotates after 30 minutes idle); `id` is
optional and is generated when omitted.

- `POST /api/v1/analytics/ingest/page-view`: `path`, `referrer`. The returned id is the page
  view id for time-spent heartbeats.
- `POST /api/v1/analytics/ingest/time-spent`: `view_id`, `path`, `focus_seconds` (cumulative).
  Send one every 15–30 seconds while visible and one on unload (`navigator.sendBeacon` cannot
  set headers, so use `fetch` with `keepalive: true`).
- `POST /api/v1/analytics/ingest/navigation`: `from` (omit for an entry), `to`, `transition`.
- `POST /api/v1/analytics/ingest/search`: `query`, `result_count`, `filters`. The returned id is
  the `search_id` for clicks.
- `POST /api/v1/analytics/ingest/search-click`: `search_id`, `position`, `resource_type`,
  `resource_id`.

#### Ingest pipeline

1. A body over 8 KiB is `413 analytics.payload_too_large`; more than
   `ANALYTICS_INGEST_PER_MINUTE` requests per client IP (default 300, shared by the five routes)
   is `429`. Both checks run before the consent lookup.
2. The consent gate (see [Implementation](#implementation)) decides between a granted subject,
   an anonymous visitor, and a refusal.
3. The service validates and normalises the event. It drops, while still answering `202`,
   events from bots and scripted clients (user agent match, or no user agent), prefetches
   (`Sec-Purpose`/`Purpose: prefetch`), and refusing subjects. The answer never depends on
   whether a path, post, or search exists, so the endpoints cannot be used to probe content or
   users.
4. The event goes into a bounded in-process queue (`ANALYTICS_QUEUE_SIZE`, default 10000). A
   background writer inserts batches of up to 500 every second with one multi-row statement per
   table. A full queue or a failed insert drops events and counts them in
   `blog_analytics_events_dropped_total`; shutdown flushes the queue for up to 5 seconds. Events
   still queued when a process crashes are lost: at most about one second of traffic.

#### Visitor identity

- A subject that granted analytics: `visitor_hash` is an HMAC of the subject id, stable across
  days, so retention can follow it. `subject_uuid` is stored for erasure.
- Anyone else: `visitor_hash` is an HMAC of the day, client IP, and user agent. The same visitor
  gets an unrelated hash the next day, so anonymous visits cannot be linked across days.
- The HMAC key derives from `APP_ENCRYPTION_KEY`. Without it, each API process uses a random key
  held in memory, so anonymous unique counts across replicas (or restarts) are approximate.
- The country comes from `ANALYTICS_COUNTRY_HEADER` (for example `CF-IPCountry`), read only when
  the request's immediate peer is in `APP_TRUSTED_PROXIES`. `XX` and `T1` are treated as unknown.
- Device class and browser family come from the user agent, which is then discarded.

### Admin Dashboard (RBAC Protected)

- `GET /api/v1/admin/analytics/overview`
  - stat cards + trend series (7/30/90 day windows)
  - query params: `window`, `start_date`, `end_date`, `path`, `country`
- `GET /api/v1/admin/analytics/pages`
  - popular pages, top by time spent, rising pages
- `GET /api/v1/admin/analytics/navigation`
  - section flows, top entry/exit paths
- `GET /api/v1/admin/analytics/retention`
  - cohort retention views for new vs returning
- `GET /api/v1/admin/analytics/search`
  - top queries, CTR stats, zero-result queries, position CTR
- `GET /api/v1/admin/analytics/realtime/stream`
  - SSE endpoint for live activity stream
  - `events`: `realtime.page_view`, `realtime.search`, `realtime.summary`
- `POST /api/v1/admin/analytics/export`
  - exports CSV/JSON for a date window
  - requires `analytics.export` and step-up auth if 2FA is enabled

### Consent Management

- `GET /api/v1/analytics/consent`
- `POST /api/v1/analytics/consent`
  - grant or reject consent with scope flags
- `DELETE /api/v1/analytics/consent/{id}`
  - withdraw consent and mark records eligible for erasure

#### Implementation

- The browser identifies itself with an opaque consent token in the `X-Consent-Token` header.
  The first `POST` without a valid token creates a pseudonymous subject and returns the token
  once (`201`); later calls return `200` without it. Only the SHA-256 hash of the token is stored
  (`consent_subjects.token_hash`).
- Body: `{"purposes": {"analytics": true, "authenticated_analytics": false}, "policy_version": "2026-09"}`.
  Purposes are `analytics` and `authenticated_analytics`. Each decision is stored per purpose in
  `analytics_consents` with status (`granted`, `rejected`, `withdrawn`), policy version, and
  decision time. Refusing a previously granted purpose records `withdrawn`; a first refusal
  records `rejected`. Unchanged decisions are not rewritten.
- `authenticated_analytics` requires a signed-in user and a granted `analytics` consent. Granting
  it links the subject to the user; refusing or withdrawing it unlinks them. Withdrawing
  `analytics` also withdraws `authenticated_analytics`.
- `DELETE /analytics/consent/{id}` is allowed for the token holder or the linked user; anything
  else is `404`. `POST` is rate limited to 20 requests per minute per client.
- Ingest enforcement: every `/analytics/ingest/*` route passes through a gate before the handler.
  `analytics.enabled=false` returns `404 analytics.disabled`; with
  `analytics.consent_required=true`, a request without a token whose `analytics` consent is
  granted returns `403 analytics.consent_required`. With it off, events without granted consent
  are stored anonymised (no subject link), and events from a subject that rejected or withdrew
  analytics are accepted and dropped.
- Each status change records `analytics.consent.granted`, `analytics.consent.rejected`, or
  `analytics.consent.withdrawn` in the outbox.

## Real-Time Pipeline

Recommendation for live dashboard:

1. Ingestion API validates consent.
2. On success, queue the raw event for the batched writer (write path).
3. Hand accepted events to the live counters in process (no outbox; see [Events](#events)).
4. Async handlers:
   - update Redis counters for live counts (last 5 min, last hour)
   - fan out per-topic events to admin SSE stream via pub/sub
5. The `analytics-rollup` job in `app scheduler` rolls raw events up into the
   [rollups](#rollups) that historical views read.

## Events

Domain and integration events:

- `analytics.consent.granted`
- `analytics.consent.rejected`
- `analytics.consent.withdrawn`
- `analytics.realtime.summary.tick`

Consent events go through the outbox. Ingested events do not: an outbox row per event would put
a database write back on the ingest path. There are no `analytics.*.ingested` events, and
rollups publish nothing: dashboards read the rollup tables directly.

## Jobs and Workers

- `analytics-rollup` (`app scheduler`, every 15 minutes): builds the rollups and cohorts; see
  [Aggregation](#aggregation).
- Raw-event pruning under `analytics.raw_retention_days`: planned with the export and retention
  epic.

Events refused by consent are dropped before they are queued, so no cleanup job is needed.

## Validation and Error Handling

Validation rules:

- reject payloads over 8 KiB (`413`)
- `path` must start with `/`; the query and fragment are dropped, repeated slashes collapse, a
  trailing slash is removed, whitespace or control characters are rejected, and the result is at
  most 512 bytes
- `referrer` keeps only an http(s) scheme, host, and path; anything else is dropped
- search queries are lowercased, whitespace-collapsed, stripped of control characters, and cut to
  200 characters
- `focus_seconds` is clamped to 0..14400, `position` to 1..1000, `result_count` to 0..1000000
- unknown `transition`, `resource_type`, or filter keys are `400`
- events from a subject that rejected or withdrew consent are dropped

Error handling:

- ingestion failures should be idempotent where possible; prefer dedupe keys
- SLA: public ingestion endpoints should return fast; defer heavy aggregation out-of-process
- dashboard queries must return deterministic errors for bad windows, bad dimensions, or permission issues
- never expose raw consent tokens or any user PII in error messages

## Caching Strategy

Use a three-level strategy:

1. **Redis hot cache** for overview widgets and top-N queries in the current day/window
   - TTL per widget type (realtime short, 30-day long)
2. **Materialized daily aggregates** in PostgreSQL for historical windows
3. **In-process short TTL** for repeated identical admin requests within the same API instance

Invalidation triggers:

- after daily rollup job completes
- admin forces refresh via dashboard action
- right-to-erasure or consent-withdrawal events invalidate dependent aggregates

## Privacy By Implementation

Hard rules for the backend:

- consent token is pseudonymous; never join directly to `users.id` unless the user explicitly opts into authenticated analytics (separate scope)
- allow deletion pipeline: mark tokens as erased, then delete/mask raw events and rebuild affected aggregates. Account erasure deletes the user's consent subjects and, in the same statement, every raw event carrying their `subject_uuid`; anonymised events carry no link and are not affected
- export pipeline: return only records tied to a consent token proof, signed or token-protected
- never log raw event payloads at INFO or DEBUG levels; keep structured logs metric-only or hash-only

## Testing Strategy

### Unit Tests

- services: consent enforcement for ingestion
- query builders: correct date windows, correct dimension filters
- aggregators: rollup math for page views, time spent, CTR, retention cohorts

### Integration Tests

- HTTP endpoints with and without consent tokens
- ingestion -> raw event persistence -> aggregate rollup end-to-end
- realtime SSE stream receives `page_view` and `search` events
- admin RBAC: `analytics.read`, `analytics.export`, `analytics.realtime.read`
- withdrawal/erasure flow correctly removes or quarantines records

### Performance Tests

- ingestion endpoint throughput with realistic batch sizes
- dashboard overview query latency for 30-day and 90-day windows
- SSE fan-out under a realistic number of concurrent admin clients
