# Phase 4 — Event-Driven Platform

## Goal

Make domain events first-class: Watermill publishing, a durable outbox, workers and jobs, a
transformed-media cache strategy, and operational hardening.
Index: [README.md](./README.md).

## Status

**Partial** — the multi-broker Watermill layer and the `app worker` command exist. The
transactional outbox is in place: services can record events inside a unit of work, and the
worker relays them with retries, parking, pruning, and metrics. No service records domain
events yet, and the worker's only consumer is a heartbeat handler.

## Epic: messaging transports

- [x] `internal/platform/messaging` factory selecting a transport via `MESSAGE_BROKER`
- [x] Kafka publisher and subscriber via `watermill-kafka/v3`
- [x] RabbitMQ publisher and subscriber via `watermill-amqp/v3` durable pub/sub
- [x] Google Cloud Pub/Sub publisher and subscriber via `watermill-googlecloud/v2`
- [x] Broker alias normalization (`amqp`, `rabbit`, `pubsub`, `gcp-pubsub`)
- [x] `ValidateMessaging` enforces per-broker required environment variables
- [x] Topic prefixing through `MESSAGE_TOPIC_PREFIX`
- [x] Kafka and RabbitMQ in `compose.yaml` under the `messaging` profile
- [x] `make infra-up-messaging` / `make infra-down-messaging`
- [x] `app doctor` probes broker reachability when messaging is enabled
- [x] Document per-broker delivery semantics and ordering guarantees in [events.md](../backend/events.md#delivery)
- [ ] Add a dead-letter or parking-lot topic per broker
- [x] Decide and document the message envelope: schema version, event ID, occurred-at, actor
      (broker headers per the AsyncAPI `EventEnvelope` trait; [events.md](../backend/events.md#envelope))

Design: [2026-07-30-messaging-brokers-design.md](../superpowers/specs/2026-07-30-messaging-brokers-design.md)

## Epic: domain event publishing

- [x] `outbox_events` table from migration `00001`
- [ ] Event catalogue matching the channels in [asyncapi.yaml](../architecture/asyncapi.yaml)
- [x] Outbound event-publisher port in the core layer with no Watermill import (`internal/core/event`)
- [ ] Publish `post.published` from the post service
- [ ] Publish `post.created` and `post.updated`
- [ ] Publish `comment.created` and `comment.moderated` (needs Phase 3)
- [ ] Publish `user.registered` and `user.password_reset_requested`
- [ ] Publish `media.uploaded` and `media.transform_requested` (needs Phase 2 media)
- [ ] Keep [asyncapi.yaml](../architecture/asyncapi.yaml) authoritative and validate it in CI

## Epic: durable outbox

- [x] Outbox writer that appends to `outbox_events` inside the business transaction
- [x] Unit-of-work helper so services enlist the outbox write atomically (`persistence.Transactor`)
- [x] Relay loop in `app worker` that claims unpublished rows and publishes to the broker
- [x] Row claiming that is safe across replicas (`FOR UPDATE SKIP LOCKED`; PostgreSQL is the
      only supported database)
- [x] Retry with exponential backoff and a `failed` terminal state after N attempts
      (migration `00020_outbox_relay.sql`; `app outbox retry` requeues parked rows)
- [x] Idempotency key on each event so consumers can dedupe (the `id` header and message UUID)
- [x] Pruning of successfully published rows on a configurable retention window (`OUTBOX_RETENTION`)
- [x] Metrics or log counters for outbox lag, backlog depth, and failure rate (`blog_outbox_*`)

## Epic: workers and jobs

- [x] `app worker` command bootstrapping a Watermill router with graceful shutdown
- [ ] Replace the heartbeat handler with real subscriptions per event channel
- [ ] Consumer middleware: correlation ID propagation, panic recovery, retry, and poison queue
- [ ] Scheduled jobs runner for pruning, digests, and retention tasks — see [jobs.md](../backend/jobs.md)
- [ ] Single-flight or leader election so scheduled jobs do not double-run across replicas
- [ ] Notification fan-out consumer feeding the Phase 3 SSE streams
- [ ] Email dispatch consumer replacing inline mail sends

## Epic: transformed media cache

- [ ] Decide the transform pipeline: on-demand with cache, or asynchronous pre-generation
- [ ] Cache transformed derivatives in object storage keyed by source ID plus transform params
- [ ] Signed or validated transform parameters so the endpoint cannot be used to burn CPU
- [ ] Invalidate derivatives when the source media is replaced or deleted
- [ ] Document the strategy in [media.md](../backend/media.md)

## Epic: operational hardening

- [ ] Readiness gate that fails when the broker is configured but unreachable
- [ ] Backpressure and consumer concurrency limits driven by config
- [ ] Runbook entries for broker outage, outbox backlog, and consumer crash loops
- [ ] Load-shedding or circuit breaking on downstream failures
- [ ] Deployment notes for running `serve` and `worker` as separate processes —
      see [deployment.md](../architecture/deployment.md)

## Dependencies and order

1. The message envelope decision must precede publishing any event.
2. The outbox writer must land before consumers, so no event is lost on crash.
3. Dialect-safe row claiming blocks running the relay on more than one replica.
4. Comment and media events depend on Phase 3 and Phase 2 respectively.
5. The transformed-media cache depends on the Phase 2 object-storage adapter.
6. Real consumers must exist before the notification SSE fan-out can work across replicas.

## Cross-cutting

- [x] Unit tests for broker normalization, misconfiguration, and topic prefixing
- [ ] Integration tests against Compose Kafka and RabbitMQ, skipped when brokers are absent
- [x] Outbox tests proving events commit and roll back with the business write
- [ ] Consumer idempotency tests using duplicate deliveries
- [ ] Secrets review for broker credentials — see [secrets-and-headers.md](../security/secrets-and-headers.md)
- [ ] Update [checklist.md](../deployment/checklist.md) with worker rollout steps

## References

| Topic | Doc |
| --- | --- |
| Events and channels | [events.md](../backend/events.md) |
| Jobs | [jobs.md](../backend/jobs.md) |
| Tech stack | [tech-stack.md](../architecture/tech-stack.md) |
| Architecture | [architecture.md](../architecture/architecture.md) |
| Deployment | [deployment.md](../architecture/deployment.md) |
| Local brokers | [local.md](../deployment/local.md) |
