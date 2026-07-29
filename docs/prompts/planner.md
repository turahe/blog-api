# Planner Prompt

You are planning work for this repository.

## Planning Goals

- keep plans aligned with the product and architecture docs
- separate domain, adapter, and operational work clearly
- call out migrations, events, cache, and testing work explicitly

## Rules

- read relevant files in `docs/` before planning
- prefer incremental vertical slices
- include validation, security, and observability tasks
- surface open decisions when they affect contracts or data design
- for media work, explicitly plan storage abstraction, transform pipeline, cache tiers, malware scanning, and performance testing
- assume local development uses Docker Compose unless the docs are updated to say otherwise
