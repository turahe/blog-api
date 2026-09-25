# Newsletter Backend

Visitors and signed-in users subscribe to admin-defined lists with double opt-in, manage their
preferences and unsubscribe through opaque single-purpose tokens, and receive issues that
editors write in Markdown. Issues are sent by `app worker` through the SMTP mailer or a signed
`custom_http` gateway; a signed webhook feeds bounces and complaints back.

Product spec: [newsletter-subscriptions.md](../features/newsletter-subscriptions.md).

## Placement

| Layer | Package | Role |
| --- | --- | --- |
| Domain | `internal/core/newsletter/domain` | subscribers, memberships, lists, tokens, consent events, issues and their transitions, errors |
| Ports | `internal/core/newsletter/ports` | `Repository`, `Mailer` (confirm and welcome), `Sender`, `ContactSync`, `WebhookVerifier`, `Captcha`, `Links`, `Markdown`, `Accounts` |
| Service | `internal/core/newsletter/service` | subscribe, confirm, preferences, unsubscribe, `/me`, admin, `ReleaseDue`, `Dispatch`, `SyncSubscriber`, `ProviderWebhook`, `PruneTokens` |
| Persistence | `persistence.NewsletterRepository`, `NewsletterIssueRepository` | migration 00030 |
| Sending | `newsletterprovider.SMTP`, `newsletterprovider.HTTP` | one MIME message per recipient, or a signed JSON POST per recipient |
| Transactional mail | `newslettermail` | `newsletter.confirm` and `newsletter.welcome` through the notification templates and mail queue |
| Consumers | `consumer.NewsletterDispatch`, `consumer.NewsletterSync` | worker handlers for the two newsletter events |
| Wiring | `bootstrap.NewNewsletterService` | picks the sender from `NEWSLETTER_PROVIDER` |

## Lists and provider config

Admins define lists (`slug`, `name`, `description`, `isDefault`) through
`PUT /api/v1/admin/newsletter/provider-config`. A subscribe request without lists joins the
default lists. Removing a list from the config archives it; issues keep their audience.

The same request stores the non-secret sending fields in the singleton
`newsletter_provider_config` row: `fromName`, `fromEmail`, `replyTo`, `postalAddress`,
`confirmTtlHours` (1–168, default 48), and `doubleOptinRequired`. The provider, its endpoint,
and its secret stay in env; `GET` returns them read-only as `provider` with the endpoint stripped
of credentials and query and the secret reduced to `secretConfigured`. `readyToSend` is false
until a postal address is set, and scheduling or queuing an issue answers `422
newsletter.not_configured` until then (CAN-SPAM).

## Subscriber lifecycle

Statuses: `pending_confirm`, `active`, `unsubscribed`, `bounced`, `complained`, `erased`. Each list
membership is `pending`, `active`, or `left`.

1. `POST /newsletter/subscribe` (JSON or form) validates the address, honeypot, and optional
   Turnstile challenge, marks the requested lists pending, and mails a confirmation link. It
   answers `202 {status: "pending_confirmation"}` for new, pending, active, and suppressed
   addresses alike, so the response never reveals whether an address is known. A filled honeypot
   is accepted and ignored. At most 3 confirmation emails go to one address per 30 minutes.
2. `POST /newsletter/confirm` consumes the token, activates the pending lists, records
   `opted_in_at`, and sends the welcome email. A used token answers `409 newsletter.token_used`,
   an expired one `410 newsletter.token_expired`, an unknown one `404 newsletter.token_invalid`.
3. `POST /newsletter/confirm/resend` issues a fresh link under the same limit and the same `202`.
4. `GET|PATCH /newsletter/preferences/{token}` shows and changes the format (`html` or
   `plaintext`) and lists; the GET masks the address. `unsubscribeAll: true` leaves every list.
5. `POST /newsletter/unsubscribe` takes the token in the body, form, or `?token=` query and
   accepts an optional `reasonCode` (`too_frequent`, `not_relevant`, `never_signed_up`, `other`)
   and `feedback`. It is the RFC 8058 one-click target: mail clients POST
   `List-Unsubscribe=One-Click` as a form to the URL in the `List-Unsubscribe` header.

Signed-in users use `/me/newsletter/subscriptions|subscribe|unsubscribe` with their account
email. A verified account email joins at once unless `doubleOptinRequired` is set; otherwise
it gets the same confirmation email. Account subscribers are linked by `user_id`.

