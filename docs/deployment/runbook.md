# Runbook

What to check and do when the event pipeline or the API is under stress. Processes and probes
are described in [deployment.md](../architecture/deployment.md#process-roles); settings in
[config.md](./config.md).

## Broker outage

**Signals:** worker `GET /readyz` answers 503 with `messaging` unhealthy; API
`GET /health/ready` still answers 200 but lists `messaging` as `"healthy": false,
"critical": false`; `blog_outbox_publish_failures_total` rises; `blog_outbox_lag_seconds`
grows.

**Impact:** none for API writes. Events and queued emails wait in `outbox_events`; SSE
notifications from other replicas stop arriving until the broker is back.

**Do:**

1. Confirm with `app doctor` from a worker host, then fix the broker or the network path.
2. Do not restart API replicas; they stay in rotation on purpose.
3. After recovery, watch `blog_outbox_pending` fall. Rows that exhausted
   `OUTBOX_MAX_ATTEMPTS` during a long outage are parked: check `app outbox status` and run
   `app outbox retry`.

## Outbox backlog

**Signals:** `blog_outbox_pending` or `blog_outbox_lag_seconds` keeps growing while the broker
is healthy; `app outbox status` shows many pending rows.

**Do:**

1. Check that at least one `app worker` is running and ready. Without a worker nothing
   publishes.
2. Check worker logs for publish errors (topic permissions, message size, broker quotas).
3. Scale workers out: relays claim rows with `FOR UPDATE SKIP LOCKED`, so replicas share the
   backlog. Raising `OUTBOX_BATCH_SIZE` helps when each transaction is small.
4. For parked rows (`blog_outbox_failed` > 0), fix the cause and run `app outbox retry`.

## Consumer crash loops

**Signals:** worker restarts repeatedly; logs show panics or `router:` errors; the same message
id appears in many attempts.

**Do:**

1. Read the last error before the restart. A startup failure (bad `APP_ENCRYPTION_KEY`,
   `SMTP_*`, database DSN) exits before consuming: fix the config.
2. A panicking handler is recovered, retried, then sent to `blog.dead_letter`, so it should
   not crash the process. If it does, roll the worker back to the previous image; messages
   stay in the broker until a healthy worker acks them.
3. Scale the worker to zero if it harms downstream systems; the outbox and the broker buffer
   the work.

## Consumer circuit breaker open

**Signals:** log `consumer circuit breaker changed state` with `to=open`; the consumer stops
sending while messages stay in the broker.

**Cause:** `CONSUMER_BREAKER_FAILURES` consecutive transient failures, usually SMTP or database
unavailability. Permanent errors (for example an undecryptable email command) do not count.

**Do:** fix the downstream. Every `CONSUMER_BREAKER_TIMEOUT` one trial message goes through;
the first success closes the breaker and delivery resumes. Messages refused while open are
nacked and redelivered, not dead-lettered.

## Dead letters

Messages that failed every retry land on `blog.dead_letter` (with `MESSAGE_TOPIC_PREFIX`),
carrying `reason_poisoned`, `topic_poisoned`, and `handler_poisoned` headers. Inspect them with
broker tools, fix the cause, and republish to the original topic. Handlers are idempotent, so
replaying an already handled message is safe.

## API overload

**Signals:** `503 server.overloaded` responses; `blog_http_requests_in_flight` pinned at
`HTTP_MAX_INFLIGHT`.

**Do:** scale API replicas out, or look for a slow dependency (database, Redis) that holds
requests open. Raise `HTTP_MAX_INFLIGHT` only when the database pool (`DB_POOL_MAX_OPEN`) and
memory have headroom; shedding exists so a replica fails fast instead of timing out every
request.

## Analytics

Analytics never blocks a page: ingest answers `202` and a background writer inserts events in
batches, so trouble shows up as lost or stale numbers rather than failed requests. Details:
[analytics.md](../backend/analytics.md).

**Alerts worth wiring:**

| Signal | Suggested alert |
| --- | --- |
| `rate(blog_analytics_events_dropped_total[5m]) > 0` for 10 minutes | Events are being lost |
| `app scheduler status` shows a `LAST ERROR` for `analytics-rollup`, or its `LAST STARTED` is older than 1 hour | Dashboards are going stale |
| Log `scheduled job failed` with `job=analytics-exports` or `job=analytics-retention` | Exports or pruning stopped |
| Log `analytics visitor salt unavailable` for more than a few minutes | Unique-visitor counts drift |
| `429` rate on `/api/v1/analytics/ingest/*` well above its usual level | Limit too tight, or abuse |

### Dropped analytics events

**Signals:** `blog_analytics_events_dropped_total` rises; API logs `analytics queue full`,
`analytics insert failed`, or `analytics writer closed` (at most one line every few seconds,
counts only).

**Do:**

1. `analytics insert failed` means the database refused the batch: check its health and the
   error attached to the log line (a failed migration shows up here as a missing table).
2. `analytics queue full` means the writer cannot keep up with the replica's traffic. Check
   database latency first, then scale API replicas out (each has its own writer) or raise
   `ANALYTICS_QUEUE_SIZE` when memory allows.
3. `analytics writer closed` during a deploy is expected: events arriving while a replica
   shuts down are dropped.
4. Lost events are not replayed. Rollups count what was stored, so note the gap for readers
   of the dashboards.

### Stale dashboards or failed analytics jobs

**Signals:** the dashboard stops moving; `app scheduler status` shows a `LAST ERROR` for an
`analytics-*` job; logs show `scheduled job failed`.

**Do:**

1. Check that one `app scheduler` is running. Jobs take a PostgreSQL advisory lock, so extra
   schedulers are safe but idle.
2. Fix the reported error and run the job once with `app scheduler run analytics-rollup`
   (or `analytics-exports`, `analytics-retention`).
3. The rollup job only keeps today and yesterday current. After an outage longer than a day, or
   after restoring raw events, recompute the gap with
   `app analytics rollup --from YYYY-MM-DD --to YYYY-MM-DD`; it is idempotent.
4. A failed export is retried 3 times and then shows `failed` with its `last_error`; the
   requester can queue a new one.
5. If `analytics-retention` stays broken, raw events and old daily rollups stop being pruned,
   which breaks the published retention promise: treat it as a privacy issue, not only a disk
   one.

### Visitor salt unavailable

**Signals:** API logs `analytics visitor salt unavailable; using a replica-local salt`.

**Impact:** each replica keys anonymous visitor hashes with its own salt for the day, so one
visitor behind a load balancer may be counted once per replica until the shared salt loads.
Ingest keeps working, and each replica retries every 10 seconds.

**Do:** fix database connectivity from the API. Nothing needs cleaning up afterwards; the
affected day's unique-visitor counts are slightly high.

### Load test before a traffic change

Before a launch or a capacity change, run
`app analytics loadtest --url https://staging.example --rate 500 --duration 5m --metrics-url
https://staging.example:9090/metrics` against a staging stack (it stores real events under
`/loadtest/...`; set `ANALYTICS_INGEST_PER_MINUTE=0` there or pass `--spread` from a trusted
proxy). It fails on any non-`202` answer, an accepted rate below 95% of the target, p99 above
250 ms, or dropped events. See
[analytics.md](../backend/analytics.md#testing-strategy).
