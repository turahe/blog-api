# Newsletter Subscriptions

## Scope

Public and authenticated users can subscribe to one or more newsletter lists, confirm the
subscription via a signed double-opt-in email link, manage their preferences, and unsubscribe
at any time from a footer link in every outgoing newsletter. Administrators draft, schedule,
preview, and send newsletter issues; all sends integrate with an external Email Service Provider
(ESP) such as Mailchimp, ConvertKit, SendFox, Brevo, MailerLite, Beehiiv, Buttondown, or a
custom SMTP (SES/SMTP). The module records every consent event, suppresses bounces/complaints,
and preserves audit trails for GDPR, CASL, and CAN-SPAM compliance.

## Implementation status

Implemented; the backend is described in [newsletter.md](../backend/newsletter.md). Where this
spec and the build differ, the build is:

| Topic | Built |
| --- | --- |
| Providers | `smtp` (the transactional mailer, sent by the worker) and `custom_http` (signed JSON gateway). The other ESPs in the table below are extension points: a gateway speaking the `custom_http` contract, or a new adapter. |
| Provider config | Provider, endpoint, and secret come from env (`NEWSLETTER_*`); the admin endpoint stores only from/reply-to, postal address, confirm TTL, `double_optin_required`, and the lists. |
| Lists | Several admin-defined lists with default lists for subscribes that name none. |
| Frequency | Only `format` (`html` or `plaintext`). Digests are not built; every issue is sent as it is queued. |
| Content | Markdown rendered to sanitized HTML in a fixed email template; plaintext is derived from the Markdown. No MJML. |
| Tokens | 32 random bytes, SHA-256 on disk. The one-click target is `POST /api/v1/newsletter/unsubscribe?token=…`; there is no mailto unsubscribe. |
| ESP sync | Subscriber changes go through the outbox (`blog.newsletter.subscriber.changed`) to a consumer that pushes current state; broker retries replace the `newsletter_provider_syncs` table. |
| Bounces | A signed webhook (`POST /api/v1/newsletter/webhooks/provider`) for hard bounces, soft bounces, and complaints. |
| Permissions | Public and self-service routes need no permission; the admin routes use the eight `newsletter.subscribers.*`, `newsletter.issues.*`, and `newsletter.provider_config.*` permissions. |
| Monitoring | Structured logs for sends; the metrics and alerts listed below are not built. |

## Capability Matrix

| Action | Anonymous / public | Authenticated user | Newsletter editor | Admin |
|---|---|---|---|---|
| Subscribe new email | ✅ | ✅ | ✅ | ✅ |
| Confirm opt-in (token) | ✅ | ✅ | N/A | N/A |
| Resend confirm | Rate limited | ✅ | ✅ | ✅ |
| Manage preferences by token | ✅ | ✅ (me) | ❌ | ✅ |
| Unsubscribe by token | ✅ | ✅ | ❌ | ✅ |
| View own subscriptions | ❌ | ✅ | ❌ | ✅ |
| Subscribe as current user | N/A | ✅ | ❌ | ✅ |
| Unsubscribe current user | N/A | ✅ | ❌ | ✅ |
| List/search subscribers | ❌ | ❌ | ✅ | ✅ |
| Export subscribers (JSON/CSV) | ❌ | ❌ | ✅ | ✅ |
| Scrub (hard delete PII) subscriber | ❌ | ❌ | ✅ (on request) | ✅ |
| Draft/schedule/edit issue | ❌ | ❌ | ✅ | ✅ |
| Send issue / preview test | ❌ | ❌ | ✅ | ✅ |
| Configure ESP provider | ❌ | ❌ | ❌ | ✅ |

## Integration Model

The platform does **not** own list rendering or SMTP primitives for bulk sends. Instead,
an adapter layer speaks to a pluggable ESP driver:

```
Newsletter service
  ├─ NewsletterSubscribeController         -> create subscriber row, queue double-opt-in
  ├─ NewsletterConfirmController          -> consume token, mark active, sync to ESP
  ├─ NewsletterUnsubscribeController      -> consume token, mark unsubscribed, sync to ESP
  ├─ NewsletterIssuesService              -> draft / schedule / preview / send
  ├─ NewsletterSubscriberPreferencesSvc   -> format / frequency / list preferences
  ├─ EspProviderAdapter (per provider)    -> sync contacts + lists + send + webhook ingress
  └─ NewsletterConsentAuditWriter         -> append-only per consent event
```

### Provider adapter plugs

The `NewsletterProviderConfig.provider` field selects an adapter:

