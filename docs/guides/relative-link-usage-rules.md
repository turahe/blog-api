# Relative Link Usage Rules (enforced)

This specification defines — formally and operationally — how every cross-reference link inside every Markdown document in this repository **must** be written. The rules are enforced automatically by `scripts/docs/validate_relative_links.cjs` in every PR that modifies Markdown documents, and violations will **block** merge when the validator returns a non-zero exit code.

The repository uses **relative links only**. There are zero exceptions for repository-internal references. Absolute `file:///…` paths, server-root-absolute paths such as `/docs/backend/database.md`, and scheme-based URLs other than `http(s)://…` for external references are **forbidden**.

> Enforcer script: [scripts/docs/validate_relative_links.cjs](../../scripts/docs/validate_relative_links.cjs) (clickable via relative rules!)
> CI workflow (expected): see `.github/workflows/docs-lint.yml` — runs validator on every PR touching `docs/**/*.md`, `contracts/**/*.yaml`, `paths/**/*.yaml`, `README.md`.

---

## 1. Structured Relative Link Format

The canonical format for repository-internal Markdown links is:

```
[DISPLAY_TEXT](<RELATIVE_PATH>[#ANCHOR] ["OPTIONAL_TITLE"])
```

where:

| Fragment | Definition & Constraints |
|---|---|
| `DISPLAY_TEXT` | Human readable link text. For file links this MUST be the **basename of the target file** optionally followed by a one-line description and optional section name. E.g. `[database.md §Notes]`, `[api.md §SSE rate-limit]`, `[ERD.md]`. The display text is NEVER used as a URI or path — renaming a file MUST update display text in every reference, not rely on path-in-display. |
| `RELATIVE_PATH` | Relative filesystem path from the **current Markdown file's directory** to the target file. Always forward-slashes `/` on every OS. Preserves case (Linux CI is case-sensitive). Rules for composing `RELATIVE_PATH` are enumerated in §2. |
| `#ANCHOR` (optional) | Either: **numeric line-range anchor** `#L<start>-L<end>` e.g. `#L62-L457`, a single line `#L120`, **OR** a **GitHub-compatible heading-slug anchor** `#connection-lifecycle` matching the target heading slug exactly. Numeric line ranges take precedence for cross-references to specific prose. Heading anchors must match a real heading in the target file (validator checks heading slugs per §3). |
| `"OPTIONAL_TITLE"` (optional) | Mouse-hover tooltip text, quoted in double quotes, placed after a single space before the closing `)`. Do not rely on titles — keep display text descriptive for accessibility. |

### 1.1 Normalization rules applied automatically by the validator

The validator treats all of the following as **equivalent** and rewrites to the canonical short form during `--fix`:

- `./sibling.md` → `./sibling.md` (kept if same dir)
- `./subdir/file.md` → `./subdir/file.md`
- `sibling.md` → rewritten to `./sibling.md` (explicit dot-slash preferred)
- Encoded spaces in display paths that are actually filenames: `My%20File.md` → automatically decoded during resolution (validator is lenient for reading, emits a warning for encoding inside repo-relative links).

---

## 2. Written Rules (Syntax, Cross-Directory, Rename/Move, Validation)

### Rule 0 — Absolute links are forbidden (hard-block on CI)

**Scope**: every Markdown file in the repository, including `README.md`, all files under `docs/`, `contracts/`, `paths/`, `tests/`, `scripts/`, and repo root.

| Bad (blocked by exit code 2) | Good |
|---|---|
<!-- relative-links nolint-begin -->
| `[x](file:///mnt/myadrive/repo/turahe/blog-api/docs/backend/database.md#L62-L457)` | — depends on source file (see below) |
| `[x](/docs/backend/database.md)` (server-root absolute) | From `docs/backend/api.md`: `[database.md](./database.md)` |
| `[x](/contracts/openapi.yaml)` | From `docs/features/x.md`: `[openapi.yaml](../../contracts/openapi.yaml)` |
| `[x](C:\repo\docs\backend\database.md)` (Windows absolute) | Validator rejects on general absolute-link rules. |
<!-- relative-links nolint-end -->

External **HTTP/HTTPS/Mailto** are explicitly allowed and never blocked (no rewrite performed):
- `[Mermaid 10.x](https://mermaid.js.org/intro/)` — fine.
- `[contact support](mailto:support@example.com)` — fine.

### Rule 1 — Same-directory references

