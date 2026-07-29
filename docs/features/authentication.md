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

## Related Documents

- `docs/architecture/security.md`
- `docs/backend/api.md`
- `docs/backend/events.md`
