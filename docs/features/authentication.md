# Authentication

## Scope

- admin login
- session or token renewal
- logout
- password-based auth
- social login
- TOTP-based 2FA

## Requirements

- support multiple internal users
- enforce RBAC on protected routes
- require recent 2FA for high-risk actions
- audit important auth events

## Security Rules

- hash passwords
- encrypt TOTP secrets
- hash backup codes
- rate limit login and challenge endpoints

## Implemented: TOTP 2FA

- RFC 6238 TOTP (SHA-1, 6 digits, 30 s, ±1 step), enrolled at `/api/v1/me/2fa/*`.
  Login returns a challenge token that `POST /api/v1/auth/2fa/challenge` exchanges for tokens.
- Secrets are encrypted with AES-256-GCM under `APP_ENCRYPTION_KEY`. The 10 single-use
  backup codes are stored as keyed HMACs.
- Each time step is accepted once. A challenge lasts 5 minutes and dies after 5 attempts.
- Without the key, enrollment is off and enrolled accounts fail closed (`503`).
- Still open: "recent 2FA" step-up for high-risk actions, and auth audit events (phase 3).

See `docs/backend/api.md` → Two-factor authentication for the contract.

## Related Documents

- `docs/architecture/security.md`
- `docs/backend/api.md`
- `docs/backend/events.md`