Hard bounces and complaints suppress the address: later public subscribes are accepted but send
nothing. Only the owner can lift a suppression, by subscribing from their account or by
choosing lists through a preferences link from an earlier issue (recorded as `resubscribed`).
Soft bounces are only recorded.

## Tokens

Tokens are 32 random bytes, base64url encoded. Only their SHA-256 is stored in
`newsletter_tokens`, with a purpose (`confirm`, `unsubscribe`, `preferences`), an expiry, and a
`used_at`. A token works only for its purpose. Confirmation tokens live for `confirmTtlHours`
and are single use; a new one revokes the previous ones. Each issue mails every recipient fresh
unsubscribe and preferences tokens that live 37 days (30 days after the send plus 7 days of
grace). The `newsletter-tokens-prune` job deletes tokens 30 days after expiry. Request logs
record the route template, so tokens in paths are never logged.

## Issues

Editors create issues with `subject`, `preheader`, `bodyMarkdown`, and target `lists`. Status
moves:

| From | To |
| --- | --- |
| `draft` | `scheduled`, `queued`, `cancelled` |
| `scheduled` | `draft`, `scheduled` (new `sendAt`), `queued`, `cancelled` |
| `queued` | `cancelled` |
| `sending` | `queued` (resume after retries ran out), `cancelled` |

Content and lists change only while an issue is `draft` or `scheduled`. Moving to `scheduled` or
`queued` needs `newsletter.issues.send`. `GET /admin/newsletter/issues/{id}?preview=true` adds the
rendered HTML and plaintext with placeholder links.

The Markdown goes through the sanitizing newsletter renderer (an allowlist of text elements,
`https` and `mailto` links with `rel="noreferrer"`, https images), then into an email-safe table layout whose footer carries the unsubscribe and
preferences links and the postal address. The plaintext part is the Markdown source with raw
HTML stripped, plus the same footer. Every message carries `List-Unsubscribe` (the one-click API
URL) and `List-Unsubscribe-Post: List-Unsubscribe=One-Click`.

## Dispatch

Queuing an issue records `blog.newsletter.issue.send_requested`; the `newsletter-release` job
queues due scheduled issues every minute. The `newsletter-dispatch` consumer then:

1. moves the issue from `queued` to `sending`;
2. claims up to `NEWSLETTER_SEND_BATCH` active subscribers of the issue's lists by inserting
   `newsletter_deliveries` rows (one per issue and subscriber, so nobody gets an issue twice);
3. mails each claimed recipient in their format and records `sent` or `failed`;
4. repeats until no recipient is left, stopping early if the issue is cancelled;
5. marks the issue `sent` with `sentCount` and `failedCount`.

A claim older than 10 minutes can be retaken, so a crashed worker loses nothing. A failed
delivery is retried up to 3 times; a permanent failure (bad address, header injection, a 4xx
other than 408/429 from the gateway) is not. While retryable failures remain the consumer
returns an error and the broker redelivers with backoff. When the broker gives up, an editor
PATCHes the `sending` issue to `queued` to resume it.

With no sender configured the dispatch consumer is not registered: issues stay queued in the
broker and `sendingEnabled` in the provider config is false.

## Providers

`NEWSLETTER_PROVIDER=smtp` (default) sends through the transactional SMTP settings, one message
per recipient with a `multipart/alternative` body (or plain text for plaintext subscribers).

`NEWSLETTER_PROVIDER=custom_http` POSTs every delivery and every subscriber change as JSON to
`NEWSLETTER_HTTP_ENDPOINT` with these headers:

| Header | Value |
| --- | --- |
| `X-Newsletter-Event` | `newsletter.delivery` or `newsletter.contact` |
| `X-Newsletter-Timestamp` | Unix seconds |
| `X-Newsletter-Signature` | `v1=` + hex HMAC-SHA256 of `timestamp + "." + body` with `NEWSLETTER_HTTP_SECRET` |
| `Idempotency-Key` | the delivery id, or the subscriber id and change time |

Delivery bodies carry `delivery_id`, `to`, `subject`, `text`, `html`, `from_name`, `from_email`,
`replyTo`, and `headers`. Contact bodies carry the subscriber's current `id`, `email`, `name`,
`status`, `format`, `lists`, and `changed_at`; an erased contact carries only its id and status.
Syncing current state rather than the change makes retries and reordering harmless. The gateway
answers 2xx once it has accepted a message.

