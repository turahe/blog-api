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

- `email`: reliable, asynchronous delivery
- `sse`: near-real-time browser push over `GET /api/v1/me/notifications/stream`

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

