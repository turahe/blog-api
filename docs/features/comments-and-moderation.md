# Comments & Moderation

## Scope

Authenticated and anonymous guests may post, edit, delete (own), and reply to comments on published posts.
Administrators and content moderators review flagged or pending comments and can approve, reject,
mark as spam, soft-delete, or hard-delete any comment. All comment submissions pass through
anti-spam layers, content sanitization, and format validation. Feature also includes per-comment
upvotes, reader flagging, threaded display up to depth 5, CSRF protection on mutating actions,
and rate limits to combat abuse.

## Capability Matrix

| Action | Guest | Authenticated user | Moderator | Admin |
|---|---|---|---|---|
| Read approved thread | ✅ | ✅ | ✅ | ✅ |
| Post comment | Configurable | ✅ | ✅ | ✅ |
| Reply to comment | Configurable | ✅ | ✅ | ✅ |
| Edit own (grace window) | ❌ | ✅ | ✅ | ✅ |
| Soft-delete own | ❌ | ✅ | ✅ | ✅ |
| Upvote / cancel upvote | Limited | ✅ | ✅ | ✅ |
| Flag for moderation | Limited | ✅ | ✅ | ✅ |
| View pending / spam queue | ❌ | ❌ | ✅ | ✅ |
| Approve / reject / spam / restore | ❌ | ❌ | ✅ | ✅ |
| Bulk moderate up to 500 ids | ❌ | ❌ | ✅ | ✅ |
| Hard-delete with audit | ❌ | ❌ | ✅ | ✅ |
| View comment stats dashboard | ❌ | ❌ | ✅ | ✅ |
| Configure moderation policy | ❌ | ❌ | Settings manager | ✅ |

## Rules

- **Threading and depth**: `parent_id` points to the parent comment; `depth` is capped at `5` (enforced in API + DB trigger). Thread roots have `parent_id = NULL` and `depth = 0`.
- **Guest submissions**: Must include valid `author_name` and `author_email` if anonymous commenting is enabled via site setting `comments.guest.enabled`. Guest identity can never override a JWT-authenticated identity — if a bearer token is present, `author_id` wins.
- **Anti-spam pipeline** (runs in order; stop on first spam decision):
  1. Honeypot field (`CommentCreateRequest.honeypot`) — non-empty → mark spam silently.
  2. Captcha (`turnstile_response`) if `comments.spam.challenge` setting is enabled.
  3. Rate limiting per IP hash plus identity: default `6/minute` create and `30/minute` read.
  4. Content checks: max length 10,000 chars; disallowed patterns; link count cap.
  5. External engine (Akismet, Mollom, internal model) → `spam_score`, `spam_verdict`.
