# Security Overview

## Trust boundaries

| Boundary | Inside | Outside |
| --- | --- | --- |
| Public edge | Gin router, CORS allowlist, rate limits | Browsers, bots, third-party clients |
| Auth boundary | JWT / session validation, CSRF for browsers | Unauthenticated callers |
| Admin boundary | RBAC (`settings.*`, `user.*`, `impersonation.*`, …) | Authenticated non-admin users |
| Data plane | Postgres, Redis, object storage, Watermill | App process only via adapters |

## Non-negotiables

- authorize on the server; never trust client roles, user IDs, or media URLs
- never log passwords, tokens, TOTP secrets, backup codes, or decrypted PII
- state-changing browser requests require CSRF; API tokens only when explicitly allowlisted
- high-risk actions (password/email change, privacy tighten, impersonation start, admin profile edit) require step-up
- contract `security` blocks drive auth mode (`required` / `none` / `optional`); see [api.md](../backend/api.md)
- `server_only` settings and infrastructure secrets never leave the API

## Threat classes (priority)

1. Privilege escalation (RBAC gaps, impersonation leaks, IDOR on `/me` and `/admin`)
2. Auth abuse (credential stuffing, reset enumeration, 2FA bypass)
3. Injection / unsafe media (upload type/size, transform params, malware)
4. Data exposure (privacy toggles, audit oversharing, SSE fan-out to wrong user)
5. Availability (unbounded SSE connections, analytics ingest floods)

## Abuse and spam

Comments are the main surface anyone can write to. The controls stack, cheapest first:

| Control | Behavior | Configuration |
| --- | --- | --- |
| Rate limits | Redis per-minute budgets per signed-in user, else per client IP: `comments.create`, and a shared actions budget for `comments.flag` and `comments.upvote`. Over budget returns `429` with `Retry-After`. If Redis is down, requests are let through and a warning is logged. | `COMMENTS_CREATE_PER_MINUTE` (6), `COMMENTS_ACTIONS_PER_MINUTE` (30); `0` disables |
| Post policy | Each post is `open`, `authenticated` (no guests), or `disabled` (hidden, no writes). | `comment_policy` on the post |
| Guest gate | Guests are off by default. When enabled they must give a name and email, and their comments always start `pending`. | `COMMENTS_GUEST_ENABLED`, `COMMENTS_REQUIRE_APPROVAL` (also holds signed-in comments) |
| Captcha | When set, guest creates need a valid Cloudflare Turnstile token (`400 comments.spam.challenge_invalid`). If Turnstile can't be reached, the request fails with `503` rather than skipping the check. Signed-in users skip the captcha. | `TURNSTILE_SECRET_KEY` |
| Honeypot | A filled hidden `honeypot` field stores the comment as `spam` and skips the captcha. The response says `pending`, so bots get no signal. | — |
| Content limits | 10,000 runes, reply depth 5, and markdown rendered to an allow-listed HTML subset (`content_html`), which is the field clients should display. | — |
| Community flags | One flag per user, or per hash of IP + user agent for guests. When the flag count reaches the threshold, an `approved` comment moves to `flagged` and leaves public view until a moderator acts. | `COMMENTS_FLAG_THRESHOLD` (3) |
| Moderation | `comment.moderate` holders approve, reject, or mark spam, singly or in bulk, with an append-only log; `comment.delete` hard-deletes (scrubbing parents so replies survive). Authors can be notified of the outcome. | RBAC |

