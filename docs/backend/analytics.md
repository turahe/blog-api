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

`analytics_exports` (migration 00034) queues rollup exports: requester, `grain`, `first_day` and
`last_day` (whole periods, in the stored `timezone`), `status`, `attempts`, `last_error`, and,
once built, `storage_key`, `size_bytes`, and `expires_at`. A partial unique index allows one
pending or running export per user. See [Export](#export).

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
- **Shared queries only.** Query text is typed by visitors and can hold names or contact
  details, so a query is kept by name only when at least 2 distinct visitors searched it on one
  UTC day of the period (`MinQueryVisitors`); anything rarer folds into `(other)`, and its
  clicked results are not kept. Visitors are told apart only within a day because anonymous
  hashes change daily, so one person repeating a query across a week does not qualify it.
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

Raw-event retention keeps every open period and the cohort window: the pruning job (see
[Retention](#retention)) never deletes events from the current or previous month or the last 31
days, whatever `analytics.raw_retention_days` says.

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
- Anyone else: `visitor_hash` is an HMAC of the day's salt, client IP, and user agent. The salt
  is 32 random bytes per UTC day in `analytics_salts` (migration 00035), created by the first
  replica that needs it and cached by each. Salts older than yesterday are deleted (when a new
  day's salt is created and by the hourly retention job), after which nobody, including an
  operator holding `APP_ENCRYPTION_KEY`, can test a guessed IP and user agent against that day's
  hashes. The same visitor gets an unrelated hash the next day.
- The HMAC key derives from `APP_ENCRYPTION_KEY`. Without it, each API process uses a random key
  held in memory, so anonymous unique counts across replicas (or restarts) are approximate. The
  same holds for a day's hashes while PostgreSQL cannot hand out the salt: the replica uses a
  stand-in salt of its own, logs `analytics visitor salt unavailable`, and retries every 10
  seconds.
- The country comes from `ANALYTICS_COUNTRY_HEADER` (for example `CF-IPCountry`), read only when
  the request's immediate peer is in `APP_TRUSTED_PROXIES`. `XX` and `T1` are treated as unknown.
- Device class and browser family come from the user agent, which is then discarded.

### Admin Dashboard (RBAC Protected)

Every report reads the rollup tables only, never raw events, so a report costs the same whatever
the traffic and raw pruning never changes a figure.

| Route | Permission | Contents |
| --- | --- | --- |
| `GET /api/v1/admin/analytics/overview` | `analytics.read` | totals, a series per period, top 10 traffic sources, country/device/browser |
| `GET /api/v1/admin/analytics/pages` | `analytics.read` | top pages by views, average focus time (`sort=time`), or views gained (`sort=rising`) |
| `GET /api/v1/admin/analytics/navigation` | `analytics.read` | top steps between pages with their transition type, top entry and exit pages |
| `GET /api/v1/admin/analytics/retention` | `analytics.read` | daily cohorts with day 1/7/30 returns, weighted rates, new versus returning |
| `GET /api/v1/admin/analytics/search` | `analytics.search.read` | search totals and series, CTR, time to first click, top and zero-result queries, clicks per position, top clicked results |

Admins hold every permission; editors are seeded with `analytics.read` and
`analytics.search.read` (rerun `app seed` on existing databases). Without an RBAC enforcer the
handlers fall back to the admin and editor roles. Export stays admin-only.

#### Query parameters

All five reports share one query:

| Param | Default | Meaning |
| --- | --- | --- |
| `from`, `to` | the 30 days through today | inclusive dates (`YYYY-MM-DD`) in the `site.timezone` setting; at most 731 days |
| `grain` | from the range: up to 92 days `day`, up to 366 `week`, else `month` | the rollup grain; `day` covers at most 92 days (`400` beyond) so a report scans a bounded number of rows |
| `compare` | `previous` | `previous` also returns the same number of periods just before the window; `none` skips it |
| `limit` | 20 | rows per list, 1–100 (pages, navigation, search) |
| `sort` | `views` | pages only: `views`, `time`, or `rising` |

#### Semantics

- **Whole periods.** A report covers every period of the grain that touches `from`..`to`, so a
  weekly window starts on the Monday on or before `from` and a monthly one on the 1st. The
  response echoes `timezone`, `grain`, `window` (`from`, `to`, `periods`), and `comparison`
  (`from`, `to`, or `null` with `compare=none`), all as local dates.
- **Series.** One point per period, zero-filled where no rollup row exists; each point's
  `visitors` is exact for that period.
- **Visitors over a range.** Unique visitors do not add up across periods, so range totals are
  the sum of per-period uniques and are named after the grain: `visitor_days`,
  `visitor_weeks`, or `visitor_months` (likewise `consented_visitor_*` and each list row's
  visitors). When the window is a single period the true distinct count is also returned as
  `visitors`. For a distinct count over a month, ask for `grain=month` on that month.
- **Rates** (`bounce_rate`, `pages_per_session`, `avg_time_seconds`, `search_ctr`, `ctr`,
  `avg_seconds_to_click`, retention `rates`, position `share`) are ratios of summed counts and
  are `null` when the denominator is zero.
- **Comparison.** `previous` holds the comparison window's totals; pages add `previous_views` and
  `change` with `compare=previous` or `sort=rising`.
- **The `(other)` row** folds everything outside a period's top 1000. It appears in view-ordered
  page lists, query lists, and traffic sources, but never in `sort=time`, `sort=rising`, or
  zero-result queries. Search totals and average time to click sum every row, `(other)`
  included.
- **Retention.** Cohorts are the consented visitors first seen on each day of the window. A
  return day that has not ended yet is `null` and is left out of that rate's weighting.
- **Freshness.** Rollups of the current and previous periods are rebuilt every 15 minutes, so
  today lags by at most one run. Responses carry `Cache-Control: private, no-store`.

- `GET /api/v1/admin/analytics/realtime/stream` — live view over SSE; needs
  `analytics.realtime.read` (admins and editors). See [Real-Time Pipeline](#real-time-pipeline).
- `POST /api/v1/admin/analytics/export`, `GET /api/v1/admin/analytics/exports`, and
  `GET /api/v1/admin/analytics/exports/{id}` — rollup exports; need `analytics.export` (admins
  only) and a password (plus 2FA code) step-up. See [Export](#export).

### Consent Management

- `GET /api/v1/analytics/consent`
- `POST /api/v1/analytics/consent`
  - grant or reject consent with scope flags
- `DELETE /api/v1/analytics/consent/{id}`
  - withdraw consent; withdrawing `analytics` deletes the subject's stored events

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
- Withdrawing `analytics` (through `DELETE` or by `POST`ing `false` after a grant) deletes, in
  the same transaction, every raw event carrying the subject's `subject_uuid` and its
  `analytics_subject_first_seen` row. Rollups keep their counts, which identify no one.
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

1. Ingest validates the event and consent, and queues it for the batched writer.
2. Every accepted event (after bot, prefetch, and refused-consent filtering) is also reduced to
   its live form and queued for the broker: kind, time, session id, and for page views the
   path, country, and device, for searches the normalised query and result count. No visitor
   hash or subject id leaves the process.
3. Each replica publishes its queue to the broadcast topic `analytics.live` once a second (or
   every 500 events). The queue holds 10,000 events; beyond that events are dropped from the
   live view only and a warning is logged. There is no outbox: see [Events](#events).
4. Every replica subscribes to `analytics.live` on its own broadcast queue, so each one counts
   every replica's traffic in memory. A replica that starts (or restarts) begins empty and is
   complete after 30 minutes.
5. The `analytics-rollup` job in `app scheduler` rolls raw events up into the
   [rollups](#rollups) that historical views read; the live view never touches the database.

### Live stream

`GET /api/v1/admin/analytics/realtime/stream` needs `analytics.realtime.read`, a message
broker (`503 analytics.realtime_unavailable` without one), and a free stream slot
(`SSE_MAX_CONCURRENT_PER_USER` per user, `429 analytics.realtime_limit` with `Retry-After`).

| Window | Value |
| --- | --- |
| Active sessions | sessions with any event (including time-spent heartbeats) in the last 5 minutes; tracking stops at 100,000 sessions (`active_sessions_capped: true`) |
| Series and top pages | the last 30 minutes in one-minute buckets; top 10 pages, with paths past 1,000 per minute folded into `(other)` |
| Refresh | every 5 seconds, plus once when the stream opens |

Each refresh writes `realtime.page_view` frames (`ts`, `path`, `country`, `device`) and
`realtime.search` frames (`ts`, `query`, `result_count`) for events that arrived since the last
refresh, at most 20 of each (the latest when busier; the first refresh replays up to 20
recent ones), then one `realtime.summary` frame:

```json
{
  "ts": "2026-09-25T10:00:05Z",
  "active_sessions": 42, "active_sessions_capped": false,
  "active_window_minutes": 5, "window_minutes": 30,
  "views": 1830, "searches": 96,
  "series": [{"minute": "2026-09-25T09:31:00Z", "views": 61, "searches": 3}],
  "top_pages": [{"path": "/posts/hello", "views": 210}]
}
```

The summary doubles as the heartbeat (`X-Accel-Buffering: no` disables proxy buffering), the
stream ends with `stream.closed` (code `shutdown`) when the server stops, and a client that
reconnects simply gets a fresh summary: live data has no replay.

## Export and Retention

### Export

`POST /api/v1/admin/analytics/export` queues a ZIP of CSV files built from the rollups only:

```json
{"from": "2026-08-01", "to": "2026-08-31", "grain": "day", "current_password": "…", "two_factor_code": "123456"}
```

- **Scope.** One CSV per rollup table (`site`, `pages`, `referrers`, `audience`, `navigation`,
  `searches`, `search_positions`, `search_results`) holding every row of the window's periods,
  plus `cohorts` (the daily cohorts of the window's days) and a `manifest.json` (window, time
  zone, grain, notes). Raw events, visitor hashes, session ids, and subject ids are never
  exported; a clicked result carries only its resource type and UUID.
- **Window.** Same rules as the dashboard: `from` and `to` are dates in the site time zone,
  widened to whole periods of `grain` (picked from the range length when omitted), at most 731
  days. The dashboard's 92-day cap on `day` does not apply: an export may hold two years of days.
  Visitors are distinct within one period only.
- **Step-up.** The body must carry the caller's current password, and a TOTP or backup code when
  two-factor is enabled; a failure is `403 analytics.step_up_required`. Every request, refused or
  not, is audited as `admin.analytics.export` with the window (and failure reason).
- **Queue.** One open export per user: a second request is `409 analytics.export_in_progress`
  with the open export in `error.details`. The `analytics-exports` job builds up to two exports
  a minute (three attempts each) and stores them at `analytics-exports/<id>.zip` in `S3_BUCKET`,
  which must not be publicly readable. Without object storage the routes answer
  `503 analytics.export_unavailable`.
- **Download.** `GET /api/v1/admin/analytics/exports/{id}` (and the list of the caller's 20 most
  recent exports) returns `status` (`pending`, `running`, `completed`, `failed`, `expired`) and,
  while the archive exists, a presigned `download_url` valid for `ANALYTICS_EXPORT_URL_TTL`
  (15 minutes, never past `archive_expires_at`). A new link is signed on every call. Exports of
  other users are `404`, even for admins. Archives are deleted after
  `ANALYTICS_EXPORT_RETENTION` (72 hours), after which the export shows `expired`.
- **Spreadsheet safety.** Paths, hosts, and queries come from visitors, so any value starting
  with `=`, `+`, `-`, `@`, a tab, or a carriage return is prefixed with `'`.

### Retention

| Data | Default | Setting | Notes |
| --- | --- | --- | --- |
| Raw events (all five tables) | 90 days | `analytics.raw_retention_days` (1–730) | Never the current or previous month or the last 31 days |
| Daily rollups | 25 months | `analytics.rollup_day_retention_months` (0–120, 0 = forever) | Independent of raw retention |
| Weekly and monthly rollups, cohorts | forever | — | Contain no identifiers |
| First-seen times (`analytics_subject_first_seen`) | forever | — | Deleted with the subject on erasure or analytics withdrawal |
| Visitor salts (`analytics_salts`) | today and yesterday (UTC) | — | Older salts are destroyed, so older anonymous hashes cannot be recomputed |
| Export archives | 72 hours | `ANALYTICS_EXPORT_RETENTION` | Rows stay as the export history |

The `analytics-retention` job (hourly) deletes visitor salts older than yesterday (UTC), raw
events stored before local midnight of the cutoff day, 5000 rows per table per batch and at most 200 batches per run, then daily rollups of
periods starting before the rollup cutoff. Both are computed in the site time zone. Because
rollups outlive raw events, `app analytics rollup` cannot rebuild a pruned period: it skips days
before the oldest remaining event.

## Events

Domain and integration events:

- `analytics.consent.granted`
- `analytics.consent.rejected`
- `analytics.consent.withdrawn`

Consent events go through the outbox. Live analytics use the broker topic `analytics.live`
directly (see [Real-Time Pipeline](#real-time-pipeline)). Ingested events do not: an outbox row per event would put
a database write back on the ingest path. There are no `analytics.*.ingested` events, and
rollups publish nothing: dashboards read the rollup tables directly.

## Jobs and Workers

- `analytics-rollup` (`app scheduler`, every 15 minutes): builds the rollups and cohorts; see
  [Aggregation](#aggregation).
- `analytics-exports` (`app scheduler`, every minute): builds queued exports and deletes expired
  archives; see [Export](#export).
- `analytics-retention` (`app scheduler`, hourly): prunes raw events and daily rollups; see
  [Retention](#retention).

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
- deletion: account erasure deletes the user's consent subjects and, in the same statement, every raw event and first-seen row carrying their `subject_uuid`; withdrawing `analytics` does the same for the subject. Anonymised events carry no link and are not affected
- export pipeline: admin exports contain rollups only (no raw events or identifiers), sit behind
  a password (and 2FA) step-up, and are served through short-lived presigned links
- never log raw event payloads at INFO or DEBUG levels; keep structured logs metric-only or hash-only

### Privacy review (2026-09-25)

Scope: what is stored, for how long, how visitors are identified, and what leaves the system.

| Area | Finding | Outcome |
| --- | --- | --- |
| Account erasure | Erasure deleted the user's consent subjects but not the raw events and first-seen rows tagged with them | Fixed: erasure now deletes them in the same statement |
| Consent withdrawal | Past events of a withdrawing subject stayed until raw retention | Changed: withdrawing `analytics` deletes them and the first-seen row |
| Anonymous visitor hash | Keyed only with the long-lived `APP_ENCRYPTION_KEY`, so a key holder could test a guessed IP and user agent against any past day | Changed: a random per-day salt, destroyed after a day, is part of the hash |
| Search queries in rollups | Free text kept by name in rollups (daily 25 months, weekly and monthly forever) and exports, even when one person typed it | Changed: kept by name only when 2 distinct visitors searched it on one day |
| Paths in rollups | Paths are kept by name forever; the query string and fragment are already dropped at ingest | Accepted. Frontends must not put secrets (reset tokens, emails) in paths |
| Raw retention | 90 days by default, never under the recompute window (current and previous month, last 31 days) | Accepted |
| Referrers | Raw events keep scheme, host, and path (no query) until raw retention; rollups keep the host | Accepted |
| Exports | Rollups only, with query and path text subject to the rules above; password and 2FA step-up, audited, 15-minute links, archives deleted after 72 hours | Accepted |
| Live view | Paths and queries of the last 30 minutes, in memory only, for editors | Accepted |
| Ingest enumeration | Answers do not depend on content, users, bearer tokens, or unknown or refused consent tokens | Accepted; pinned by `TestAnalyticsIngestCannotEnumerateContentOrUsers` |

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

Both are opt-in, need `TEST_DATABASE_URL`, and should run without `-race`:

- **Ingest peak.** The target is 500 events per second across the five ingest routes with no
  dropped events. `TEST_ANALYTICS_LOAD=1 go test ./internal/bootstrap -run
  TestAnalyticsIngestSustainsPeakLoad` sends that for 10 seconds through the real router and
  writer (default queue, batch, and flush settings) into PostgreSQL and requires every response
  to be `202`, an accepted rate of at least 95% of the target, p99 under 250 ms, no drops, and
  every event stored (heartbeats merge per page view). On a laptop it measured p99 under 1 ms.
  Its rows are deleted afterwards.
- **Report latency.** `TEST_ANALYTICS_PERF=1 go test ./internal/adapters/outbound/persistence
  -run TestAnalyticsReportsStayFastOnTwoYearsOfRollups` seeds two years of rollups at the
  per-period caps (about 730k daily rows each for pages, transitions, and queries, plus weekly
  and monthly rows) inside a rolled-back transaction and requires every report to answer within
  300 ms (median of three) at 30 and 92 days daily, 365 and 731 days weekly, and 731 days
  monthly. It measured at most about 160 ms. Longer daily ranges are refused because their scans
  grow past the budget (about 1.4 s for navigation over 731 days).

Against a running stack, `app analytics loadtest --url http://host:8080 --rate 500 --duration
60s [--metrics-url http://host:9090/metrics] [--spread 200]` sends the same traffic mix and
fails on any non-`202` answer, a low accepted rate, a slow p99, or (with `--metrics-url`) a
rise in `blog_analytics_events_dropped_total`. It stores real events under a `/loadtest/...`
path prefix, so point it at a disposable or staging stack, with `ANALYTICS_INGEST_PER_MINUTE=0`
or with `--spread` from a proxy listed in `APP_TRUSTED_PROXIES`.
