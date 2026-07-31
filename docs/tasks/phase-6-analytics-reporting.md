# Phase 6 — Analytics and Reporting

## Goal

Turn behaviour into insight: page view, time spent, and navigation telemetry; popular content
and retention widgets; search analytics and CTR reporting; real-time admin activity views; and
analytics data export with retention controls.
Index: [README.md](./README.md).

## Status

**Planned** — all 8 public analytics operations and all 7 admin analytics operations return
`501`. There is no analytics schema, ingest path, or aggregation layer.

## Epic: telemetry ingest

- [ ] Analytics event schema and storage — see [analytics.md](../backend/analytics.md)
- [ ] Analytics core module with an ingest port and a storage adapter
- [ ] `analytics.ingest.page_view` — `POST /api/v1/analytics/ingest/page-view`
- [ ] `analytics.ingest.time_spent` — `POST /api/v1/analytics/ingest/time-spent`
- [ ] `analytics.ingest.navigation` — `POST /api/v1/analytics/ingest/navigation`
- [ ] `analytics.ingest.search` — `POST /api/v1/analytics/ingest/search`
- [ ] `analytics.ingest.search_click` — `POST /api/v1/analytics/ingest/search-click`
- [ ] Reject or anonymise events when consent is absent (Phase 5 consent store)
- [ ] Rate limit and size-cap unauthenticated ingest endpoints
- [ ] Batch or buffer writes so ingest never blocks on a slow database
- [ ] Bot and crawler filtering
- [ ] Session or visitor identity that does not require storing raw IP addresses

## Epic: aggregation pipeline

- [ ] Rollup tables for daily, weekly, and monthly grains
- [ ] Scheduled aggregation job running on the Phase 4 worker
- [ ] Idempotent recompute for a given time window
- [ ] Late-event handling policy for the current window
- [ ] Backfill command for recomputing history

## Epic: admin dashboard reporting

- [ ] `admin.analytics.overview` — `GET /api/v1/admin/analytics/overview`
- [ ] `admin.analytics.pages` — `GET /api/v1/admin/analytics/pages`
- [ ] `admin.analytics.navigation` — `GET /api/v1/admin/analytics/navigation`
- [ ] `admin.analytics.retention` — `GET /api/v1/admin/analytics/retention`
- [ ] `admin.analytics.search` — `GET /api/v1/admin/analytics/search` with CTR
- [ ] Serve every dashboard read from rollups, never from raw events
- [ ] Consistent date-range, timezone, and comparison-period semantics across operations
- [ ] Casbin permission for `analytics.read`

Spec: [analytics-dashboard.md](../features/analytics-dashboard.md)

## Epic: real-time admin views

- [ ] `admin.analytics.realtime.stream` — `GET /api/v1/admin/analytics/realtime/stream` (SSE)
- [ ] Live counters fed from the Phase 4 broker rather than polling the database
- [ ] Bounded time window with a documented refresh cadence
- [ ] Reuse the Phase 3 SSE lifecycle handling: heartbeat, disconnect cleanup, proxy buffering

## Epic: export and retention

- [ ] `admin.analytics.export` — `POST /api/v1/admin/analytics/export`
- [ ] Asynchronous export producing a downloadable artefact in object storage
- [ ] Signed, expiring download links
- [ ] Configurable raw-event retention with a pruning job
- [ ] Aggregate retention independent of raw retention
- [ ] Document retention defaults in [analytics.md](../backend/analytics.md)

## Dependencies and order

1. Consent storage from Phase 5 must exist before ingest is enabled.
2. Rollups block every dashboard operation.
3. The Phase 4 worker runtime blocks aggregation, real-time counters, and export.
4. Object storage from Phase 2 blocks export artefacts.
5. Search analytics depends on the Phase 5 search integration emitting query events.

## Cross-cutting

- [ ] Update `paths/analytics.yaml` and the admin analytics paths first, then `make routes`
- [ ] Load test ingest at expected peak write volume
- [ ] Query-performance tests on rollup reads with realistic row counts
- [ ] Privacy review: retention, anonymisation, and export scope
- [ ] Verify ingest endpoints cannot be used to enumerate content or users
- [ ] Add analytics dashboards or alerts to the operational runbook

## References

| Topic | Doc |
| --- | --- |
| Analytics | [analytics.md](../backend/analytics.md) |
| API surface | [api.md](../backend/api.md) |
| Events | [events.md](../backend/events.md) |
| Jobs | [jobs.md](../backend/jobs.md) |
| Security overview | [overview.md](../security/overview.md) |
| Testing strategy | [strategy.md](../testing/strategy.md) |
