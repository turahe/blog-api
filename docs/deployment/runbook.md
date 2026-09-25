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