Privacy: client IPs are only stored as hashes (`ip_hash`; see [Privacy stance](#privacy-stance)),
and user agents are truncated. Hard delete clears both.

## Privacy stance

The API collects the least personal data that each feature needs, keeps it no longer than it
needs, and lets the data subject, and only the data subject, decide on consent.

- **Consent comes from the data subject.** Analytics consent (`/api/v1/analytics/consent`) and
  newsletter subscriptions (`/api/v1/me/newsletter/*`) cannot be given or withdrawn with an
  impersonation token (`403 impersonation.forbidden_action`), and neither can privacy settings,
  data export, or erasure. Staff manage newsletter subscribers through the admin API, which is
  audited, never by acting as the user.
- **Pseudonymous consent.** An anonymous visitor's consent is keyed by a random token the browser
  holds (`X-Consent-Token`). Only its SHA-256 hash is stored, so the table cannot be used to
  resume someone else's consent. Linking consent to an account needs a signed-in user and a
  separate `authenticated_analytics` grant. Consent responses are `Cache-Control: no-store`,
  because one carries the token and the others are keyed by a header shared caches ignore.
- **Tokens at rest.** Newsletter confirm, preference, and unsubscribe tokens and consent tokens are
  32 random bytes, stored only as SHA-256 hashes, bound to one purpose, and expiring; confirmation
  links are single-use. Refresh and reset tokens use a keyed hash. None appear in logs: access
  logs and traces record the route pattern rather than the URL, and error reports drop query
  strings, cookies, bodies, and token headers.
- **IP addresses.** Comments, guest flags, and newsletter consent evidence store a hash of the
  client IP, never the address. With `APP_ENCRYPTION_KEY` set the hash is an HMAC-SHA256 under a
  key derived from it, so a leaked table cannot be reversed by hashing the small IPv4 space.
  Without the key it falls back to plain SHA-256, which is reversible, and startup logs a warning.
  Rows written before the key was set keep their old hashes, so a guest may flag the same comment
  once more after the switch.
- **Analytics telemetry.** Raw events store neither the IP address nor the user agent: only a
  path without its query string, a device class, a browser family, an optional country from a
  trusted proxy header, and a visitor hash. For a visitor without granted consent that hash is an
  HMAC of a random per-day salt, the IP, and the user agent, so their visits cannot be linked
  across days or to an account, and once the salt is destroyed (after a day) not even a holder of
  `APP_ENCRYPTION_KEY` can test a guessed IP against old hashes; with no `APP_ENCRYPTION_KEY` the
  HMAC key is random per process and never stored. Search queries, which visitors type freely,
  are kept by name in rollups and exports only when two distinct visitors searched them on one
  day. Withdrawing analytics consent deletes the subject's stored events. Ingest answers `202`
  whether or not an event is kept (bots, prefetches, and refusing subjects are dropped), so it
  cannot be used to probe users or content. See [analytics.md](../backend/analytics.md), whose
  privacy review lists what is kept and why.
- **Retention.** Audit entries are kept for `AUDIT_RETENTION_DAYS` (395). Data exports stay in
  object storage for `PRIVACY_EXPORT_RETENTION` (72h); the download link is presigned for at most
  `PRIVACY_EXPORT_URL_TTL` (15m) and never past the export's own expiry, and the response is
  `no-store`. Admin analytics exports hold rollups only (no raw events, visitor hashes, or
  session ids), need a password (and 2FA) step-up that is audited even when refused, and follow
  the same pattern with `ANALYTICS_EXPORT_RETENTION` (72h) and `ANALYTICS_EXPORT_URL_TTL` (15m).
  Raw analytics events are pruned after `analytics.raw_retention_days` (90) and daily rollups
  after `analytics.rollup_day_retention_months` (25). Erasure anonymizes the account, deletes its analytics consents with the raw events
  and first-seen records linked to them (rollups hold only counts), and erases its
  newsletter subscriber (personal data, feedback, and IP hashes cleared; tokens deleted), keeping
  only the append-only consent history the law requires.
- **Impersonation.** Staff acting as a user are bound to their own sign-in, see a response header
  naming the session (`X-Impersonation-Session`), and every request they make is audited with
  both identities. See [impersonation.md](../backend/impersonation.md).

Known gaps: there is no content classifier, link or keyword filter, or reputation score, and no
alerting on abuse bursts. Watch the queue depth in `/admin/comments/stats`.

## Where enforcement lives

| Concern | Layer |
| --- | --- |
| Auth mode + route group chains | `internal/adapters/inbound/http` + generated `v1` routes |
| Password / session / 2FA rules | `internal/core/auth` (when implemented) |
| Permission checks | middleware + service (Casbin); never UI-only |
| Input allowlists | HTTP adapters + validation docs |
| Audit | service layer on every security-sensitive mutation |

Full policy detail: [security.md](../architecture/security.md).
