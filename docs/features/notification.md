# Notification

## Scope

- email-based notifications
- **SSE real-time stream for authenticated users**
- event-driven notification triggers
- future extensibility for additional channels

## Initial Use Cases

- account verification
- password reset
- recovery updates
- moderation alerts
- publication-related notifications
- real-time alerts to logged-in users via browser stream

## Channels

- `email`: reliable delivery over SMTP. This is the only channel that may contain a secret token.
- `web`: in-app inbox copy (title and body) for the signed-in user. No secret tokens.
- `sse`: near-real-time browser push over `GET /api/v1/me/notifications/stream`. Event name `notification.created`, with a title and a short preview. No secret tokens.

## Templates

Templates are stored in the `notification_templates` table (migration `00013`), one row per `(type, channel)`. `app seed` inserts the built-in catalogue from `internal/core/notification/template` and never overwrites rows that already exist, so edits survive a re-seed. When a row is missing or the database is unavailable, the built-in copy is used.

Rows are validated before they are saved: the channel must be `email`, `web`, or `sse`; email needs a subject, web and SSE need a title, and SSE needs an event name; and only the fields listed below may be used. `{{.Token}}` is rejected everywhere except the email body, and at render time the token is blanked for every other field, so a stored template cannot expose it on web or SSE.

Values are inserted as plain text. Newlines in values are collapsed so they cannot break headers.

| Variable | Used for |
| --- | --- |
| `{{.Name}}` | Greeting (full name, otherwise username) |
| `{{.Email}}` | Current account address |
| `{{.NewEmail}}` | Address being confirmed |
| `{{.Token}}` | Email body only |
| `{{.ExpiresAt}}` | UTC expiry, RFC3339 |
| `{{.PublicURL}}` | API origin |
| `{{.PostTitle}}` / `{{.PostURL}}` | Publication or moderation target |
| `{{.Reason}}` | Moderation reason |
| `{{.ActorName}}` | Person who triggered the notice (moderation alert, reply author) |
| `{{.Excerpt}}` | Short one-line quote of a reply (140 characters at most) |
| `{{.Outcome}}` | Moderation result: `approved`, `rejected`, `marked as spam`, `removed`, `held for review` |

| Type | Email subject | Web title | SSE preview |
| --- | --- | --- | --- |
| `account.verify` | Verify your email address | Verify your email | Check your email to confirm this account. |
| `account.exists` | You already have an account | — | — |
| `password.reset` | Reset your password | Reset your password | Check your email for the reset token. |
| `password.changed` | Your password was changed | Your password was changed | If this was not you, reset your password. |
| `email.change.confirm` | Confirm your new email address | Confirm your new email | Check the new address for the confirmation token. |
| `email.change.notice` | Email change requested | Email change requested | Confirm it from the new address, or change your password if this was not you. |
| `email.changed` | Your email address was changed | Your email address was changed | All sessions were signed out. |
| `moderation.alert` | Moderation alert: `{{.PostTitle}}` | Moderation alert | `{{.PostTitle}}` needs review. |
| `publication.published` | Published: `{{.PostTitle}}` | Your post is published | `{{.PostTitle}}` |
| `publication.republished` | — | A post you commented on is back | `{{.PostTitle}}` |
| `comment.reply` | — | `{{.ActorName}}` replied to your comment | `{{.Excerpt}}` |
| `comment.moderated` | — | Your comment was `{{.Outcome}}` | `{{.PostTitle}}` |

Types with no email subject are in-app only: they have web and SSE templates and are never mailed.
`account.exists` is the reverse: email only, sent when someone registers with an address that
already has an account. `account.verify` and `account.exists` go to the address, not to a user.

SSE frames use `event: notification.created`. The `data` object carries `type`, `title`, and `preview`. Web uses the same `type` and `title`, plus the longer `body`. Email uses `subject` and the plain-text `body`, which is the only place a reset or confirmation token appears.

## In-app inbox

Web notices are stored in the `notifications` table (migration `00019`), one row per recipient.
Each row keeps the rendered web `title` and `body`, the SSE `preview`, a `payload` with links
(`post_id`, `comment_id`, `parent_id`, `status`, `url`), the actor, and `read_at`. Copy is
rendered when the row is written, so later template edits do not rewrite old notices.

Triggers (implemented by `notification/service.Inbox`, called from the comment and post services):

| Event | Recipient | Type | Not sent when |
| --- | --- | --- | --- |
| A reply becomes visible (created approved, or approved from pending, spam, or rejected) | Registered author of the parent comment | `comment.reply` | The parent author is a guest or wrote the reply |
| A moderator decides with `notify_author: true` | Registered comment author | `comment.moderated` | The author is a guest or is the moderator |
| A post is published by someone other than its author (or by no user) | Post author | `publication.published` | The author published it |
| A post is published again | Registered users with an approved comment on it | `publication.republished` | The user is the author or the publisher |

Delivery never fails the comment or post request. With `MESSAGE_BROKER` set, the request only
records a `notification.requested` command and the `notification-dispatch` consumer in
`app worker` stores the notices, retrying failures (see
[events.md](../backend/events.md#notification-dispatch)). Without a broker the notices are
stored in the request and a failed insert is only logged. Every notice has a per-user dedupe key (for example `comment.reply:<reply id>`), so
re-approving a reply or retrying a publish does not notify twice. Bulk moderation sends reply
notices but never `comment.moderated`, because it has no `notify_author` flag.

`GET /api/v1/me/notifications` lists the caller's notices newest first (`?unread=true`,
`page`, `per_page` up to 100) and returns the unread total in `X-Unread-Count`.
`POST /api/v1/me/notifications/{id}/read` marks one read; repeating it keeps the first
`read_at`, and another user's id is `404`.

## SSE Rules

- the SSE endpoint must require a valid authenticated session or token
- fan-out must be user-scoped; clients must never see another user’s events
- reconnecting clients should replay any missed in-app notifications from the REST history endpoint or through durable pub/sub offsets when available
- keep-alive pings should be sent at a configured interval to prevent proxy timeouts
- SSE should be treated as an inbound adapter that consumes internal notification events via pub/sub (Watermill or Redis pub/sub)

## Rules

- trigger notifications from events or jobs, not directly from controllers
- keep providers behind outbound ports
- template content clearly and safely
- make notification handlers idempotent where practical

