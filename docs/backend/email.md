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

Both drivers send the same plain-text message and reject empty subjects and line breaks in
headers before anything leaves the process. A Resend failure is a `*mail.ResendError` that
keeps the HTTP status.

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
