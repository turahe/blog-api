# Relative Link Usage Rules

This specification defines how every cross-reference link inside every Markdown
document in this repository **must** be written. Compliance is enforced by
**PR review** (the former Node link checker under `scripts/` was removed).

The repository uses **relative links only**. There are zero exceptions for
repository-internal references. Absolute `file:///…` paths, server-root-absolute
paths such as `/docs/backend/database.md`, and scheme-based URLs other than
`http(s)://…` / `mailto:` for external references are **forbidden**.

---

## 1. Structured Relative Link Format

The canonical format for repository-internal Markdown links is:

```
[DISPLAY_TEXT](<RELATIVE_PATH>[#ANCHOR] ["OPTIONAL_TITLE"])
```

where:

| Fragment | Definition & Constraints |
|---|---|
| `DISPLAY_TEXT` | Human-readable link text. Prefer the **basename of the target file**, optionally followed by a short section cue (e.g. `[database.md §Notes]`). |
| `RELATIVE_PATH` | Relative filesystem path from the **current Markdown file's directory** to the target. Always `/` separators. Case-sensitive. |
| `#ANCHOR` (optional) | Numeric line-range `#L<start>-L<end>` / `#L120`, **or** a GitHub-compatible heading slug. |
| `"OPTIONAL_TITLE"` (optional) | Hover tooltip; keep display text descriptive for accessibility. |

Prefer `./sibling.md` over bare `sibling.md` for same-directory links.

---

## 2. Written Rules

### Rule 0 — Absolute links are forbidden

**Scope**: every Markdown file in the repository (`README.md`, `docs/`,
`contracts/`, `paths/`, `tests/`, and repo root).

| Bad | Good |
|---|---|
<!-- relative-links nolint-begin -->
| `[x](file:///…/docs/backend/database.md)` | Depends on source — use a relative path |
| `[x](/docs/backend/database.md)` | From `docs/backend/api.md`: `[database.md](./database.md)` |
| `[x](/contracts/openapi.yaml)` | From `docs/features/x.md`: `[openapi.yaml](../../contracts/openapi.yaml)` |
| `[x](C:\repo\docs\backend\database.md)` | Relative path only |
<!-- relative-links nolint-end -->

External **HTTP/HTTPS/Mailto** are allowed:
- `[Mermaid 10.x](https://mermaid.js.org/intro/)`
- `[contact support](mailto:support@example.com)`

### Rule 1 — Same-directory references

`RELATIVE_PATH` MUST begin with `./`. Do not use bare filenames.

<!-- relative-links nolint-begin -->
| Source | Target | Correct link |
|---|---|---|
| `docs/backend/api.md` | `docs/backend/database.md` | `[database.md](./database.md)` |
| `docs/backend/api.md` | `docs/backend/database.md#L62-L457` | `[database.md §Cloud SQL](./database.md#L62-L457)` |
<!-- relative-links nolint-end -->

### Rule 2 — Sub-directory references (descending)

Use `./dirname/file.md`. No leading `../`.

### Rule 3 — Cross-directory references

Navigate upward with `..` to the closest shared ancestor, then descend.
Do not resolve above the repository root.

<!-- relative-links nolint-begin -->
| Source (dir) | Target | Correct relative link |
|---|---|---|
| `docs/architecture/` | `docs/backend/database.md` | `[database.md](../backend/database.md)` |
| `docs/features/` | `contracts/openapi.yaml` | `[openapi.yaml](../../contracts/openapi.yaml)` |
| `docs/guides/` | `README.md` | `[README.md](../../README.md)` |
<!-- relative-links nolint-end -->

### Rule 4 — Numeric line-range anchors

Use `#L<start>-L<end>` (or `#L120`) when citing exact prose lines. Prefer these
when a heading rename would break a section cue.

### Rule 5 — Heading anchors

Use GitHub-compatible heading slugs: lowercase, spaces → `-`, drop other
punctuation. Example: `## 3. Connection Lifecycle` → `#3-connection-lifecycle`.

### Rule 6 — Renamed or moved documents

1. Rename with `git mv` (or equivalent).
2. Update every Markdown reference that pointed at the old path (search the
   basename).
3. Update display text that still names the old basename.
4. Spot-check links in the IDE preview and on the PR blob view.

### Rule 7 — Review checklist

In PRs that touch Markdown, reviewers should confirm:

1. No `file:///…` or root-absolute `/docs/…` links.
2. No `javascript:` / `data:` schemes.
3. Targets exist on disk.
4. Heading / line anchors still make sense after edits.
5. Display text is descriptive (avoid `[here]` / `[link]`).

---

## 3. Contributor examples

### Same-directory — from `docs/backend/api.md`

<!-- relative-links nolint-begin -->
| Correct | Incorrect |
|---|---|
| `[database.md](./database.md)` | `[database.md](database.md)` (bare sibling) |
| `[database.md §Notes](./database.md#L579-L641)` | `[here](./database.md)` (generic display text) |
<!-- relative-links nolint-end -->

### Cross-directory — from `docs/features/` to `paths/`

<!-- relative-links nolint-begin -->
| Correct | Incorrect |
|---|---|
| `[notifications.yaml](../../paths/notifications.yaml)` | `[notifications.yaml](/paths/notifications.yaml)` |
<!-- relative-links nolint-end -->

---

## 4. Environments where links must resolve

| Environment | How to spot-check |
|---|---|
| Local IDE Markdown preview | Ctrl/Cmd-click a sample of same-dir, cross-dir, and anchor links |
| GitHub PR blob view | Relative `./` / `../` and `#heading-slug` / `#L62-L457` |
| Static docs export (if used) | Confirm the exporter preserves relative Markdown links |

---

## Versioning

Current rules version: **1.1.0** — automated `scripts/docs` link checker removed;
rules remain review-enforced.

Related: [Mermaid Competency Framework](./mermaid-competency-framework.md).
