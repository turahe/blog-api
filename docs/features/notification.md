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
| `{{.ActorName}}` | Person who triggered a moderation alert |

| Type | Email subject | Web title | SSE preview |
| --- | --- | --- | --- |
| `account.verify` | Verify your email address | Verify your email | Check your email to confirm this account. |
| `password.reset` | Reset your password | Reset your password | Check your email for the reset token. |
| `password.changed` | Your password was changed | Your password was changed | If this was not you, reset your password. |
| `email.change.confirm` | Confirm your new email address | Confirm your new email | Check the new address for the confirmation token. |
| `email.change.notice` | Email change requested | Email change requested | Confirm it from the new address, or change your password if this was not you. |
| `email.changed` | Your email address was changed | Your email address was changed | All sessions were signed out. |
| `moderation.alert` | Moderation alert: `{{.PostTitle}}` | Moderation alert | `{{.PostTitle}}` needs review. |
| `publication.published` | Published: `{{.PostTitle}}` | Your post is published | `{{.PostTitle}}` |

SSE frames use `event: notification.created`. The `data` object carries `type`, `title`, and `preview`. Web uses the same `type` and `title`, plus the longer `body`. Email uses `subject` and the plain-text `body`, which is the only place a reset or confirmation token appears.

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

