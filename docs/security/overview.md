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
| Community flags | One flag per user, or per SHA-256(IP + user agent) for guests. When the flag count reaches the threshold, an `approved` comment moves to `flagged` and leaves public view until a moderator acts. | `COMMENTS_FLAG_THRESHOLD` (3) |
| Moderation | `comment.moderate` holders approve, reject, or mark spam, singly or in bulk, with an append-only log; `comment.delete` hard-deletes (scrubbing parents so replies survive). Authors can be notified of the outcome. | RBAC |

Privacy: client IPs are only stored as SHA-256 hashes (`ip_hash`), and user agents are
truncated. Hard delete clears both.

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