When the target file lives alongside the source file, `RELATIVE_PATH` MUST begin with `./`. Do NOT use bare filenames.

<!-- relative-links nolint-begin -->
| Source | Target | Correct link |
|---|---|---|
| `docs/backend/api.md` | `docs/backend/database.md` | `[database.md](./database.md)` |
| `docs/backend/api.md` | `docs/backend/database.md#L62-L457` | `[database.md §Cloud SQL](./database.md#L62-L457)` |
<!-- relative-links nolint-end -->

Why? A bare `database.md` is indistinguishable from a URL scheme-less external link to some parsers and confuses link-highlighting in some editors. The validator emits formatted violation for bare sibling filenames.

### Rule 2 — Sub-directory references (descending)

Use `./dirname/file.md`. No leading `../`.

<!-- relative-links nolint-begin -->
| Source | Target | Correct |
|---|---|---|
| `docs/guides/mermaid-competency-framework.md` | `docs/guides/_mermaid-templates.md` | `[_mermaid-templates.md](./_mermaid-templates.md)` |
| `docs/architecture/architecture.md` | `docs/architecture/security.md` | `[security.md](./security.md)` |
<!-- relative-links nolint-end -->

### Rule 3 — Cross-directory references (ascending, then descending)

Every cross-reference that leaves the current directory MUST navigate upward via `..` segments until reaching the **closest shared ancestor directory**, then descend via the remaining path. Do NOT traverse above the repository root. The validator blocks links that resolve outside `--root` (exit code 3).

Directory abbreviations used below:

```
<repo>/
  docs/
    architecture/   (AR)
    backend/        (BE)
    features/       (FT)
    guides/         (GU)
    product/        (PR)
  contracts/        (CO)
  paths/            (PA)
  scripts/docs/     (SC)
```

#### Example cross-directory link matrix (hand-verified)

<!-- relative-links nolint-begin -->
| Source (dir) | Target (path relative to <repo>) | Correct relative link |
|---|---|---|
| `docs/architecture/` tech-stack.md | `docs/backend/database.md#L62-L457` | `[database.md](../backend/database.md#L62-L457)` |
| `docs/architecture/` architecture.md | `docs/features/realtime-notifications-sse.md` | `[realtime-notifications-sse.md](../features/realtime-notifications-sse.md)` |
| `docs/architecture/` tech-stack.md | `docs/guides/mermaid-competency-framework.md` | `[mermaid-competency-framework.md](../guides/mermaid-competency-framework.md)` |
| `docs/architecture/` tech-stack.md | `docs/backend/ERD.md` | `[ERD.md](../backend/ERD.md)` |
| `docs/guides/` mermaid-competency.md | `docs/backend/ERD.md` | `[ERD.md](../backend/ERD.md)` |
| `docs/guides/` mermaid-competency.md | `docs/features/authentication.md` | `[authentication.md](../features/authentication.md)` |
| `docs/guides/` mermaid-competency.md | `docs/tasks/README.md` | `[README.md](../tasks/README.md)` |
| `docs/backend/` ERD.md | `docs/guides/mermaid-competency-framework.md` | `[mermaid-competency-framework.md](../guides/mermaid-competency-framework.md)` |
| `docs/backend/` ERD.md | `docs/backend/database.md#L579-L641` | `[database.md §Notes](./database.md#L579-L641)` |
| `docs/backend/` rbac-casbin.md | `docs/features/rbac-with-casbin.md` | `[rbac-with-casbin.md](../features/rbac-with-casbin.md)` |
| `docs/backend/` rbac-casbin.md | `docs/architecture/security.md` | `[security.md](../architecture/security.md)` |
| `docs/backend/` rbac-casbin.md | `contracts/openapi.yaml` | `[openapi.yaml](../../contracts/openapi.yaml)` |
| `docs/backend/` rbac-casbin.md | `contracts/asyncapi.yaml` | `[asyncapi.yaml](../../contracts/asyncapi.yaml)` |
| `docs/backend/` api.md | `paths/notifications.yaml#L41-L153` | `[notifications.yaml](../../paths/notifications.yaml#L41-L153)` |
| `docs/features/` realtime-notifications-sse.md | `paths/notifications.yaml#L41-L153` | `[notifications.yaml](../../paths/notifications.yaml#L41-L153)` |
| `docs/features/` realtime-notifications-sse.md | `contracts/asyncapi.yaml` | `[asyncapi.yaml](../../contracts/asyncapi.yaml)` |
| `docs/features/` realtime-notifications-sse.md | `docs/backend/api.md#L73-L88` | `[api.md](../backend/api.md#L73-L88)` |
| `docs/features/` realtime-notifications-sse.md | `docs/backend/events.md#L103-L155` | `[events.md](../backend/events.md#L103-L155)` |
| `docs/features/` rbac-with-casbin.md | `docs/backend/rbac-casbin.md` | `[rbac-casbin.md](../backend/rbac-casbin.md)` |
| `docs/features/` rbac-with-casbin.md | `docs/features/authentication.md` | `[authentication.md](./authentication.md)` |
| `scripts/docs/` validate_relative_links.cjs (README inside scripts dir) | `docs/guides/relative-link-usage-rules.md` | `[relative-link-usage-rules.md](../../docs/guides/relative-link-usage-rules.md)` |
<!-- relative-links nolint-end -->