| Provider | Adapter | Notes |
|---|---|---|
| `mailchimp` | Mailchimp Marketing API v3 | List/audience id, API key via secrets manager. |
| `convertkit` | ConvertKit REST v3 | Tags, sequences, custom fields. |
| `sendfox` | SendFox API | Simpler API; limited list metadata. |
| `brevo` | Brevo (Sendinblue) API v3 | SMTP relay + contacts API combo. |
| `mailerlite` | MailerLite API v2 | Groups + double opt-in natively supported. |
| `beehiiv` | Beehiiv REST API v1 | Publication-centric list model. |
| `buttondown` | Buttondown API v1 | Minimal, markdown-first. |
| `mailgun` | Mailgun mailing lists API | For teams self-hosting their SMTP relay. |
| `ses_smtp` | Amazon SES + SNS bounces/complaints | No contact API; list contact state stored locally in DB only. |
| `custom_http` | Webhook adapter | Implementor supplies `api_endpoint` + signed HMAC; good for proxy gateways. |

## Double Opt-In (DOI) Lifecycle

1. `POST /api/v1/newsletter/subscribe` → create subscriber in `status: pending_confirm`; generate a 64-byte CSPRNG token; **never persist raw token**; store only SHA-256 hash + TTL (default 48 hours). Record a `confirm_sent` consent audit event.
2. Transactional email queue → send DOI email containing `confirm_link = SITE_URL + '/newsletter/confirm?token=<raw>'`. If site setting `newsletter.confirm_from_shortcode` is set, mail includes `MAILGUN` / `SES` shortcode token too for SMTP verification.
3. User clicks link → frontend calls `POST /api/v1/newsletter/confirm` with the raw token. Server hashes the token, looks up `pending_confirm` subscriber, transitions status to `active`, records `confirm_clicked` + `opted_in_at`, emits welcome email, and pushes the new contact to the configured ESP via adapter.
4. Retry: If ESP sync fails, create a `newsletter_provider_syncs` row with `sync_status=pending` and exponential backoff. Queue background job.
5. Idempotency: repeat confirmations for a consumed token return HTTP 409/410 depending on age; never reveal whether the email existed in any endpoint response shape.

## Unsubscribe Requirements (every send, no exceptions)

All outgoing newsletter issue emails MUST include, **per send and per recipient**:

1. A prominent footer link labeled "Unsubscribe" → url `SITE_URL + '/newsletter/unsubscribe?token=<per-recipient>'` with a **per-recipient** signed token (hashed per issue send).
2. RFC 8058 `List-Unsubscribe` headers:
   - `List-Unsubscribe: <https://api.example.com/api/v1/newsletter/unsubscribe-header?sid=...>, <mailto:unsubscribe@example.com?subject=unsubscribe>`
   - `List-Unsubscribe-Post: List-Unsubscribe=One-Click`
3. A `List-Unsubscribe` one-click POST endpoint that processes signed sid tokens without requiring user login.
4. When a token hits POST `/api/v1/newsletter/unsubscribe`, transition status to `unsubscribed` immediately and **suppress any further sends** within the same job run.
5. Append the action to `newsletter_consent_audit` (reason code + free-text feedback).
6. Push suppression to the ESP adapter so it also marks the contact unsubscribed at the provider.

## Rules

- **No PII in URLs**: tokens are opaque, signed, and hashed on disk. Tokens expire: confirm TTL 48 h, unsubscribe token rotated per issue (expires after 30 days of issue send + 7 days grace).
- **Non-enumeration**: subscribe endpoint always returns 201/202 regardless of whether email is new or existing; never distinguish "new" vs "duplicate" in client responses.
- **Honeypot + rate limit**: public subscribe requires `honeypot` empty; configurable turnstile. Per-IP hash: max 5 subscribe attempts / 5 minutes; per email: max 3 confirm resends per 30 minutes.
- **Double opt-in required by default**: `NewsletterProviderConfig.double_optin_required=true`. Setting this to false only disables DOI for admins managing migration imports — public subscribe always uses DOI regardless.
- **Frequency preferences**: honor `format (html | plaintext)` and `frequency (instant | daily_digest | weekly_digest | monthly_digest)` before send.
- **Bounce + complaint handling**: ESP webhooks (`bounced_at`, `complained_at`) write to `newsletter_subscribers`, create `provider_syncs` entry, unsubscribe from list. FBL/complaint moves status to `complained` and suppresses permanently unless user explicitly opts back in.
- **Erasure (GDPR)**: `DELETE /admin/newsletter/subscribers/{id}?mode=hard_delete` or `delete_data=true` from unsubscribe: zero out PII fields (email, display_name, normalized_email, ip_hash, ua), retain status + audit for suppression lists. Consent audit retained per legal basis if any.
- **Esp sync correctness**: every update (`active`, `unsubscribed`, bounce, new fields, new list membership) creates a `newsletter_provider_syncs` pending row and schedules a retry. Max retries = 10, then manual review required (error alert + ops dashboard).
- **CAN-SPAM physical address**: `NewsletterProviderConfig.from_name / from_email / reply_to` plus site mailing address must be injected into every issue footer template.

## Key Entities

