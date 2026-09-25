# Phase 6 — Analytics and Reporting

## Goal

Turn behaviour into insight: page view, time spent, and navigation telemetry; popular content
and retention widgets; search analytics and CTR reporting; real-time admin activity views; and
analytics data export with retention controls.
Index: [README.md](./README.md).

## Status

**In progress** — telemetry ingest, the aggregation pipeline, dashboard reporting, and the live
view are done: the five ingest routes store raw events, `app scheduler` rolls them up, the five
admin reports read the rollups, and the realtime stream counts broker-fed events in memory.
Export still returns `501`.

## Epic: telemetry ingest

- [x] Analytics event schema and storage — see [analytics.md](../backend/analytics.md)
      (migration 00032: one plain table per event type, deduplicated by client event id)
- [x] Analytics core module with an ingest port and a storage adapter
- [x] `analytics.ingest.page_view` — `POST /api/v1/analytics/ingest/page-view`
- [x] `analytics.ingest.time_spent` — `POST /api/v1/analytics/ingest/time-spent`
- [x] `analytics.ingest.navigation` — `POST /api/v1/analytics/ingest/navigation`
- [x] `analytics.ingest.search` — `POST /api/v1/analytics/ingest/search`
- [x] `analytics.ingest.search_click` — `POST /api/v1/analytics/ingest/search-click`
- [x] Reject or anonymise events when consent is absent (Phase 5 consent store): `403` while
      `analytics.consent_required` is on; otherwise stored without a subject link; refusals are
      dropped
- [x] Rate limit and size-cap unauthenticated ingest endpoints (`ANALYTICS_INGEST_PER_MINUTE`,
      8 KiB)
- [x] Batch or buffer writes so ingest never blocks on a slow database (bounded in-process queue,
      `ANALYTICS_QUEUE_SIZE`, `blog_analytics_events_dropped_total`)
- [x] Bot and crawler filtering (user agent, missing user agent, prefetch)
- [x] Session or visitor identity that does not require storing raw IP addresses (subject HMAC,
      or a daily HMAC of IP and user agent)

## Epic: aggregation pipeline

- [x] Rollup tables for daily, weekly, and monthly grains (migration 00033, bucketed in
      `site.timezone`; top 1000 per capped dimension plus `(other)`; consented-visitor cohorts)
- [x] Scheduled aggregation job running on the Phase 4 worker (`analytics-rollup` in
      `app scheduler`, every 15 minutes)
- [x] Idempotent recompute for a given time window (each period replaced in one transaction
      under an advisory lock)
- [x] Late-event handling policy for the current window (events are timed on receipt; each run
      also recomputes yesterday's periods, then they close)
- [x] Backfill command for recomputing history (`app analytics rollup --from --to`)

## Epic: admin dashboard reporting

- [x] `admin.analytics.overview` — `GET /api/v1/admin/analytics/overview`
- [x] `admin.analytics.pages` — `GET /api/v1/admin/analytics/pages`
- [x] `admin.analytics.navigation` — `GET /api/v1/admin/analytics/navigation`
- [x] `admin.analytics.retention` — `GET /api/v1/admin/analytics/retention`
- [x] `admin.analytics.search` — `GET /api/v1/admin/analytics/search` with CTR
- [x] Serve every dashboard read from rollups, never from raw events
- [x] Consistent date-range, timezone, and comparison-period semantics across operations
  (whole periods in `site.timezone`, labelled visitor sums, `compare=previous|none`) — see
  [analytics.md](../backend/analytics.md#admin-dashboard-rbac-protected)
- [x] Casbin permission for `analytics.read` (plus `analytics.search.read`; seeded for admins and
  editors)

Spec: [analytics-dashboard.md](../features/analytics-dashboard.md)

## Epic: real-time admin views

- [x] `admin.analytics.realtime.stream` — `GET /api/v1/admin/analytics/realtime/stream` (SSE,
  `analytics.realtime.read`, admins and editors)
- [x] Live counters fed from the Phase 4 broker rather than polling the database (broadcast
  topic `analytics.live`, batched once a second; each replica counts in memory)
- [x] Bounded time window with a documented refresh cadence (active sessions over 5 minutes,
  series and top pages over 30, a frame every 5 seconds) — see
  [analytics.md](../backend/analytics.md#live-stream)
- [x] Reuse the Phase 3 SSE lifecycle handling: heartbeat, disconnect cleanup, proxy buffering
  (per-user stream cap, `stream.closed` on shutdown, `X-Accel-Buffering: no`)

## Epic: export and retention

- [x] `admin.analytics.export` — `POST /api/v1/admin/analytics/export` (`analytics.export`,
  admins only, password and 2FA step-up; rollups only, as a ZIP of CSVs)
- [x] Asynchronous export producing a downloadable artefact in object storage (`analytics-exports`
  job, `analytics-exports/<id>.zip`; status via `GET /api/v1/admin/analytics/exports/{id}`)
- [x] Signed, expiring download links (`ANALYTICS_EXPORT_URL_TTL`, archive kept
  `ANALYTICS_EXPORT_RETENTION`)
- [x] Configurable raw-event retention with a pruning job (`analytics.raw_retention_days`,
  `analytics-retention` job)
- [x] Aggregate retention independent of raw retention (`analytics.rollup_day_retention_months`
  for daily rollups; weekly, monthly, and cohorts kept)
- [x] Document retention defaults in [analytics.md](../backend/analytics.md#retention)

## Dependencies and order

1. Consent storage from Phase 5 must exist before ingest is enabled.
2. Rollups block every dashboard operation.
3. The Phase 4 worker runtime blocks aggregation, real-time counters, and export.
4. Object storage from Phase 2 blocks export artefacts.
5. Search analytics depends on the Phase 5 search integration emitting query events.

## Cross-cutting

- [x] Bind analytics ingest and admin analytics handlers in `routes.Register*`, annotate them,
      then `make swagger` + `make routes-check`
- [x] Load test ingest at expected peak write volume
- [x] Query-performance tests on rollup reads with realistic row counts
- [x] Privacy review: retention, anonymisation, and export scope
- [x] Verify ingest endpoints cannot be used to enumerate content or users
- [x] Add analytics dashboards or alerts to the operational runbook

## References

| Topic | Doc |
| --- | --- |
| Analytics | [analytics.md](../backend/analytics.md) |
| API surface | [api.md](../backend/api.md) |
| Events | [events.md](../backend/events.md) |
| Jobs | [jobs.md](../backend/jobs.md) |
| Security overview | [overview.md](../security/overview.md) |
| Testing strategy | [strategy.md](../testing/strategy.md) |