### Rule 4 — Numeric line-range anchors (preferred for specific prose)

Use `#L<start>-L<end>` when citing exact line ranges in another document:

- Minimum form: `#L120` (single line)
- Ranged form: `#L62-L457`
- Start ≤ end; if the range goes beyond the target file's line count the validator reports `[BROKEN LINE RANGE]` exit code 3.

**Do not** invent `#connection-lifecycle` style heading anchors when you actually mean lines 62–457 of the Cloud SQL section — if the heading is renamed, specific prose lines stay correct.

### Rule 5 — Heading anchors

When referencing a full section (i.e. "see the SSE Lifecycle chapter"), use the GitHub-compatible heading slug: convert heading title to lowercase, replace spaces with `-`, drop non-letter/number non-dash chars, collapse multiple dashes. **No emoji in anchors**. Example:

- Target heading: `## 3. Connection Lifecycle` → slug `3-connection-lifecycle`.
- Cross-ref from `docs/backend/api.md`: `[§3 Connection Lifecycle](../features/realtime-notifications-sse.md#3-connection-lifecycle)`.

The validator validates heading anchors by parsing every target `.md` file's headings, building a slug set, and reporting `[BROKEN HEADING ANCHOR]` with exit code 3 if missing.

### Rule 6 — Renamed or moved documents (operational workflow)

Before running a commit that renames or moves a target file T, run the 4-step rename protocol:

1. **Stage 1 — Validate BEFORE move**:
   ```bash
   node scripts/docs/validate_relative_links.cjs docs contracts paths
   ```
   Confirm exit 0. This freezes baseline.

2. **Stage 2 — Rename file on disk** (git mv or manual) then run **AUTOFIX**:
   ```bash
   node scripts/docs/validate_relative_links.cjs docs contracts paths README.md --fix
   ```
   `--fix` does NOT repair renames (since the target file is already gone and validator cannot invent new paths). Instead, `--fix` exists only for converting legacy `file:///…` absolute links (Rule 0 violations) to relative links during the one-time migration. For ongoing rename/move work, use the helper below.

3. **Stage 3 — For rename/move work, use a companion shell script (optional but preferred)**:
   ```bash
   # Example: rename docs/backend/old.md → docs/backend/new.md, in every Markdown file of the repo:
   find . -name '*.md' -exec sed -i -E 's|(\]\(\.\./backend/)old(\.md)|\1new\2|g; s|(\]\(\./)old(\.md)|\1new\2|g; s|(\]\()old(\.md)|\1./new\2|g' {} +
   ```
   Then run validator again; any remaining broken targets are reported line-by-line with `[BROKEN TARGET]`.

4. **Stage 4 — Manual sweep & PR checklist**: Manually fix any remaining heading anchors that referenced the **old basename** inside display text (validator does not rewrite DISPLAY_TEXT per §1). Every reference to `old.md` inside `[...]` display text must be updated to `new.md` manually.

### Rule 7 — Validation requirements (hard-block in CI)

The validator [validate_relative_links.cjs](../../scripts/docs/validate_relative_links.cjs) is the single source of truth for compliance checks. It performs the following six checks **in order** on every Markdown link:

1. **Rule 0 enforcement** — Reject `file:///…` (exit code 2 accumulation if any).
2. **Root-absolute rejection** — Reject `/docs/... /contracts/... /paths/... /tests/... /scripts/...`.
3. **Forbidden schemes** — Reject `javascript:` and `data:` links.
4. **Escape detection** — If resolution navigates above `--root` (repo root by default), reject.
5. **Target existence** — file on disk must exist; else `[BROKEN TARGET]` (exit 3).
6. **Anchor validation** —
   - Numeric `#Lx-Ly`: assert end-line ≤ target file length (exit 3 if exceed).
   - Heading slug: assert in heading-slug catalogue of target file (exit 3 if absent).
   - Intra-file `#slug` anchors: compare against current file's own slugs.