- **Sanitization and storage**: Raw markdown/text stored in `content`. `content_html` is generated
  server-side via an allow-list markdown processor (bold, italic, links, code, blockquote, lists)
  and a strict DOMPurify-style XSS sanitizer. HTML is never accepted from clients.
  Implemented with goldmark and bluemonday in `internal/adapters/outbound/markdown`; see
  [api.md](../backend/api.md#comment-content) for the exact allow-list.
- **Edit grace window**: Authors can edit their own comment for `N` minutes after creation
  (default 15; site setting `comments.edit_grace_minutes`). After the window, any edit
  requires moderation permissions. `edited_at` and `edited_reason` are recorded when an
  admin edits.
- **Soft-delete default**: `PATCH /api/v1/comments/{id}` and own-delete transition status to
  `deleted` and set `deleted_at` / `deleted_by`. Public readers see a "deleted" placeholder.
  Hard-delete is a separate moderator/admin action that physically removes the row or
  scrubs content after retention (retention: `comments.retention_deleted_days`).
- **Flags and auto-escalation**: A comment with `flag_count >= site settings threshold` is
  automatically status=`flagged` and added to the moderation queue. Duplicate flags from the
  same identity (user id or SHA-256(IP+UA) for guests) collapse to one.
- **Moderation audit**: Every `approve/reject/spam/restore/unspam/soft_delete/hard_delete`
  action writes a `comment_moderation_log` row with before/after snapshots, `moderator_user_id`,
  `reason`, and `notify_author` flag.
- **Notifications**: If `notify_author=true` on a moderation action, the comment author
  (or guest email) receives a transactional email with the action + reason and a link back
  to the post.
- **Post-level policy controls**: Each post may override site policy via a Post option —
  comments disabled, read-only, authenticated-only, or full permissions. Implemented as
  `posts.comment_policy` (`open`, `authenticated`, `read_only`, `disabled`; disabled also hides
  existing comments); see [api.md](../backend/api.md#comment-policy).

## Key Entities

- `comments` — core comment tree (PK `id`, FKs `post_id`, `parent_id`, `author_id`).
- `comment_flags` — per-flag reports (`comment_id`, `reporter_user_id`, `reason_code`).
- `comment_upvotes` — per-comment upvote ledger with unique on `comment_id + voter_user_id` or hash.
- `comment_moderation_log` — append-only audit trail of moderator actions.
- `posts` — `comments_count` counter cache updated by DB trigger on comment mutations (optional, exposed in meta).

## Key Permissions

- `comments.read` — default public for approved; queue access requires moderation permission.
- `comments.create` — default allow (subject to guest switch)
- `comments.edit.own` — default allow with grace window
- `comments.delete.own` — default allow
- `comments.flag` — default allow (rate limited)
- `comments.upvote` — default allow for auth; limited anonymous by id hash
- `comments.moderate.queue` — view pending/flagged queues
- `comments.moderate.approve`
- `comments.moderate.reject`
- `comments.moderate.spam`
- `comments.moderate.restore`
- `comments.moderate.bulk`
- `comments.moderate.hard_delete`
- `comments.stats.read` — dashboard widgets and per-post totals

## Key API Endpoints

Full request and response contracts are in [swagger.json](../swagger.json). Public and
self-service operations and the admin moderation operations are implemented. Admin routes are
guarded by the coarser `comment.moderate` and `comment.delete` (hard delete) permissions rather
than the per-action keys above, and the `spam_score` / `flagged_reason` list filters wait on a
spam engine.
Delivery status is tracked in [phase-3-collaboration-moderation.md](../tasks/phase-3-collaboration-moderation.md).

- Public / self
  - `GET /api/v1/posts/{id}/comments` list threaded tree by post
  - `POST /api/v1/posts/{id}/comments` create comment or reply
  - `GET /api/v1/comments/{id}` single + children
  - `PATCH /api/v1/comments/{id}` edit own in grace window (or any by mod)
  - `DELETE /api/v1/comments/{id}` soft-delete own
  - `POST /api/v1/comments/{id}/flag` flag for moderation
  - `POST /api/v1/comments/{id}/upvote` toggle upvote
  - `GET /api/v1/me/comments` current user comments
- Admin / moderation
  - `GET /api/v1/admin/comments` queue with filters (status, spam_score, flagged_reason, post_id)
  - `GET /api/v1/admin/comments/{id}` full view (includes ip_hash, spam assessment, before/after snapshots via audit log)
  - `POST /api/v1/admin/comments/{id}/moderate` single action
  - `POST /api/v1/admin/comments/bulk-moderate` transactional bulk up to 500
  - `DELETE /api/v1/admin/comments/{id}` hard delete
  - `GET /api/v1/admin/comments/stats` moderation health + per-post counts

## Browser / UX Requirements

- **Responsive**: Thread view collapses replies on narrow widths (< 480 px) with "Show N replies" tap target; no horizontal scroll.
- **Cross-browser**: Tested on latest Chrome/Firefox/Safari/Edge (Windows, macOS, iOS Safari 17+, Android Chrome). No ES syntax unsupported by Safari 16.
- **Progressive enhancement**: Form works without JS via server-rendered template; JS adds live-threading, inline validation, previews (markdown client-side preview with safe DOMPurify output only), and optimistic UI upvotes (reverts on error).
- **Accessibility**: `role="group"` per comment, `aria-level` per depth, keyboard navigation for reply/edit actions, visible focus rings, captcha support for screen readers, error messages associated via `aria-describedby`, reply threads ordered semantically.
- **SEO**: Approved comment content is rendered server-side in post HTML for crawlers. No JavaScript-exclusive content for public comment reads.

## Lifecycle & Compliance

- Retention of `ip_hash` and `user_agent`: default 12 months for pending/flagged rows; 30 days after `approved` unless country law requires longer.
- GDPR: Authenticated user may request export + deletion of their `comments`/`comment_flags`/`comment_upvotes` via the privacy request flow (`/api/v1/me/privacy`). Moderation decisions and logs are retained per legal basis.
- CSAM / illegal content: configured rule-sets auto-escalate to `flagged` + quarantine + admin notification.

## Comment Notifications (Trigger Conditions, Recipients, Channels, Templates)

Each comment lifecycle action that produces a user-facing notification writes a single `notifications` DB row (REST `Notification` shape, see [Notifications.yaml](../../components/schemas/Notifications.yaml)), fans out to the user's SSE stream via channel `notifications.user.{user_id}.stream` (see [events.md](../backend/events.md#L132-L155) and [realtime-notifications-sse.md](./realtime-notifications-sse.md)), and — depending on user preferences — dispatches an email or push notification through the notification service worker.

Trigger timing is always **transactional outbox**: the domain event (`blog.comment.*`) is appended to the `outbox` table in the same DB transaction that commits the comment/flag/vote row, then Watermill (see [asyncapi.yaml](../../contracts/asyncapi.yaml) channel definitions) asynchronously consumes the outbox and fans it out. In-app (SSE + in-app inbox) notifications are delivered "at-least-once"; email and push are "at-least-once with dedup".

### Notifications Channel Routing

All comment notifications flow through four potential delivery channels. Per-channel enablement is a function of (a) site defaults, (b) user NotificationPreferences, and (c) per-notification-type overrides including mandatory channels (e.g. moderation-decision emails **cannot** be fully suppressed — only frequency-capped, see §Access Control).

| Channel | Envelope | Typical end-to-end latency | Retry / durability |
|---|---|---|---|
| `in_app` | Row in `notifications` table + surfaced via `GET /api/v1/me/notifications` | < 1 s after outbox flush | Persistent until user dismisses or TTL (180 days) |
| `sse` | `event: notification.created` on user's EventSource connection | < 500 ms (SLO p95 < 150 ms for SSE fan-out alone — see SSE doc) | Delivered at-least-once; dedup by `notification_id` client-side |
| `email` | Transactional HTML email via the configured SMTP/mail provider | 5 s – 5 min depending on provider queue + MX | Exponential backoff 6 attempts (see §Exception Handling) |
| `push` | WebPush VAPID / FCM / APNs via provider adapter | 1 – 10 s | 4 attempts, final failure logged; never retried beyond 1 h |

### Notification Catalogue — All Comment Scenarios

The table below enumerates every user-facing notification produced by the Comments & Moderation feature. `type` values match `Notification.type` (see [Notifications.yaml](../../components/schemas/Notifications.yaml)) and are the stable identifiers used in preferences + unsubscribe APIs.

| # | Trigger condition | Notification `type` | Primary recipient(s) | Channels enabled by default | Trigger timing |
|---|---|---|---|---|---|
| N1 | A comment (root or reply) is successfully `POST`ed and either: (a) auto-approved, or (b) held for moderation and the site setting `notifications.comment.pending_alert_author=true`. | `comment.created` | Comment **author** (self); additionally for **replies** (parent_id≠NULL) also the **parent comment author** via `comment.reply`. | N1→author self: `in_app,sse` (email opt-in). N1→reply-parent recipient see N2. | Immediately after the `blog.comment.created` outbox event is committed (same transaction as `comments.status IN ('approved','pending')`). |
| N2 | A reply (`parent_id IS NOT NULL`) is created and the reply author ≠ parent comment author. | `comment.reply` | **Parent comment author**. | `in_app,sse,email` (push opt-in). Self-replies are **never** notified (dedup on `reply.author_id == parent.author_id`). | Same transaction as N1; sent iff `parent_id IS NOT NULL AND reply.author_id != parent.author_id`. |
| N3 | A comment receives a new upvote (toggle: off → on only; cancelling an upvote does NOT notify). | `comment.upvoted` | **Comment author** (if authenticated; guest-authored comments never notify by design — no identity to deliver to). | `in_app,sse` (email opt-in, push opt-in). Coalesced per §Exception Handling (N3-rollup). | After `comment_upvotes` insert commits; outbox event raised only for `action=added`, not `action=removed`. |
| N4 | A comment receives a reader flag (first flag from a distinct reporter identity per comment). | `comment.flagged` | All users holding `comments.moderate.queue` permission (moderators + admins). Site setting `notifications.comment.flagged.threshold` controls how many distinct flags must accumulate before notification fires — default is **1**. Setting `notifications.comment.flagged.cooldown_seconds` (default 300 s) prevents duplicate mod-alert storms for the same hot comment. | `in_app,sse` (email opt-in). | After first insert into `comment_flags` per comment per cooldown window; batched if threshold > 1. |
| N5 | Moderator action: a pending / flagged comment is **approved** (status `pending/flagged` → `approved`) AND the moderation-log `notify_author=true`. | `comment.approved` | **Comment author** (authenticated user or guest email address). | `in_app,sse,email`. Guest-authored comments fall back to `email` only (no `in_app`/`sse` identity). | After `comment_moderation_log` + status update commit (outbox event `blog.comment.approved`, see [asyncapi.yaml](../../contracts/asyncapi.yaml#L77-L83)). |
| N6 | Moderator action: a pending / flagged comment is **rejected** (soft-deleted, marked spam, or hard-deleted with scrub) AND moderation-log `notify_author=true`. | `comment.rejected` | **Comment author** (authenticated or guest email). | `in_app,sse,email`. Mandatory minimum delivery: at least one of `in_app | email` MUST succeed per regulation (see Exception Handling). | Same transaction as N5; condition on `action IN ('reject','spam','soft_delete','hard_delete')`. |
| N7 | Moderator action: a previously-approved comment is **edited by a moderator** (outside the grace window and with `edited_reason` set) AND `notify_author=true`. | `comment.moderated_edited` | **Comment author**. | `in_app,sse,email`. | After `comment_moderation_log` insert with `action='edit'`. |
| N8 | **Escalation**: automated anti-spam or CSAM classifier returns verdict `quarantine` + `severity>=high`. | `comment.quarantined` | Admins + assigned moderation-oncall user (rotated via site setting `notifications.moderation.oncall_user_id`). | `in_app,sse,email,push`. Mandatory push for oncall. | Immediately after the content-classifier service writes the verdict; before any moderator UI sees it. |

### Content Templates (Title / Preview / Email Subject & Body)

All notifications produce a stable `title` string, optional `preview` (first 200 chars of the triggering comment's sanitized content for in-app surfaces), and — when email is enabled — a subject + HTML body. Template variables use `${snake_case}` substitution and are always HTML-escaped before render.

#### Common variables available in every template

| Variable | Source | Example |
|---|---|---|
| `${comment_id}` | `comments.id` (uuid) | `0191c65b-7d2a-7…` |
| `${post_title}` | `posts.title` (truncated to 80 chars) | `"Announcing our Q3 roadmap"` |
| `${post_url}` | Canonical post URL | `https://blog.example.com/posts/${slug}` |
| `${comment_author_name}` | `users.display_name` or guest `author_name` | `"Ada Lovelace"` |
| `${comment_preview}` | `LEFT(content_plaintext, 200) || ''` — no raw HTML | `"Great article, I especially appreciated …"` |
| `${comment_depth_hint}` | `depth=0 → ''; depth>0 → ' (reply to "' + parent_snippet + '")'` | `" (reply to \"Thanks!\")"` |
| `${moderator_reason}` | `comment_moderation_log.reason` or empty string | `"Contains an unapproved external link."` |
| `${preferences_url}` | Deep-link to notification preferences | `https://blog.example.com/me/settings/notifications` |
| `${unsubscribe_token}` | HMAC-SHA256-signed per-recipient per-type token, valid 30 days | `v1.uq3xz…` |

#### Per-type templates (excerpt — canonical versions live in `internal/notification/templates/*`)

| Type | In-app `title` | In-app `preview` | Email `Subject:` |
|---|---|---|---|
| `comment.created` (self-confirm) | `"Your comment on ${post_title} is ${status_hint}`" — status_hint ∈ { `"published"`, `"pending review"` } | `${comment_preview}` | `[${site_name}] Your comment on "${post_title}"` |
| `comment.reply` | `"${reply_author_name} replied to your comment on ${post_title}"` | `${comment_preview}` | `[${site_name}] Reply: "${reply_author_name}" replied to your comment` |
| `comment.upvoted` (coalesced) | Plural-aware: `"Your comment on ${post_title} received ${count} new upvote(s)"` | `${comment_preview}` | (email opt-in only) `[${site_name}] ${count} new upvote(s) on your comment` |
| `comment.flagged` | Moderator-only: `"Comment flagged on ${post_title} — see moderation queue"` | `${comment_preview}` — stripped of any PII by the mod policy | `[MOD-ALERT] New flags (${flag_count}) on comment id ${comment_id}` |
| `comment.approved` | `"Your comment on ${post_title} has been approved"` | `${comment_preview}` | `[${site_name}] Your comment was approved — join the discussion` |
| `comment.rejected` | `"Your comment on ${post_title} was ${action_label}"` — action_label ∈ { `"removed"`, `"marked as spam"`, `"held"` } | `${moderator_reason}` if set else `"Contact the moderation team for more information."` | `[${site_name}] A moderator reviewed your comment` |
| `comment.moderated_edited` | `"A moderator edited your comment on ${post_title}"` | `"Reason: ${moderator_reason}"` | `[${site_name}] Your comment was edited by a moderator` |
| `comment.quarantined` | `"[URGENT] Quarantined comment — review required"` | `"Severity: ${severity}; Verdict: ${verdict}; comment_id=${comment_id}"` | `[MOD-ONCALL] Quarantined comment ${comment_id} — ${severity}` |

Email bodies follow the site transactional template. All transactional emails include a one-click unsubscribe link at the footer using `${unsubscribe_token}` — see §Access Control for token semantics.

## Notification Access Control, Preferences, and Unsubscribe

### Granularity of User Preferences

Every authenticated user has a single `notification_preferences` JSON column (or dedicated table, implementation detail) keyed by `Notification.type` with three per-channel booleans + a digest cadence. Guest commenters (no user account) receive per-email unsubscribe tokens but cannot persist cross-notification preferences across browser sessions.

Preference schema (stored shape; enforced by `POST /api/v1/me/notification-preferences` validation):

```
NotificationPreference = {
  type:        string   // one of Notification.type — e.g. "comment.reply"
  in_app:      bool     // default true for all types except comment.created (self)
  sse:         bool     // default true; only meaningful if in_app=true (SSE mirrors inbox)
  email:       bool     // per-type default per catalogue above
  push:        bool     // default false (opt-in globally first)
  digest:      enum     // "immediate" | "hourly" | "daily" | "weekly"
  // Special: "comment.upvoted" and "comment.flagged" support an
  // additional rollup_threshold so a burst produces exactly 1 digest.
  rollup_threshold?: int  // default=5 for upvoted, default=1 for flagged
}
```

Global defaults for new user accounts (applied on signup):

| Type | in_app | sse | email | push | digest |
|---|---|---|---|---|---|
| `comment.created` (self) | ✅ | ✅ | ❌ | ❌ | immediate |
| `comment.reply` | ✅ | ✅ | ✅ | ❌ | immediate |
| `comment.upvoted` | ✅ | ✅ | ❌ | ❌ | hourly + rollup≥5 |
| `comment.flagged` | **mod-only**, defaults inherit from moderator role preset | | | | immediate |
| `comment.approved` | ✅ | ✅ | ✅ | ❌ | immediate |
| `comment.rejected` | ✅ | ✅ | ✅ | ❌ | immediate |
| `comment.moderated_edited` | ✅ | ✅ | ✅ | ❌ | immediate |
| `comment.quarantined` | **admin/oncall only**, push is mandatory | | | | immediate |

### Mandatory Channels (Non-unsubscribable)

The following types **cannot be fully disabled** by the user (legal basis: legitimate interest for account security, moderation transparency, and on-call incident response). The user may switch the *channel* but not suppress delivery entirely:

- `comment.rejected` — at least one of { in_app, email } must remain enabled. If the user disables both, the system silently keeps `email=true` and records a shadow preference (visible in audit).
- `comment.approved` — at least one of { in_app, email } when the original submission was held in pending status (so authors know when they're live).
- `comment.quarantined` — admin/oncall: `push` is forced on if `push_vapid_sub || push_fcm_token` exists; disabling only toggles email frequency.

### Preference & Unsubscribe API Endpoints

Full contracts live in [paths/notifications.yaml](../../paths/notifications.yaml) and the self-service paths (create if missing at `/api/v1/me/notification-preferences`):

| Method + Path | Purpose | Auth |
|---|---|---|
| `GET /api/v1/me/notification-preferences` | Read typed array of `NotificationPreference[]` for the current user (includes all comment types) | Bearer / session cookie |
| `PUT /api/v1/me/notification-preferences` | Upsert full array; server validates mandatory-channel constraints and returns `422` with a `constraint_violations[]` array if user tries to disable a mandatory channel entirely | Bearer / session |
| `POST /api/v1/me/notifications/read-all` | Mark entire inbox as read (fan-out `notification.read` to SSE) | Bearer / session |
| `POST /api/v1/me/notifications/{id}/dismiss` | Soft-dismiss a single notification (client UI state) | Bearer / session |
| `GET  /unsubscribe?token=${unsubscribe_token}` | **Email one-click unsubscribe**: no auth required; validates HMAC signature, then (a) shows confirmation page, (b) disables `email` channel for the type encoded in the token OR (c) honors `scope=all` to globally email-opt-out. | Public, token-only |
| `POST /unsubscribe` | Form submission from the GET page; same token semantics | Public |

#### Unsubscribe token format

```
token = "v1." + base64url( JSON.stringify({
  sub: user_id,           // OR guest_email: string for non-auth recipients
  type: "comment.reply",  // exact Notification.type, or "*" for global-email opt-out
  iat: unix_seconds,
  exp: iat + 2_592_000,   // 30 days
  nonce: 16 random bytes
}) ) + "." + base64url( HMAC_SHA256(NOTIFICATION_UNSUBSCRIBE_KEY, payload) )
```

Tokens are single-use: after first application, the `nonce` is stored in a `unsubscribe_tokens_consumed` set (Redis, TTL = exp) to prevent replay.

### GDPR PII Erasure in Notifications

When an authenticated user submits a GDPR erasure request (`POST /api/v1/me/privacy` with `action=erase`), the following scrub rules apply to comment-related notification rows and outbound deliveries **non-destructively** (row integrity preserved):

- `notifications` table rows where `recipient_user_id = erased_user.id`:
  - `title` / `preview` / `email_subject` → replaced with `[GDPR-ERASED ${notification_id}]`
  - Any `payload_json` columns with PII → keys redacted recursively
  - Row PK, `type`, `created_at`, and `read` status are retained (needed for dedup and audit integrity)
- Queued email/push jobs that have not yet been dispatched: cancelled and scrubbed.
- Already-sent email archive entries: subject scrubbed per site retention policy (legal hold exempt).

A user may request the list of persisted notification identifiers attributed to them via `GET /api/v1/me/privacy/export?dataset=notifications` before erasure.

## Notification Exception Handling (Retry, Dedup, Rate & Noise Controls)

### Retry Policy for Email and Push

In-app and SSE delivery is synchronous fan-out; email and push go through an async worker pool (Watermill consumer group `comment-notification-dispatch`) with the following per-message retry schedule **on transient failures only**:

| Attempt | Backoff (wall clock from prior failure) | Class of "transient" |
|---|---|---|
| 1 | immediate (inline with outbox consumer) | TCP RST / TLS handshake timeout / DNS |
| 2 | + 15 s | 5xx from provider / rate-limit 429 with `Retry-After` (use Retry-After if greater) |
| 3 | + 60 s | |
| 4 | + 5 min | |
| 5 | + 30 min | |
| 6 | + 2 h | Final attempt |

A "permanent" failure (provider 4xx **excluding** 429, malformed push subscription endpoint, revoked VAPID subscription) moves the message directly to the dead-letter queue on the first attempt and increments a counter exposed in `/api/v1/admin/comments/stats`. For moderation-decision notifications (`comment.rejected`, `comment.approved` pending→approved, `comment.quarantined`) the worker additionally falls back to an alternate provider configured via `notifications.fallback_provider` if the primary returns permanent — guaranteeing at-least-one channel success.

Dead-letter queue:
- Topic: `dlq.notifications.dispatch` — retained 14 days, replayable via admin tooling.
- Alert: Any 5 consecutive failed dispatches for the same recipient → raises `blog.notification.recipient_degraded` admin SSE alert.

### Idempotency & Deduplication Rules

Duplicate deliveries are eliminated by a three-tier key set:

| Tier | Key shape | Scope | How it works |
|---|---|---|---|
| T1 — *Application idempotency* | `"cmt-notif:${outbox_event_id}:${type}:${recipient_id}"` | Redis SETNX, TTL 24 h | The notification service worker derives this from the outbox message ID. If a duplicate Watermill redelivery occurs, the second insert is a no-op. **This is the primary dedup.** |
| T2 — *Client SSE dedup* | `data.notification_id` (UUIDv7 on the notifications row) | Browser / client | Per SSE doc at-least-once guarantees, clients dedup by storing the last 1000 seen `notification_id`s in a circular buffer. |
| T3 — *Email/push external idempotency* | `"X-Msg-ID: ${notification_id}@${site_domain}"` header (email); `"collapse_key=${notification_id}"` (FCM/APNs) | Provider-side | MTAs respect RFC 2822 `Message-ID` / `References` so a re-send does not create duplicate Inbox threads when the original was eventually delivered. |

### Rate Limiting & Noise Reduction

The following layered rate limits apply **above and beyond** the generic comment-create rate limits in §Rules. Goal: never spam a single recipient with more than N notifications per rolling window, and collapse similar events into a single rollup summary.

#### (R1) Per-recipient per-type hard cap

Enforced at notification-insert time by the service (before outbox):

| Type | Hard cap (per recipient) | Behaviour on exceed |
|---|---|---|
| `comment.upvoted` | 1 notification / 600 s (10 min) + rollup_threshold=5 | All discrete upvote events in the window are counted in a Redis counter; when the window closes or count ≥ threshold, a single coalesced `comment.upvoted` notification is produced with `${count}` aggregate. The window resets. |
| `comment.flagged` (mod alerts) | 1 notification / comment / 300 s | New flags above the first do not re-alert; a summary email runs hourly if any mod queue has new items. |
| `comment.reply` | 30 notifications / recipient / 1 h | Above cap → silent discard of new `comment.reply` events **for that specific thread only**; a single digest "N replies waiting" email is sent at 1 h boundary. |
| `comment.created` (self-confirm) | 10 / 1 h per author | Above cap → in-app only, email suppressed (too many pending). |
| `comment.quarantined` (oncall) | 3 / 1 min per oncall user | Above cap → remaining items batched, 1 push summary every 5 min. |

#### (R2) Cross-type per-recipient soft cap

Per-15-minutes window: if **total** notifications delivered to one user exceeds 60 across all comment types, **email and push are automatically downgraded to `digest=hourly`** for the remainder of the calendar hour. In-app and SSE continue normally (the user is online and actively engaging). After the hour, the user's stored digest cadence is restored.

#### (R3) SSE per-connection fan-out buffer

See §1 of [realtime-notifications-sse.md](./realtime-notifications-sse.md#L21-L41): per-connection fan-out buffer size 512. If the comment burst creates a backlog > 512 pending SSE frames for one connection, the oldest buffered frame is dropped, a single `event: error` with `{code:"fanout.buffer_full", dropped_count:N}` is sent, and the client should call `GET /api/v1/me/notifications?since=<last_seen_created_at>` to catch up via REST. Per-user concurrent SSE connections are capped at 3; the 4th gets 429.

#### (R4) Global platform noise cap

Per-minute comment-notification dispatches across **all** users:
- In-app/SSE: effectively unlimited (Redis pub/sub fan-out, shared-nothing nodes)
- Email / push providers: rate limit set to `90% of provider documented QPS` by the adapter; excess messages are queued with a jitter (0–10 s) and retried per the retry schedule above. Provider 429 responses always reset the adapter's local token bucket to zero for the duration of `Retry-After`.

### Moderator Action `notify_author=false` Escape

When a moderator explicitly sets `notify_author=false` on a `POST /admin/comments/{id}/moderate` request (see Key API Endpoints §moderate), the outbox domain event is still published for audit and metrics, but the notification dispatch worker inserts the combination `(comment_id, moderator_action, recipient_id)` into the skip list Redis key `cmt-notif:skipped:${hour_bucket}` for 1 h. The notification insert is therefore skipped without raising an error, and the counter `moderation_notify_author_skipped_total` is incremented for observability.

## Monitoring / Logging

- Structured logs on every moderation action (`comment.moderate.*`) with actor, action, comment_id, and counts of before/after for bulk.
- Metrics counters: `comments_created_total`, `comments_spam_total`, `comments_flagged_total`, `comments_edited_grace_hit_ratio`, `moderation_queue_depth`.
- SLO: 99th percentile comment create < 400 ms; 99th moderation list query < 500 ms.