Mailchimp, Brevo, SES and other ESPs are not built in. Put a small gateway in front of them that
speaks this contract, or add a `Sender`/`ContactSync` adapter.

## Bounce and complaint webhook

`POST /api/v1/newsletter/webhooks/provider` is enabled when `NEWSLETTER_HTTP_SECRET` is set, with
either provider. It uses the same signature headers; the timestamp must be within 5 minutes.
The body is:

```json
{"events": [{"type": "hard_bounce", "email": "reader@example.com", "occurred_at": "2026-09-25T10:00:00Z"}]}
```

`type` is `hard_bounce`, `soft_bounce`, or `complaint`; 1–100 events per request, body at most
1 MB. The whole batch is validated before any event is applied. A bad signature answers `401
newsletter.signature_invalid`; without a secret it answers `422 newsletter.not_configured`.
Unknown addresses are ignored and responses never echo addresses.

## Admin

| Operation | Permission (fallback roles) |
| --- | --- |
| List, search, and get subscribers | `newsletter.subscribers.read` (admin, editor) |
| CSV export (`?format=csv`, up to 50000 rows) | `newsletter.subscribers.export` (admin, editor) |
| Delete subscriber | `newsletter.subscribers.erase` (admin) |
| List, get, create, and edit issues | `newsletter.issues.read` / `newsletter.issues.edit` (admin, editor) |
| Schedule or send issues | `newsletter.issues.send` (admin, editor) |
| Provider config | `newsletter.provider_config.read` / `newsletter.provider_config.update` (admin) |

`DELETE /admin/newsletter/subscribers/{id}` unsubscribes by default; `?mode=hard_delete` erases
the address, name, IP hash, user agent, and consent feedback but keeps the row, its `erased`
status, and the consent history for suppression. Account erasure (`POST /api/v1/me/activity/erase`)
does the same to the subscriber linked to the account, in the same transaction, recording the
consent event with source `privacy_request`. CSV cells that start with `=`, `+`, `-`, `@`,
tab, or carriage return are prefixed with `'` so spreadsheets do not run them as formulas.
Admin writes go to the audit log.

## Storage

Migration 00030:

| Table | Holds |
| --- | --- |
| `newsletter_lists` | admin-defined lists; archived rather than deleted |
| `newsletter_provider_config` | the singleton sending settings |
| `newsletter_subscribers` | one row per normalized address (`email` is NULL once erased), status, format, source, and timestamps |
| `newsletter_list_memberships` | subscriber × list with `pending`/`active`/`left` |
| `newsletter_tokens` | token hashes by purpose |
| `newsletter_consent_audit` | append-only consent events with source, reason code, feedback, and IP hash |
| `newsletter_issues`, `newsletter_issue_lists` | issues and their target lists |
| `newsletter_deliveries` | one row per issue and recipient: claim, attempts, outcome |

IP addresses are stored only as hashes, as comments store them: HMAC-SHA256 under a key derived
from `APP_ENCRYPTION_KEY`, or plain SHA-256 when the key is unset (see
[Privacy stance](../security/overview.md#privacy-stance)).

## Events

| Event | When | Consumer |
| --- | --- | --- |
| `blog.newsletter.issue.send_requested` | an issue is queued (by an editor or `newsletter-release`) | `newsletter-dispatch` |
| `blog.newsletter.subscriber.changed` | confirm, list or preference change, resubscribe, unsubscribe, bounce, complaint, erase | `newsletter-provider-sync` (custom_http only) |

Payloads carry ids and state only; consumers read the current row. See
[asyncapi.yaml](../architecture/asyncapi.yaml).

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `NEWSLETTER_PROVIDER` | `smtp` | `smtp` or `custom_http` |
| `NEWSLETTER_HTTP_ENDPOINT` | | gateway URL for `custom_http`; https in production |
| `NEWSLETTER_HTTP_SECRET` | | HMAC secret, at least 32 bytes; required for `custom_http`, enables the webhook with either provider |
| `NEWSLETTER_SEND_BATCH` | `50` | recipients claimed per dispatch step, 1–500 |

Confirmation and welcome emails need SMTP (`SMTP_HOST`); without it they are only logged. The
public links use `APP_PUBLIC_URL` for the one-click API URL and the `site.public_url` setting (falling back to `APP_PUBLIC_URL`) for the
site pages (`/newsletter/confirm`, `/newsletter/unsubscribe`, `/newsletter/preferences`).