**Exit codes**: 0 = all OK, 2 = absolute violations only, 3 = broken/formatted violations only, 4 = mixed. CI treats any non-zero as failure.

### Rule 8 — Display text correctness (human review, soft-warning)

The validator does NOT enforce display-text content (can't know semantic meaning), but each PR's documentation reviewer is responsible for flagging:
- Display text says "see database.md" but link targets `security.md`.
- Line ranges out of date with a recent file update (validator catches *too long* ranges but not ranges whose content has changed semantically).
- Display text is too generic such as `[here]` or `[link]` (accessibility failure WCAG 2.4.4). Display text MUST be descriptive of target identity.

---

## 3. Automated Checks (Enforcement)

### 3.1 Usage summary

See the validator file for full CLI help and per-exit-code semantics.

```bash
# validate (read-only):
node scripts/docs/validate_relative_links.cjs docs contracts paths README.md

# one-time migration helper: convert legacy absolute file:/// links to relative
# (only changes Rule 0 violations; you must still run rename/move workflow separately)
node scripts/docs/validate_relative_links.cjs docs contracts paths README.md --fix
```

### 3.2 Pre-commit hook (recommended)

Add to `.husky/pre-commit` (if using Husky):

```bash
#!/usr/bin/env bash
# docs: relative-link lint (staged md files)
set -e
STAGED_MD=$(git diff --cached --name-only --diff-filter=ACM | grep '\.md$' || true)
if [ -n "$STAGED_MD" ]; then
  node scripts/docs/validate_relative_links.cjs $STAGED_MD
fi
```

### 3.3 CI workflow job

Add this job block to `.github/workflows/docs-lint.yml` (create the workflow if missing):

```yaml
name: docs-lint
on:
  pull_request:
    paths:
      - "docs/**/*.md"
      - "contracts/**/*.yaml"
      - "paths/**/*.yaml"
      - "README.md"
      - "scripts/docs/*"
jobs:
  relative-links:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - name: Install docs deps
        run: npm --prefix . install --no-audit --no-fund
      - name: Validate relative links (full repo)
        run: node scripts/docs/validate_relative_links.cjs docs contracts paths README.md --root $GITHUB_WORKSPACE
```

---

## 4. Contributor Examples (Correct / Incorrect)

### 4.1 Same-directory sibling links — from `docs/backend/api.md` to `docs/backend/database.md`

<!-- relative-links nolint-begin -->
| Correct | Incorrect | Why Incorrect |
|---|---|---|
| `[database.md](./database.md)` | `[database.md](database.md)` | Bare sibling filename; validator flags as formatted violation |
| `[database.md §Notes](./database.md#L579-L641)` | `[database.md Notes](./database.md)` | Section cue is inside display text using wrong bracket grouping |
| — | `[db](file:///.../database.md)` | Rule 0 absolute path |
| — | `[db](../backend/database.md)` | Rule 0 root-absolute |
| — | `[here](./database.md)` | Rule 8 — generic display text WCAG 2.4.4 failure |
<!-- relative-links nolint-end -->

### 4.2 Cross-directory (2 levels up, 1 down) — from `docs/features/realtime-notifications-sse.md` to `paths/notifications.yaml#L41-L153`

<!-- relative-links nolint-begin -->
| Correct | Incorrect |
|---|---|
| `[notifications.yaml](../../paths/notifications.yaml#L41-L153)` | `[paths/notifications.yaml](../../paths/notifications.yaml#L41-L153)` — root absolute |
| — | `[notifications.yaml](./paths/notifications.yaml#L41-L153)` — wrong directory, broken target |
| — | `[notifications.yaml](../../../repo/paths/notifications.yaml)` — escapes `--root`, rejected |
<!-- relative-links nolint-end -->

### 4.3 Heading anchor — from `docs/architecture/security.md` to `docs/features/rbac-with-casbin.md` heading `## Scope and Tier Matrix`

<!-- relative-links nolint-begin -->
| Correct | Incorrect |
|---|---|
| `[rbac-with-casbin.md §Scope and Tier Matrix](../features/rbac-with-casbin.md#scope-and-tier-matrix)` | `[...](#Scope%20and%20Tier%20Matrix)` — space-encoded case-preserved not GitHub-compatible |
| — | `[...](../features/rbac-with-casbin.md#ScopeAndTier)` — invented slug, validator flags broken heading anchor |
<!-- relative-links nolint-end -->

---

## 5. Testing Process for Cross-Environment Link Resolution

Relative links must resolve correctly in five distinct deployment/environment scenarios. Validate each of them as part of the docs-lint CI job (§3.3) and, additionally, in the dedicated MkDocs/GitHub-Pages preview deploy step:

### 5.1 Deployment environments matrix

| Environment | Directory layout | How to verify links resolve |
|---|---|---|
| **(L) Local filesystem** (IDE viewer such as VS Code Markdown preview) | Repository paths unchanged; `file://` URLs rendered by VS Code using the local md-file as base URI | Open the modified doc in VS Code preview; Ctrl/Cmd-click a sample of 10 links covering same-dir, 1-up, 2-up-cross, heading-anchor, numeric line-range. 100% clicks must jump to the correct line or heading. |
| **(C) github.com blob view** (Pull-request file render) | Base URI = `https://github.com/<org>/<repo>/blob/<commit>/<path>.md` relative links resolve against `/blob/<commit>/…` plus automatic anchor handling | Run `gh pr view --web` on the PR; click a representative sample. GitHub native renderer **natively supports** `./`, `../`, `#L62-L457`, `#heading-slug` — all should work. |
| **(M) MkDocs Material static site** (docs portal) | Base URI = `/<page-url>/`; `docs/architecture/tech-stack.md` → `/architecture/tech-stack/` | A **MkDocs plugin** (`awesome-pages` plugin OR custom `hooks/relative_links.py`) must rewrite repo-relative `../backend/database.md` links to the page's URL-based equivalent `/backend/database/#l62-l457` (for numeric ranges use `pymdownx.slugs` + custom extension to map line ranges). Run local `mkdocs serve`; link-check `lychee --include-links docs/site/` (lychee 0.14+) with 0 broken. |
| **(P) Readme.com / Docusaurus / Static Export** | Varies; base URL set by docs framework | Export static site locally; run the `validate_relative_links.cjs` with `--root <export-dir>` and a custom regex passed to match `*/current-file-path/index.html`. For Docusaurus v3, rely on MDX to preserve relative Markdown links. |
| **(R) Archive/Offline — tarball of the `docs/` folder unpacked to arbitrary `/tmp/docs-copy/`** | File structure preserved, no network | Run validator with `--root /tmp/docs-copy/ docs-copy/` inside a hermetic temp dir: exit 0 proves no paths depend on absolute CI root. |

### 5.2 Automated cross-env smoke test (CI)

Add this smoke test to the docs-lint workflow to emulate environments L and R:

```bash
# Smoke 1: validation against local repo root (L)
node scripts/docs/validate_relative_links.cjs docs contracts paths README.md --root .

# Smoke 2: copy docs/ to a temp dir and re-validate R after stripping absolute links
TMPDIR=$(mktemp -d)
cp -R docs contracts paths README.md "$TMPDIR/" 2>/dev/null || true
node scripts/docs/validate_relative_links.cjs "$TMPDIR" --root "$TMPDIR"
rm -rf "$TMPDIR"
```

### 5.3 Heading-anchor migration safety

Every time a document heading is renamed, run the validator **immediately** after saving:

```bash
node scripts/docs/validate_relative_links.cjs docs
```

If any heading anchor became obsolete, the validator fails with a line-level report (`[BROKEN HEADING ANCHOR] file.md #old-slug`). Resolve either by (a) restoring the heading text OR (b) updating cross-references to the new slug — if (b) is chosen, add a short "renamed from …" note at the top of the target section to assist humans landing from external bookmarks.

---

## Versioning of this Rules Document

This specification itself is versioned by the semver in its YAML front matter (if any) or, absent front matter, by git tags. Changes to the validator behaviour in `scripts/docs/validate_relative_links.cjs` require a **corresponding update to this document**, and vice-versa: a change in rules here requires a validator update before enforcement.

Current rules version: **1.0.0** — effective date 2026-07-29.
Framework version compatible with:
- [scripts/docs/validate_relative_links.cjs](../../scripts/docs/validate_relative_links.cjs)
- [Mermaid Competency Framework](../guides/mermaid-competency-framework.md) (cross-reference uses relative format)
- Blog API contracts-first architecture
