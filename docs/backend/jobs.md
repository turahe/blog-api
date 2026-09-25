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
- clean orphaned media metadata and storage objects (`media-orphans`)
- roll analytics events up into daily, weekly, and monthly aggregates (`analytics-rollup`)
- build admin analytics exports and prune analytics data (`analytics-exports`,
  `analytics-retention`)
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
- recompute analytics rollups for a date range with
  `app analytics rollup --from YYYY-MM-DD [--to YYYY-MM-DD]` (see
  [analytics.md](analytics.md#aggregation))
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
| `auth-tokens-prune` | 1h | Deletes refresh sessions 30 days past expiry and reset/verification tokens 7 days past expiry, and expired pending sign-ups (`registrations`) |
| `privacy-requests` | 1m | Runs queued `/me` data exports and erasures (up to 10 per run, 3 attempts each; a job stuck running for 15 minutes is retried) and deletes export archives past `PRIVACY_EXPORT_RETENTION` |
| `impersonation-expire` | 1m | Closes impersonation sessions past `expires_at` (up to 500 per run) and records `blog.impersonation.expired`; the tokens already stopped working at expiry |
| `newsletter-release` | 1m | Queues scheduled newsletter issues whose `send_at` has passed (up to 50 per run) and records `blog.newsletter.issue.send_requested` |
| `newsletter-tokens-prune` | 1h | Deletes newsletter confirm, unsubscribe, and preferences tokens 30 days past expiry |
| `analytics-rollup` | 15m | Recomputes the analytics rollups of the periods containing today or yesterday and the retention cohorts of the last 31 days; rebuilds everything the raw events cover on its first run or after `site.timezone` changes. See [analytics.md](analytics.md#aggregation) |
| `analytics-exports` | 1m | Builds queued admin analytics exports (up to 2 per run, 3 attempts each; a job stuck running for 30 minutes is retried) and deletes archives past `ANALYTICS_EXPORT_RETENTION`. See [analytics.md](analytics.md#export) |
| `analytics-retention` | 1h | Deletes anonymous visitor salts older than yesterday (UTC), raw analytics events past `analytics.raw_retention_days` (never the current or previous month or the last 31 days) and daily rollups past `analytics.rollup_day_retention_months`; weekly and monthly rollups are kept. See [analytics.md](analytics.md#retention) |
| `media-orphans` | 1h | Deletes the object and row of uploads never completed 24h after their presign expired, and of assets soft-deleted longer than `MEDIA_PURGE_AFTER` (up to 200 of each per run); skipped when media storage is not configured. See [media.md](media.md#orphan-cleanup) |

Newsletter issues are sent by the `newsletter-dispatch` consumer in `app worker`, not by the
scheduler; see [newsletter.md](newsletter.md#dispatch).

Published outbox rows are pruned by the relay in `app worker` (`OUTBOX_RETENTION`).
