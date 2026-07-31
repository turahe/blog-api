---
name: security-change-checklist
description: Walks through the project security checklist for auth, RBAC, CSRF, privacy, media, and secrets changes. Use when modifying authentication, authorization, impersonation, privacy, settings, or upload handling.
---

# Security change checklist

## Instructions

1. Read [checklist.md](../../../docs/security/checklist.md) and complete every applicable box in the PR description.
2. Confirm contract `security` matches runtime intent ([authn-authz.md](../../../docs/security/authn-authz.md)).
3. Confirm no secrets in logs or responses ([secrets-and-headers.md](../../../docs/security/secrets-and-headers.md)).
4. Add negative tests (401/403/404 privacy) per [testing/checklist.md](../../../docs/testing/checklist.md).
5. If new env vars: update `.env.example` with placeholders only.

## High-risk triggers (extra scrutiny)

- Impersonation, RBAC policy, password/reset, 2FA
- `/me` privacy or GDPR erase
- Admin settings / `server_only` keys
- Media upload or transform
- SSE fan-out identity binding

## Output

Summarize for the user:

- what was enforced
- what tests were added
- residual risks / follow-ups
