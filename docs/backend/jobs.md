# Jobs

## Canonical Contract

Job topics and command messages are defined in the AsyncAPI contract:

- [asyncapi.yaml](../architecture/asyncapi.yaml)

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

- run async event/job workers with `app worker` (outbox relay and message consumers)
- run recurring jobs with `app scheduler`; `app scheduler status` shows the last run of each
  job and `app scheduler run <job>` runs one now
- prune audit rows once with `app audit prune [--older-than-days N]`
- rebuild the post search index after changing `SEARCH_LANGUAGE` with `app search reindex`
  (see [search.md](search.md))
- validate worker/scheduler health before startup using `app doctor`

## Scheduled Jobs

`app scheduler` needs only the database. Run one or more replicas: before each run a replica
takes a PostgreSQL advisory lock for the job (`pg_try_advisory_lock` on a pinned connection)
and skips the job when `scheduled_job_runs.last_started_at` is less than the interval ago. A
job therefore runs once per interval however many replicas are up, and a replica that dies
mid-run releases the lock with its connection. Failures are logged at Error level and stored
in `last_error`; the job is tried again at its next interval.

| Job | Every | Does |
| --- | --- | --- |
| `audit-prune` | 1h | Deletes audit rows older than `AUDIT_RETENTION_DAYS` |
| `processed-messages-prune` | 1h | Deletes consumer dedupe rows older than `CONSUMER_DEDUPE_RETENTION` |
| `auth-tokens-prune` | 1h | Deletes refresh sessions 30 days past expiry and reset/verification tokens 7 days past expiry |
| `privacy-requests` | 1m | Runs queued `/me` data exports and erasures (up to 10 per run, 3 attempts each; a job stuck running for 15 minutes is retried) and deletes export archives past `PRIVACY_EXPORT_RETENTION` |

Published outbox rows are pruned by the relay in `app worker` (`OUTBOX_RETENTION`).
