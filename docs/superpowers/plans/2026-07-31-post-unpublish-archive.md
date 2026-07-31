# Post Unpublish + Archive Implementation Plan

> **For agentic workers:** Use executing-plans or subagent-driven-development.

**Goal:** Ship `admin.posts.unpublish` and `admin.posts.archive`.

**Architecture:** Extend `PostService` with Unpublish/Archive mirroring Publish; OpenAPI paths; HTTP handlers + seed `post.archive`.

**Tech Stack:** Go 1.26.5, Gin, existing post repository Update.

See [design](../specs/2026-07-31-post-unpublish-archive-design.md).
