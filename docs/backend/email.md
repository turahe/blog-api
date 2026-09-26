# Email

## Purpose

Email is used for account and notification workflows where durable user communication is required.

## Expected Use Cases

- password reset
- email verification
- account recovery notifications
- 2FA recovery updates
- moderation or system notifications
- newsletter confirmation and welcome emails (`newsletter.confirm`, `newsletter.welcome`
  templates) and newsletter issues — see [newsletter.md](newsletter.md)

## Drivers

`MAIL_DRIVER` picks the transport behind the notification `Mailer` port:

| Driver | Adapter | Needs |
| --- | --- | --- |
| `smtp` (default) | `mail.SMTP` | `SMTP_HOST`; empty keeps emails in the log |
| `resend` | `mail.Resend` ([resend-go](https://github.com/resend/resend-go)) | `RESEND_API_KEY` and a `MAIL_FROM` on a domain verified in Resend |

Both drivers send the same message, plain text with an HTML alternative, and reject empty
subjects and line breaks in headers before anything leaves the process. SMTP sends
`multipart/alternative` (quoted-printable parts); Resend gets `text` and `html`. A Resend
failure is a `*mail.ResendError` that keeps the HTTP status.

## Templates

Email copy comes from the notification templates: the built-in catalogue in
`internal/core/notification/template`, overridden per type by rows in
`notification_templates`. Templates are plain text filled with `text/template`.

The HTML part is built from that rendered text with `html/template` and the embedded layout
`internal/core/notification/template/email_layout.html`:

- blank lines separate paragraphs; single line breaks become `<br>`
- `http(s)` URLs become links (trailing `.,;:!?)` stays outside the link)
- the token (`{{.Token}}`) is shown as a code block
- `{{.SiteName}}`, when set, heads the email

Every value is escaped by `html/template`, so a stored template or user-supplied name cannot
inject markup. Edit the layout file to change the look of every transactional email; the text
templates need no HTML.

With `MESSAGE_BROKER` and `APP_ENCRYPTION_KEY` set, emails are encrypted and queued for the
`email-dispatch` consumer in `app worker`, which then needs the same driver settings; see
[events.md](events.md#email-dispatch). In production a broker without the key is a startup
error.

Newsletter issues go out through the same driver by default, one message per recipient from
`app worker`, or through a signed `custom_http` gateway (`NEWSLETTER_PROVIDER`).

## Rules

- keep email sending behind an outbound port
- do not send email directly from HTTP handlers
- template emails with clear plain-language content
- avoid leaking sensitive internal data in email bodies
- log delivery attempts without exposing message secrets
