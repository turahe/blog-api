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

- `analytics_page_views`
  - id
  - consent_token_id
  - session_id
  - path
  - referrer
  - country_code
  - device_type
  - user_agent_bucket
  - occurred_at

- `analytics_time_spent`
  - id
  - consent_token_id
  - session_id
  - path
  - started_at
  - ended_at
  - focus_seconds

- `analytics_navigation`
  - id
  - consent_token_id
  - session_id
  - from_path
  - to_path
  - transition_type
  - occurred_at

- `analytics_searches`
  - id
  - consent_token_id
  - session_id
  - normalized_query
  - result_count
  - filters_jsonb
  - occurred_at

- `analytics_search_clicks`
  - id
  - search_id (FK)
  - position
  - clicked_resource_type
  - clicked_resource_id
  - clicked_at

- `analytics_aggregates_daily`
  - date
  - metric_key (e.g. `page_views`, `unique_sessions`, `search_ctr`)
  - dimensions (JSONB for path, country, role, etc.)
  - value_numeric
  - value_jsonb
  - unique key: (date, metric_key, dimensions_hash)

### Indexing Recommendations

- btree on time columns for time-range filters
- composite index on (metric_key, date) for aggregates
- hash or GIN on dimensions for complex dashboard filters
- index on consent_token_id for right-to-erasure workflows

## API Endpoints

### Public Ingestion (Consent-Gated)

- `POST /api/v1/analytics/ingest/page-view`
  - accepts single batched page view record
  - 403 if consent is rejected/withdrawn
- `POST /api/v1/analytics/ingest/time-spent`
  - accepts heartbeat or final duration
- `POST /api/v1/analytics/ingest/navigation`
- `POST /api/v1/analytics/ingest/search`
- `POST /api/v1/analytics/ingest/search-click`

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

- `GET /api/v1/analytics/consent/:token`
- `POST /api/v1/analytics/consent`
  - grant or reject consent with scope flags
- `DELETE /api/v1/analytics/consent/:token`
  - withdraw consent and mark records eligible for erasure

## Real-Time Pipeline

Recommendation for live dashboard:

1. Ingestion API validates consent.
2. On success, persist raw event (write path).
3. Enqueue `analytics.event.ingested` via Watermill outbox.
4. Async handlers:
   - update Redis counters for live counts (last 5 min, last hour)
   - fan out per-topic events to admin SSE stream via pub/sub
5. Periodic `app scheduler` jobs roll up raw events into `analytics_aggregates_daily` for 7/30/90-day performance.

## Events

Domain and integration events:

- `analytics.consent.granted`
- `analytics.consent.rejected`
- `analytics.consent.withdrawn`
- `analytics.page_view.ingested`
- `analytics.time_spent.ingested`
- `analytics.navigation.ingested`
- `analytics.search.ingested`
- `analytics.search_click.ingested`
- `analytics.realtime.summary.tick`
- `analytics.aggregation.daily.completed`

Use the outbox pattern for persistence-to-event consistency.

## Jobs and Workers

- `analytics_ingestion_cleanup`
  - periodically drop queued events whose consent was rejected or withdrawn
- `analytics_daily_rollup`
  - roll up raw events into daily aggregates for 7/30/90-day views
- `analytics_retention_refresh`
  - refresh cohort retention materializations
- `analytics_privacy_retention`
  - enforce raw event retention and right-to-erasure schedules

Run background work via:
- `app worker` (Watermill consumers for async fan-out and rollups triggered by events)
- `app scheduler` (cron for nightly rollups, retention, periodic counter flushes)

## Validation and Error Handling

Validation rules:

- reject payloads longer than configured max size
- normalize `path` to canonical form, reject malformed URLs / suspicious characters
- clamp time-spent values within realistic min/max bounds
- clamp search result positions to 1..N range
- reject or drop events when consent token is invalid, rejected, or withdrawn

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
- allow deletion pipeline: mark tokens as erased, then delete/mask raw events and rebuild affected aggregates
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
