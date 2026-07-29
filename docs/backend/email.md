# Email

## Purpose

Email is used for account and notification workflows where durable user communication is required.

## Expected Use Cases

- password reset
- email verification
- account recovery notifications
- 2FA recovery updates
- moderation or system notifications

## Rules

- keep email sending behind an outbound port
- do not send email directly from HTTP handlers
- template emails with clear plain-language content
- avoid leaking sensitive internal data in email bodies
- log delivery attempts without exposing message secrets
