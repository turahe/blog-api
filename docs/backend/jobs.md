# Jobs

## Canonical Contract

Job topics and command messages are defined in the AsyncAPI contract:

- [contracts/asyncapi.yaml](file:///mnt/myadrive/repo/turahe/blog-api/contracts/asyncapi.yaml)

## Purpose

Jobs handle asynchronous and retryable work that should not block request-response flows.

## Candidate Jobs

- publish outbox events
- invalidate or warm caches
- send notification emails
- process moderation side effects
- run periodic cleanup for expired session or recovery state
- evict expired transformed media cache entries
- clean orphaned media metadata or storage objects after failed workflows
- materialize daily analytics aggregates
- erase analytics data per withdrawn consent token
- expire impersonation sessions (periodic sweeper)
- rebuild settings cache after invalidation

## Execution Model

- prefer idempotent job handlers
- include retry policy
- log failures with enough context for troubleshooting
- keep job payloads minimal and versionable
- match topic names and payloads in the AsyncAPI contract

## Entrypoints

- run async event/job workers with `app worker`
- run scheduled/cron jobs with `app scheduler`
- validate worker/scheduler health before startup using `app doctor`
