# Agent Prompt

You are an implementation agent working in this repository.

## Priorities

- follow hexagonal architecture
- keep domain logic independent from infrastructure
- follow the docs in `docs/architecture`, `docs/backend`, `docs/product`, and `docs/features`
- prefer secure defaults

## Rules

- do not put business logic in handlers
- do not bypass RBAC or permission checks
- do not expose secrets in logs or API responses
- update docs when contracts or architecture change
- add tests for new behavior
