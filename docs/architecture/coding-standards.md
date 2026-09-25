# Coding Standards

## General

- keep code clear, small, and explicit
- optimize for maintainability before cleverness
- prefer ASCII unless the file already uses Unicode intentionally
- add short comments only where logic is not self-evident

## Documentation Standards

- **Relative links only**: every cross-reference link inside Markdown documents must be relative to the current file's directory. Absolute `file:///…` paths and root-absolute `/docs/…` paths are forbidden. Full rules: [relative-link-usage-rules.md](../guides/relative-link-usage-rules.md)
- Review relative links manually in PRs that modify Markdown (no automated link checker in-repo).
- When renaming or moving a referenced document, follow the rename/move protocol in the rules document.
- For Mermaid diagrams, follow the [mermaid-competency-framework.md](../guides/mermaid-competency-framework.md) authoring checklist.

## Go Standards

- keep packages focused on one responsibility
- use constructor functions for services and adapters
- pass `context.Context` through request and service boundaries
- return typed domain errors where possible
- avoid global mutable state

## Hexagonal Rules

- domain entities must not import adapter packages
- ports define all inbound and outbound contracts
- handlers translate HTTP to use-case calls only
- repositories map database models to domain models
- event publishers stay behind outbound ports

## API Standards

- use `/api/v1` versioned routes
- return JSON only
- use a consistent success and error envelope
- validate all inputs server-side
- never trust client IDs for authorization decisions

## Naming

- package names should be short and descriptive
- interface names should reflect business capability, not storage detail
- event names should use dot-separated domain form such as `blog.post.published`
- REST JSON fields and query parameters are camelCase (`categoryId`, `perPage`); database
  columns, event payloads, and stored JSON stay snake_case. Convert in the HTTP adapter
  (`responses`, `requests`), not by changing domain or storage names — see
  `docs/backend/api.md` → Conventions

## Testing

- write unit tests for domain and service logic
- write integration tests for repositories, cache, and event adapters
- add request-level tests for critical HTTP flows
- keep tests deterministic and isolated

## Git and Review Hygiene

- keep changes scoped to one concern where possible
- update docs when contracts or architecture change
- treat security-sensitive changes as review-required by default
