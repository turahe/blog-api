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

Newsletter issues go out through the same SMTP settings by default, one message per recipient
from `app worker`, or through a signed `custom_http` gateway (`NEWSLETTER_PROVIDER`).

## Rules

- keep email sending behind an outbound port
- do not send email directly from HTTP handlers
- template emails with clear plain-language content
- avoid leaking sensitive internal data in email bodies
- log delivery attempts without exposing message secrets
