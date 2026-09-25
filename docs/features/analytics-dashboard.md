# Analytics Dashboard

## Feature Summary

Provide admin-only analytics that summarize how readers discover and interact with the documentation/blog surfaces: traffic, engagement, popular content, retention, search behavior, and live activity.

This feature is administrative and analytical. It does not change public content behavior, only adds measurement and a secure admin dashboard surface.

## Core Analytics Features

### Engagement Metrics

Track, at minimum:

- **page views** per route/page, with deduplication where appropriate
- **time spent on page** using heartbeats or focus-based intervals
- **section navigation patterns** such as entry path, exit path, and flow between pages
- aggregate metrics by day, week, month, and custom ranges

### Data Visualization Components

The dashboard should provide these widgets:

- **Statistics cards**
  - total page views
  - unique visitors
  - average time spent
  - bounce rate approximation
  - searches performed
  - search CTR (click-through rate)
- **Traffic trends**
  - line or area chart for 7-day, 30-day, and 90-day views
  - filterable by route, referrer, and country when available
- **Popular documentation pages**
  - top N pages by views
  - top N pages by average time spent
  - fastest-rising pages in the selected window
- **Retention rates**
  - day 1, day 7, and day 30 retention cohorts
  - returning vs new visitor split
- **Navigation patterns**
  - sankey or flow visualization for section-to-section transitions
  - entry pages and exit pages top lists

### Search Analytics

Track:

- raw search queries normalized for casing/whitespace
- search frequency over time
- top zero-result queries
- top clicked results per query
- CTR per result position
- time-to-click after query

## Admin UX Rules

- dashboard must be responsive for desktop and tablet, with mobile best-effort readability
- respect WCAG 2.1 AA accessibility where applicable:
  - semantic headings
  - descriptive labels for charts
  - keyboard-navigable date and filter controls
  - sufficient contrast for stat cards and chart colors
- all dates and windows should use explicit timezone controls and clearly state their window
- empty states must explain why no data is available (short time window, consent disabled, no traffic yet)

## Privacy and Consent

Compliance goals for GDPR and CCPA-like regimes:

- analytics collection is **opt-in** by default
- consent choices are persisted per browser
- provide a banner or privacy controls UI with Accept/Reject and granular options
- if consent is rejected or withdrawn:
  - do not emit analytics events from the client
  - drop any queued events server side before persistence
  - allow server-side right-to-erasure workflow
- never collect:
  - full IP addresses longer than needed without truncation/masking
  - exact user agent strings longer than necessary for high-level categorization
  - sensitive query parameters, tokens, credentials, or auth headers
- allow export and deletion of analytics records tied to a consent identifier when requested

## Access Controls

RBAC for analytics:

- **admin** role: full access to all dashboards, filters, exports
- **editor**: read-only reports (`analytics.read`, `analytics.search.read`); no exports
- default policy: deny by default; only explicit permissions grant access

Suggested permission keys:

- `analytics.read`
- `analytics.export`
- `analytics.search.read`
- `analytics.realtime.read`

Sensitive operations (export, deletion of records, retention changes) require recent strong authentication, including 2FA where enabled.

## Real-Time Updates

Dashboard requirements:

- provide a **Live** mode that updates metrics without a full page reload
- prefer SSE for live event feeds to keep implementation consistent with other notification streams in the system
- live view should show, at minimum:
  - active sessions count
  - recent page views stream
  - recent searches stream
  - active top pages in the last N minutes
- historical views (7/30/90 days) rely on pre-aggregated or on-demand analytics query APIs, not live streams alone

## Testing Expectations

Test coverage should include:

- **End-to-end**
  - page view tracking fires only with consent
  - time-spent and navigation events are recorded correctly
  - search events and click-through tracking aggregate correctly
  - charts render with realistic fixture data
  - 7/30/90-day range filters produce the right windows
- **Performance**
  - analytics telemetry injection must not block first paint
  - dashboard charts must render within acceptable latency targets against realistic dataset sizes
  - SSE live feed must stay within memory and connection limits
- **Security/Access**
  - unauthenticated users cannot read dashboard APIs
  - non-admin users with no `analytics.*` permissions receive 403
  - export endpoints require stronger auth step-up if configured
  - no analytics payloads persist when consent is rejected

## Maintenance and Operations

- define retention policies for raw events vs aggregated rollups
- define aggregation cadences for hourly/daily rollups
- monitor analytics ingestion lag and query latency
- provide a way to disable analytics globally via feature flag
- document dashboards, KPI definitions, filters, and any data caveats in an admin-facing runbook section