- `newsletter_subscribers` — canonical subscriber rows (PK `id`, unique `email`), GDPR fields + tokens.
- `newsletter_list_memberships` — per-list join (1 subscriber : N lists).
- `newsletter_issues` — drafts/scheduled/sent newsletter sends with engagement aggregate counters.
- `newsletter_provider_syncs` — per-subscriber and per-issue ESP state + retries + errors.
- `newsletter_consent_audit` — append-only, immutable record of every consent/opt event.
- Optional external: ESP contact records, lists, tags (authoritative copy is in the ESP).

## Key Permissions

- `newsletter.subscribe` — default allow (public w/ rate limit)
- `newsletter.confirm` — default allow
- `newsletter.manage.own` — authenticated user manage own subscriptions
- `newsletter.subscribers.read` — editors+
- `newsletter.subscribers.export` — editors+
- `newsletter.subscribers.erase` — privacy officer / admin
- `newsletter.issues.read` — editors+
- `newsletter.issues.edit` — editors+
- `newsletter.issues.send` — editors+
- `newsletter.provider_config.read` — site manager+
- `newsletter.provider_config.update` — site manager / admin

## Key API Endpoints (paths/newsletter.yaml for full contracts)

- Public
  - `POST /api/v1/newsletter/subscribe`
  - `POST /api/v1/newsletter/confirm`
  - `POST /api/v1/newsletter/confirm/resend`
  - `POST /api/v1/newsletter/unsubscribe`
  - `GET /api/v1/newsletter/preferences/{token}` + PATCH
- Self
  - `GET /api/v1/me/newsletter/subscriptions`
  - `POST /api/v1/me/newsletter/subscribe`
  - `POST /api/v1/me/newsletter/unsubscribe`
- Admin / editor
  - `GET /api/v1/admin/newsletter/subscribers` + optional CSV export
  - `GET /api/v1/admin/newsletter/subscribers/{id}`
  - `DELETE /api/v1/admin/newsletter/subscribers/{id}` soft/hard
  - `GET /api/v1/admin/newsletter/issues`
  - `POST /api/v1/admin/newsletter/issues` schedule or send
  - `GET /api/v1/admin/newsletter/issues/{id}`
  - `PATCH /api/v1/admin/newsletter/issues/{id}` update draft/scheduled or cancel
  - `GET /api/v1/admin/newsletter/provider-config`
  - `PUT /api/v1/admin/newsletter/provider-config`

## Browser / UX Requirements

- **Responsive embed**: Subscribe form renders in embeds (width >= 280 px), breakpoints at 480/768 px, no horizontal scroll. Submission buttons have sufficient hit target (>= 44×44 px).
- **Cross-browser**: Latest Chrome, Firefox, Safari, Edge; iOS Safari 16+, Android Chrome 120+. Progressive enhancement: no-JS form posts to same endpoint, returns confirmation page. JS-only provides live email validation.
- **Accessibility**: `<form aria-label="Subscribe to newsletter">`, field labels always visible, error messages tied with `aria-describedby`, confirmation pages have clear status messages. Captcha challenges include audio fallback.
- **Double opt-in flow UX**: Subscription page shows "Check your inbox to confirm." Confirm page shows "Thanks! You're confirmed." Re-token-expired pages show explicit "Link expired — request a new confirmation email."
- **Unsubscribe flow UX**: One-click link in footer → POST → auto-confirms unsubscribed + shows "You have been unsubscribed" + optional feedback form (reason code + free text, all optional).
- **Email templates**: HTML templates are fully responsive with MJML/Foundation-for-emails style, plaintext auto-generated, unsubscribe links in both. Images optional on; alt text required.

## Testing (see docs/backend/testing.md:Newsletter)

Unit tests cover:
- token generation + storage (never raw on disk)
- subscribe endpoint non-enumeration: same 202 shape for existing vs new emails
- double opt-in TTL expiry & re-consume prevention
- unsubscribe token uniqueness + suppression enforcement
- ESP idempotency keys / retry backoff in `newsletter_provider_syncs`
- GDPR scrubbing behavior (PII zeroed + audit retained)

Integration tests cover:
- Real sandbox adapter `custom_http` against a test harness webhook
- Full cycle: subscribe → confirm → send issue → unsubscribe → resubscribe → bounce webhook
- Webhook security (HMAC / IP allow lists)

E2E tests:
- Browser subscribe/confirm flow on responsive layout
- Issue editor draft → preview test recipients → schedule → publish

## Monitoring / Logging

- Events: `newsletter.subscribe`, `newsletter.confirm.clicked`, `newsletter.confirm.rate_limit`,
  `newsletter.issue.send.started`, `newsletter.issue.send.completed`, `newsletter.unsubscribe.clicked`,
  `newsletter.provider_sync.retry.exhausted`.
- Metrics: `subscribers_created_total`, `confirmations_total`, `confirm_fail_total`, `unsubscribes_total`,
  `issues_sent_total`, `issue_send_errors_total`, `esp_sync_queue_depth`, `open_rate_30d` (pingback).
- Alerts: `esp_sync_queue_depth > 500`, `issue_send_errors > 0`, `confirm_fail_rate > 10%`.
