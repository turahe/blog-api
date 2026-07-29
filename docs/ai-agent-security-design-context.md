# AI Agent Security and Design Context

## Purpose

This document gives AI agents implementation guardrails for the authentication and user management system. It is intended to reduce security mistakes, keep the user experience consistent, and make future code generation align with the approved product and technical documents.

## Scope

Apply this context to:

- login and registration
- profile view and profile edit flows
- TOTP-based 2FA setup, challenge, disable, and recovery
- backup codes
- social login and account linking
- admin security and policy screens

## Security Priorities

Always optimize for these outcomes first:

- protect account integrity
- avoid leaking secrets or security-sensitive state
- prevent account takeover and privilege escalation
- require explicit authorization on every protected action
- minimize stored third-party data

## Core Security Rules

### Authentication

- all profile and security routes require an authenticated session
- users may only access and modify their own profile unless an admin permission explicitly allows otherwise
- high-risk actions must require recent 2FA verification
- use secure, http-only, same-site cookies or equivalently strong server-managed session controls
- never trust client-provided user IDs for access control

### Passwords and Secrets

- store passwords only as strong password hashes such as argon2id or bcrypt
- never log raw passwords, TOTP secrets, backup codes, session tokens, or OAuth tokens
- encrypt stored TOTP secrets at rest
- store backup codes as hashes, not plaintext
- show backup codes only once at generation time

### Two-Factor Authentication

- require TOTP verification before marking 2FA as enabled
- require recent password or session re-verification before disabling 2FA
- rate limit TOTP and backup code attempts
- invalidate used backup codes immediately
- recovery flow must be auditable and step-based, never a silent bypass

### Social Login

- use OAuth 2.0 or OpenID Connect best practices
- validate `state`, `nonce`, issuer, audience, and callback expectations
- store only required profile attributes such as provider ID, email, avatar URL, and display name when needed
- account linking must require a currently authenticated local session or a verified ownership step
- never auto-link accounts based only on matching display names

### Authorization and RBAC

- enforce permissions on the server, not only in the UI
- treat hidden buttons as presentation only, not security
- admin security screens require explicit admin permissions
- separate self-service permissions from admin management permissions

### API and Data Handling

- validate every input server-side
- use allowlists for editable profile fields
- do not expose internal flags, token material, provider secrets, or recovery metadata in public API responses
- use consistent error responses without revealing whether sensitive identifiers are valid
- add audit logs for login, 2FA changes, social account linking, recovery actions, and admin policy changes

## Recommended High-Risk Actions

Require recent 2FA verification for:

- disabling 2FA
- regenerating backup codes
- changing email address
- changing password
- linking or unlinking a social provider
- starting privileged admin actions

Recommended recent-verification window:

- 5 to 15 minutes depending on action sensitivity

## Design Intent

The product should feel secure, calm, and trustworthy rather than intimidating. The design language should reduce user anxiety during sensitive flows like 2FA setup or account recovery.

Design tone:

- calm
- precise
- reassuring
- transparent

Avoid:

- alarm-heavy wording
- flashy visuals in security-critical flows
- ambiguous success and failure states
- destructive actions without confirmation

## UX Rules for Security Flows

### Profile Page

- show avatar, display name, email, and account creation date clearly
- label whether the email is verified
- show connected providers and 2FA status in a compact security summary
- separate editable personal information from security settings

### Profile Editing

- editable fields must be clearly scoped
- email change should trigger a verification or confirmation workflow
- show save state, validation errors, and success confirmation clearly
- do not silently overwrite security-sensitive fields

### 2FA Setup

- present setup as a step-by-step flow
- show QR code and manual secret entry fallback
- explain what authenticator apps are supported
- only show success after the user submits a valid TOTP code
- provide backup codes immediately after successful activation

### Backup Codes

- present backup codes in a readable, copyable layout
- warn the user that codes are shown only once
- regeneration must revoke the previous set
- use confirmation for regeneration

### Recovery Flow

- explain recovery consequences and expected review time
- avoid exposing internal review rules or anti-abuse thresholds
- provide clear next steps if the request is pending, approved, or denied

### Social Login and Account Linking

- provider buttons should be recognizable but visually consistent with the app
- explain when the user is signing in versus linking an additional provider
- if account linking needs confirmation, make the ownership check explicit

## Accessibility Rules

- all forms must have visible labels
- error messages must be programmatically associated with fields
- security step flows must be keyboard accessible
- QR setup must include a text fallback for non-visual use
- color alone must not communicate security status
- focus states must remain visible on every interactive element

## API Response Expectations

Prefer these principles:

- return only the authenticated user's own profile from self-service endpoints
- return booleans or summarized status for security settings when the raw secret is not needed
- never return plaintext backup codes except at generation time
- never return provider client secrets to the frontend

## AI Agent Implementation Defaults

If requirements are missing, use these defaults:

- session-based auth with secure cookies
- TOTP as the primary 2FA method
- Google, GitHub, and generic OIDC as supported providers
- server-side QR generation
- hashed backup codes
- audit logging for all auth and security mutations
- responsive desktop-first layouts with mobile-safe forms

## AI Agent Red Flags

Stop and rethink if a proposed change would:

- expose TOTP secrets or backup codes after setup
- let one user request another user's profile directly
- rely on frontend checks for authorization
- auto-link social accounts without a verified ownership step
- disable 2FA without re-authentication or recent verification
- store sensitive credentials in client-side storage

## Deliverable Standard

Any future implementation generated from this context should be:

- secure by default
- explicit about trust boundaries
- accessible and mobile-friendly
- auditable for sensitive actions
- consistent with the approved PRD and technical architecture
