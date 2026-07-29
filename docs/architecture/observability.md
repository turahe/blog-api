# Observability

## Goals

- detect failures quickly
- troubleshoot production issues efficiently
- understand user-impacting latency and error rates
- preserve auditability for sensitive actions

## Logs

- use structured logs
- include request ID, user ID when available, route, status code, latency, and error code
- never log passwords, tokens, TOTP secrets, or backup codes

## Metrics

Track at minimum:

- request count, latency, and error rate by endpoint
- database latency and error rate
- Redis latency and error rate
- event publish and consumer success/failure
- login, 2FA, and recovery attempt rates
- media upload success/failure, transform latency, cache hit ratio, and scanner rejection rate

## Tracing

- propagate request IDs across HTTP, DB, cache, and event boundaries
- use distributed tracing if workers and broker consumers are separated

## Audit Logging

Sensitive actions should create durable audit records for:

- auth events
- permission changes
- content publication events
- recovery and 2FA actions

## Health Checks

- liveness endpoint for process health
- readiness endpoint for dependency health
- optional deeper diagnostics for internal ops use

## Alerting

Alert on:

- elevated auth failures
- repeated 2FA failures
- high 5xx rate
- consumer backlog or event publish failure
- dependency outages
- elevated media transform latency or storage error rates
