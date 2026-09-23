# Security Change Checklist

Use before merging any auth, RBAC, media, settings, impersonation, or privacy change.

## Contract

- [ ] OpenAPI `security` matches intended auth mode (`required` / `none` / `optional`)
- [ ] OpenAPI tag base name maps to the correct route group
- [ ] Error codes documented; no user enumeration on auth endpoints

## Enforcement

- [ ] Authorization in middleware and/or service — not UI-only
- [ ] Allowlisted patch/body fields; unknown keys → `400`
- [ ] CSRF on browser state-changing routes
- [ ] Step-up on high-risk actions (see [authn-authz.md](./authn-authz.md))
- [ ] Rate limits defined for abuse-prone endpoints

## Data

- [ ] No secrets or `server_only` settings in responses
- [ ] Privacy toggles enforced on public profile reads
- [ ] Media uploads validate type, size, magic bytes; malware scan before finalize
- [ ] Audit log written for security-sensitive mutations (actor + target + request_id)

## Tests

- [ ] Positive and negative authz cases (401 / 403 / 404 privacy)
- [ ] Impersonation: no privilege elevation; audit metadata present
- [ ] Timing-safe auth responses where required (forgot password)

## Ops

- [ ] New env vars added to [.env.example](../../.env.example) without real secrets
- [ ] Docs updated under [docs/security](./README.md) if policy changed
