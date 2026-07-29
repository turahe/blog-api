# Mermaid.js Competency Framework: Professional Maintainable Diagram Documentation

## Overview

This competency framework operationalizes the end-to-end skills, workflows, and deliverable standards required to design, author, embed, maintain, and govern **production-grade Mermaid.js diagrams** in real-world documentation systems. It is used by every documentation contributor on this repository (OpenAPI contract authors, backend architecture writers, feature specifiers, QA) and is enforced by the pre-commit diagram validation pipeline in `scripts/docs/`.

The framework defines **six core skill areas**, each partitioned into three proficiency bands (Beginner / Intermediate / Advanced) with concrete, demonstrable outcomes for each. A diagram author must demonstrate all the Intermediate outcomes **before** adding diagrams to any architecture, feature, or backend design document in this repository. The Advanced band reflects the level required for public-facing customer documentation, training material, and diagrams embedded in customer-delivered slides / HTML portals.

Core reference: [Mermaid.js 10.x official documentation](https://mermaid.js.org/intro/).
Companion repo artifact: [ERD.md (../backend/ERD.mdfile:///mnt/myadrive/repo/turahe/blog-api/docs/backend/ERD.md) — the canonical example of a production-grade Mermaid document in this codebase.

### Authoring checklist (all diagrams, all proficiency bands)

Every Mermaid diagram committed to this repository MUST satisfy:
- fenced code block delimiter exactly: triple-backtick + `mermaid` — zero extensions (`mmd`, `md-mermaid` are forbidden for GitHub/MkDocs/GitLab compatibility).
- One explicit diagram type declaration as the very first line inside the fence: `flowchart LR` (never `graph LR` legacy), `sequenceDiagram`, `erDiagram`, `gantt`, `classDiagram`, `architecture-beta`, `stateDiagram-v2`, `mindmap`, `pie`, `journey`, `requirementDiagram`, `gitGraph`, `C4Context` / `C4Container` / `C4Component`.
- Maximum diagram size: 140 nodes / arrows combined; split diagrams exceeding this into two or more `%% @section`-annotated sub-diagrams (see §Advanced: Large-scale rendering).
- Every diagram block preceded by one plain-English **prose paragraph** describing what the diagram shows and how to read it; every diagram block followed by one plain-English **callouts paragraph** listing the decisions or caveats visible in the diagram but not labelled (this guarantees WCAG 2.1 AA "alternative for non-text content" for readers on pure-text feeds).
- Zero pixel-perfect absolute positioning; Mermaid layout is fully auto-directed. Brand alignment is applied via `init` directives; never inline `fill:` hex overrides inside nodes (see §3 Custom themes).

---

## Skill Area 1 — Core Mermaid.js Fundamentals & Syntax

Goal: Author correct, readable, semantically-named diagrams for every documentation-relevant diagram type.

### 1.1 Flowcharts (`flowchart TB/LR/BT/RL/TD`)

- **Beginner — can write linear flowcharts for single-process descriptions.**
  - Use `start` → `input/output` → `decision` → `process` → `end` node shapes with parentheses-style declarations `id([text])`, `id[/text/]`, `id{text}`, `id[text]`, `id([end])`.
  - Use `-- text -->` arrow labels and `===` thick arrows for critical-path emphasis.
  - Group related nodes inside `subgraph GroupName ... end` for blog-api controller → service → repository handoff diagrams.
  - _Use case_: authentication login happy-path flowchart in [features/authentication.md](../features/authentication.md) (password argon2id verify → session creation → CSRF mint → http-only cookie set).
- **Intermediate — can author nested flowcharts with sub-routines, parallel lanes, and hyperlinks.**
  - Use `subgraph` nesting with `direction LR` inside a TB parent; `linkStyle 0..N stroke-width:2px,stroke:#e11d48` for error lanes.
  - Use `click nodeId "/docs/backend/api.md#login" _blank` to link nodes to companion docs (GitHub/MkDocs render these as hyperlinks).
  - Apply `classDef` style classes to consistently color code success/error/warning/migration paths and reuse across the whole diagram.
  - _Use case_: RBAC policy mutation flowchart in [features/rbac-with-casbin.md](../features/rbac-with-casbin.md) (validate policy → transactional Casbin upsert + readable mirror update → Watermill audit outbox emit → admin UI invalidate policy cache).
- **Advanced — can author multi-page flowcharts with sub-graph drill-down, cross-diagram ER alignment, and CI-validated node IDs.**
  - Use `%% @anchor node_id` comment anchors for tests that assert specific nodes exist; pair with `scripts/docs/validate_mermaid.cjs` to block PRs that accidentally rename contract nodes.
  - Use `flowchart-v2` directives (`%%{init: {"flowchart": {"useMaxWidth": true, "htmlLabels": false}} }%%`) to prevent SVG layout overflow on narrow viewports.
  - Implement `%% @include ./fragment-login-flow.mmd` convention processed by `scripts/docs/bundle_mermaid.cjs` so large flowcharts are edited as composable fragments (e.g. password-reset, email-change, oauth-login sub-flows live in separate files, bundled only for render by MkDocs/Confluence export, stored as fragments in PRs).
  - _Use case_: post-publish end-to-end flow spanning media transcoding, SEO rendering, notification fanout, cache invalidation, analytics event streams, with each sub-flows editable by separate domain owners.

### 1.2 Sequence diagrams (`sequenceDiagram`)

- **Beginner — happy-path request/response with 4 participants.**
  - `participant Actor as DisplayName`; `->>` synchronous, `-->>` response, `->x` error/close, `Note over A,B: note`.
  - `alt condition / else / end`, `opt condition / end`, `loop N times / end`.
  - _Use case_: comment submission sequence in [features/comments-and-moderation.md](../features/comments-and-moderation.md) (user → api → spam filter → db insert → notification emit → response 201).
- **Intermediate — parallel lanes, box grouping, participant activation stacking, message sequence numbers.**
  - `box rgb(239,68,68,0.08) Moderation services end` visual grouping; `autonumber` on; `activate/deactivate` stacks; `par lane1 / and lane2 / end` for Redis cache + Watermill publish + notification fan-out in parallel.
  - `rect rgb(...) ... end` highlight boxes for SLO-critical sections (comment P95 create < 180ms); `Note right of Filter: checks regex + NSFW ML model + Akismet` implementation notes.
  - _Use case_: SSE notification fanout sequence in [features/realtime-notifications-sse.md](../features/realtime-notifications-sse.md) (Post → Watermill outbox → Redis pub/sub broadcast → 3 parallel hub processes → 5 open SSE streams each → p95 broadcast < 90ms + fanout.buffer_full on slow consumer).
- **Advanced — cross-cutting concern interleaving, retries/timeouts, chaos scenarios rendered as explicit lifeline fragments.**
  - `critical timeout 150ms ... option ... end` for idempotent retry windows; `break when rate_limit_exceeded then error ... end` for non-recoverable branches; `loop every 15s watchdog ... end` for ping heartbeats.
  - Use `participant Prometheus as 📊 Prometheus` emoji-aliased participants; link activation spans to SLO error budgets via callouts paragraph after the diagram; CI script asserts `autonumber` enabled for every sequence diagram over 8 messages (critical for RCA on postmortems).
  - _Use case_: outbox-poll transactional worker with 2 retries + 503 backpressure + Prometheus histogram publish + dead-letter queue branch; chaos-injectable missing-ACK fragment visible inline.

### 1.3 Entity Relationship Diagrams (`erDiagram`)

- **Beginner — 5–10 tables with correct cardinality arrows.**
  - Syntax: `TABLE1 { TYPE COLUMN "PK|FK|UK|note" }` then `TABLE1 ||--o{ TABLE2 : "has"` with `||/|o/o|}o` cardinalities exactly matching the legend in [ERD.md §1.3](../backend/ERD.md#L30-L41).
  - _Use case_: Comments module tables-only ERD slice (comments + comment_flags + comment_reactions) embedded inside the comments feature doc.
- **Intermediate — 20+ tables, role-grouped boxes, MV sidecars, per-column type notes aligned to PostgreSQL types.**
  - Explicit column type tokens that match §1.4 legend (`uuid`, `bigserial`, `varchar`, `text`, `jsonb`, `timestamptz`, `bool`, `char(2)`, `inet`, `bytea`); label PK/FK/UK inside quoted column notes; distinguish materialized views with `user_activity_daily { ... }` + callout paragraph MV marker.
  - Group tables inside `subgraph` boxes (Mermaid ER supports this via `%% @subgraph` convention, or use the native `erDiagram` `TITLE + ENTITIES` grouping pattern) by domain.
  - _Use case_: the canonical blog-api 8-domain ERD in [ERD.md](../backend/ERD.md) itself, 60+ entity/tables with append-only callouts and exact column types cross-referenced to backend/database.md field catalogue.
- **Advanced — schema-version comparison diagrams, migration-safe ER diff views, CI-validated column names.**
  - Produce `ERD-v12-to-v13.md` erDiagram with 3-colour node `classDef`s: `ADDED rgb(22,163,74,0.12)`, `REMOVED rgb(220,38,38,0.12)`, `CHANGED rgb(245,158,11,0.12)`; pair with migration list in migrations/README.md.
  - Run `scripts/docs/validate_mermaid_erd.py` against `migrations/*.sql` to confirm every column named inside the ERD actually exists in the most recent HEAD schema and every declared FK matches real `REFERENCES` clauses; CI fails on drift.
  - _Use case_: v12 → v13 migration ER delta introducing `newsletter_subscriptions` + double opt-in `subscriber_confirmations` with all added/changed/removed columns explicitly marked.

### 1.4 Gantt charts (`gantt`)

- **Beginner — linear 8–12 milestone release Gantt.**
  - `title`, `dateFormat YYYY-MM-DD`, `axisFormat %b %d`, `section GroupName`, `taskname : a1, 2026-08-01, 7d`; `crit` marker on path-critical tasks; `after a1` dependency chain.
  - _Use case_: quarterly Q3 roadmap mini-Gantt inside [product/roadmap.md](../product/roadmap.md) (SSE feature 14d → RBAC admin UI 10d → comments moderation ML 14d → analytics dashboard GA 12d).
- **Intermediate — swimlane Gantt with parallel workstreams, milestone markers, dependency gates.**
  - Explicit `Milestone : milestone, m1, 2026-08-12, 1d` zero-width dots; `crit` only for ≤ 5% of bars (release gates, SLA deadlines, compliance freeze); 3 parallel sections Frontend / Backend / QA with `after` links crossing sections.
  - _Use case_: post-launch v1.0 GA 12-week rollout Gantt: Infra private IP + VPC peering → Database migrations + perf test → Backend SSE + comment moderation → Frontend SSR/ISR migration → A11y WCAG audit → GA flag-flip milestone.
- **Advanced — effort-loaded Gantts with person-assignments, Slack-anniversary milestone tracking, capacity-capped rolling windows.**
  - Encode per-task owner via `assignee` taskname prefix convention parsed by `scripts/docs/gantt_extract.py` to export iCal/Google Calendar invites; add `%% @capacity team_frontend 40h/week` capacity directives validated for over-allocation on merge.
  - _Use case_: cross-team quarterly roadmap with capacity enforcement, weekly capacity roll-up, automatic CI rejection when any assignee exceeds 120% allocation for 3+ contiguous weeks.

### 1.5 Class diagrams (`classDiagram`)

- **Beginner — 4–8 classes with 3 relationships, attribute visibility markers.**
  - `+ public`, `- private`, `# protected` markers; `Class1 <|-- Class2` inheritance; `Class1 --> Class2` association; `Class1 o-- Class2` aggregation; `Class1 *-- Class2` composition.
  - _Use case_: domain aggregates for posts module in [backend/services.md](../backend/services.md): `Post`, `PostSEO`, `PostRevision`, `Tag`, `PostTagPivot` with `Post 1 *-- 0..* PostRevision` composition.
- **Intermediate — interface/implementation splits, generic typing arrows, class-level annotation stereotypes.**
  - `<<interface>> Repository<T>`, `<<service>>`, `<<aggregate_root>>` stereotypes via `note for Post "aggregate_root\\nUUID v7 PK"`; templated `Repository~T~` generics.
  - _Use case_: hexagonal persistence ports & adapters diagram from [architecture.md](../architecture/architecture.md): `UserRepository` interface with `FindByEmail()`, `PostgresUserRepository` implements it, `MemoryUserRepository` test-double, `UserService` depends on interface only.
- **Advanced — cross-package class diagrams with visibility & dependency-change validation in CI.**
  - Encode package/group via `package blog_api.platform.auth { ... }` nested blocks; `scripts/docs/validate_class_diagram.py` cross-checks against `internal/` package layout and fails PRs when a `public +` method listed in the class diagram is absent in Go source, or vice-versa.
  - _Use case_: full hexagonal class diagram for auth, posts, media, notification modules with enforced interface/adapter separation, auto-sync with real Go interface signatures.

### 1.6 Architecture diagrams (`C4Context`, `C4Container`, `C4Component`, `flowchart` with L1..L3 box nesting; beta `architecture-beta`)

- **Beginner — L1 C4 System Context: 3-4 boxes, 5 relationships.**
  - Person(User), Person(Admin), System(BlogAPI), SystemExt(Cloudflare R2), BiRel arrows matching [architecture/architecture.md](../architecture/architecture.md) L1 description.
- **Intermediate — L2 C4 Container + L3 C4 Component with legend, external system shading.**
  - `Container(BlogAPI, "Go Gin server", "HTTP + SSE streams")`, `ContainerDb(Postgres, "Cloud SQL Postgres 15", "source of truth")`, `ContainerQueue(Redis, "Redis Cluster", "cache + SSE fanout bus")`; explicit relationships with protocol labels `HTTPS/2`, `mTLS IAM DB auth`, `pub/sub Redis` matching cloudsqlconn stack in [backend/database.md L62-L457](../backend/database.md#L62-L457).
  - _Use case_: L2 container architecture diagram referenced from deployment.md showing Cloud Run service ↔ Cloud SQL private IP ↔ VPC connector ↔ Redis Memorystore ↔ Cloudflare R2 ↔ SES ↔ WAF.
- **Advanced — L4 C4 Code + multi-region failover architecture with live data-plane annotations.**
  - `C4Component(myContainer, "SSE Hub", "pkg/sse/hub.go")` with per-component SLO labels; use `%% @metric sse_hub_connected_streams 342` live-metric comment annotations parsed for Grafana annotation panels.
  - _Use case_: multi-region active/passive disaster-recovery architecture with regional replication arrows, Cloud SQL cross-region replica, Redis cross-region read replica, failover Gantt + decision-tree companion diagram; cross-reference with observability.md SLO budget.

---

## Skill Area 2 — Integration: Embed Mermaid Diagrams Natively Across Documentation Platforms

### 2.1 Platform support matrix (recommended configurations)

| Platform | Mermaid renderer | Enablement step | Supported diagram types | Known limitations |
|---|---|---|---|---|
| GitHub README / .md files (repo docs) | Built-in Mermaid 10.x (server-rendered SVG then cached) | zero-config, just ```mermaid fences | All stable types (flow, seq, ER, Gantt, class, state, pie, mindmap, journey, requirement, gitGraph, C4) | no `init` directives that set CSP-unsafe `securityLevel: 'loose'`; no `click` node links to `javascript:` URLs; architecture-beta not yet stable |
| GitHub Pages / MkDocs Material + `mkdocs-material` 9.x | `mermaid2` plugin + client-side Mermaid 10.9 loader | `mkdocs.yml` → `markdown_extensions: - pymdownx.superfences: custom_fences: [{name: mermaid, class: mermaid, format: !!python/name:pymdownx.superfences.fence_code_format}]` + load `<script src="https://cdn.jsdelivr.net/npm/mermaid@10.9/dist/mermaid.min.js">` in `overrides/main.html` | All stable + C4 + architecture-beta | must set `mermaid.startOnLoad = true`; `init` directive supported per-diagram; avoid SVG filters for PDFs |
| GitLab Flavored Markdown (Wikis / Snippets / MR descriptions) | Built-in Mermaid 10.x | zero-config | Same as GitHub | No per-page `init` global; use inline `%%{init: {}}%%` directive per-fence |
| Confluence Cloud / Data Center 8.x | Mermaid Charts & Diagrams for Confluence (Marketplace app — Bob Swift / draw.io-embedded Mermaid) or AppFire Diagrams | Install marketplace app; enable ```` ```mermaid ```` code fence macro | All stable | Serverless Confluence enforces 50 KB per diagram; split large ERDs |
| Notion.so (native Mermaid as of 2024) | Insert → Code block → set language = Mermaid | code block language picker = Mermaid | Stable subset: flow, seq, class, ER, Gantt, state, pie, mindmap, gitGraph | No `init` directives, no click handlers, no C4 context natively; export C4 to PNG then embed separately |
| Docusaurus v3 | `@docusaurus/theme-mermaid` + remark plugin | `docusaurus.config.js themes: ["@docusaurus/theme-mermaid"], markdown: { mermaid: true }` | Full Mermaid 10.x | Requires MDX v3 (Docusaurus v3+) |
| Readme.com / Sphinx / Slate / GitBook | Mermaid plugins (see platform docs) | Install plugin; CDN loader | Stable subset | GitBook native Mermaid support launched 2024; Sphinx uses `sphinxcontrib-mermaid` with mmdc CLI pre-render |

### 2.2 Skill proficiency bands

- **Beginner — GitHub & MkDocs native embedding.**
  - Produce ```` ```mermaid ```` fenced blocks inside any repo Markdown; verify rendering on `github.com/<org>/<repo>/blob/<branch>/file.md` preview before raising PR.
  - Write a MkDocs Material `docs/architecture/architecture.md` file with inline Mermaid fences; confirm `mkdocs serve` local render matches GitHub preview.
- **Intermediate — multi-platform portable source: same Mermaid source renders on GitHub, MkDocs, Confluence, GitLab.**
  - **Convention**: only use directives declared inside `%%{init: ... }%%` first-line of fence (never global `mermaid.initialize()`; per-page global init causes GitHub/GitLab/Notion drift); avoid `htmlLabels:true` for Confluence/Data Center compatibility; C4 diagrams use the official `C4Context/C4Container` Mermaid keywords — NOT external PlantUML imports.
  - Script: `scripts/docs/mermaid_export.mjs` uses `@mermaid-js/mermaid-cli` (headless Puppeteer) to batch-export every ```` ```mermaid ```` block from `docs/**/*.md` into `docs-assets/diagrams/<file>-<seq>.svg` + `.png` for Confluence/Notion/Slide imports; keeps vector source authoritative, raster export an artifact.
  - _Use case_: same ERD source renders on GitHub, MkDocs public docs site, Confluence architecture space, Notion launch doc — no drift, one source of truth.
- **Advanced — versioned diagram lifecycle in CI, CDN-powered theme switching for docs portal, offline bundle support.**
  - CI: `scripts/docs/validate_mermaid.cjs` parses every Mermaid block with `@mermaid-js/parser` (no browser) to assert syntax validity before merge; outputs block SHA-256 to `docs-assets/diagrams/_manifest.json` so downstream consumers (Confluence sync job, training slides) can detect when a diagram source has changed and re-export.
  - Docs portal: MkDocs Material override swaps the per-fence `init` theme based on `prefers-color-scheme` via `MutationObserver` watching `<body data-md-color-scheme>`; exports dark/light paired SVGs; brand palette sync with `docs/guides/branding-palette.md` tokens.
  - Offline: include `mermaid.min.js` self-hosted under `docs/assets/vendor/` with SRI hash; PDF export pipeline runs `mmdc -- puppeteerConfig offline-no-fonts.json` to produce WCAG-tagged accessible PDFs.

---

## Skill Area 3 — Implementation Setup, Theming & Responsive Design

### 3.1 Static HTML page setup (zero-framework)

Three required files for a standalone documentation micro-site that includes Mermaid rendering with brand-aligned responsive diagrams.

#### 3.1.1 HTML skeleton: `docs-assets/mermaid-minimal.html`

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover" />
  <title>blog-api Architecture Diagrams</title>
  <meta name="color-scheme" content="light dark" />
  <link rel="preconnect" href="https://cdn.jsdelivr.net" />
  <style>
    /* 1. Brand tokens — keep in sync with docs/guides/branding-palette.md */
    :root {
      --brand-primary:   #2563eb; /* blue-600 */
      --brand-primary-2: #1d4ed8; /* blue-700 */
      --brand-success:   #16a34a;
      --brand-warning:   #d97706;
      --brand-danger:    #dc2626;
      --brand-surface:   #ffffff;
      --brand-text:      #0b1220;
      --brand-line:      #d5dbe7;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --brand-surface: #0a0e1a;
        --brand-text:    #e5ecfa;
        --brand-line:    #243049;
      }
    }
    /* 2. Responsive wrapper: Mermaid auto-zooms, never scrolls horizontal on mobile. */
    body { margin: 0 5vw 10vh; font-family: Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto; color: var(--brand-text); background: var(--brand-surface); }
    article { max-width: 1120px; margin: 3rem auto; }
    pre.mermaid {
      background: transparent;
      padding: 1.25rem 0.5rem 2rem;
      overflow: hidden;       /* hide overflow; Mermaid scales internally */
      border: 1px solid var(--brand-line);
      border-radius: 12px;
    }
    pre.mermaid svg { max-width: 100%; height: auto; display: block; margin: 0 auto; }
    @media (max-width: 720px) {
      pre.mermaid { padding: 0.5rem 0.1rem 1rem; border-radius: 8px; }
      article   { margin: 1.5rem auto; }
      h1        { font-size: 1.35rem; }
    }
    /* 3. Print layout (PDF export) — diagrams on their own pages, no cut-off. */
    @media print {
      article { max-width: 100%; }
      pre.mermaid { page-break-inside: avoid; break-inside: avoid; border: none; }
    }
  </style>
</head>
<body>
  <article>
    <h1>blog-api system architecture (L2 container view)</h1>
    <p class="mermaid-alt">The diagram below models the production deployment of the blog-api on Google Cloud Run with Cloud SQL private IP, Redis Memorystore, Cloudflare R2 object storage, and Cloudflare WAF. Read left-to-right: users → edge → app services → data plane.</p>
<pre class="mermaid">
%%{init: {
  "theme": "base",
  "themeVariables": {
    "primaryColor":        "#eff6ff",
    "primaryTextColor":    "#0b1220",
    "primaryBorderColor":  "#2563eb",
    "lineColor":           "#475569",
    "secondaryColor":      "#f8fafc",
    "tertiaryColor":       "#eef2ff",
    "fontFamily":          "Inter, ui-sans-serif, system-ui, sans-serif",
    "fontSize":            "13px",
    "noteBkgColor":        "#fff7ed",
    "noteBorderColor":     "#d97706",
    "actorBkg":            "#eef2ff",
    "actorBorder":         "#2563eb",
    "activationBorderColor":"#1d4ed8",
    "activationBkgColor":  "#dbeafe"
  },
  "flowchart": { "useMaxWidth": true, "htmlLabels": false, "curve": "basis" },
  "sequence":  { "showSequenceNumbers": true, "mirrorActors": true }
}}%%
flowchart LR
  User([End user]) --> CF[Cloudflare WAF / CDN]
  Admin([Admin editor]) --> CF
  CF --> CR[Cloud Run service\n(Gin + SSE hub)]
  CR -->|mTLS IAM DB auth\nPrivate Service Connect| CS[(Cloud SQL\nPostgres 15+)]
  CR -->|Redis pub/sub + cache| RM[(Redis Memorystore\nprivate IP)]
  CR -->|PUT/GET signed URL| R2[(Cloudflare R2\nobject storage)]
  CR -->|Transactional outbox| WM[Watermill workers]
  WM -->|Email SES| SES[(AWS SES)]
  WM -->|Fan-out SSE replay ring| RM
  classDef default fill:#eff6ff,stroke:#2563eb,stroke-width:1.5px;
  classDef data fill:#ecfdf5,stroke:#16a34a,stroke-width:1.5px;
  classDef worker fill:#fff7ed,stroke:#d97706,stroke-width:1.5px;
  classDef edge fill:#fafafa,stroke:#475569,stroke-width:1.5px;
  class CF,User,Admin edge;
  class CS,RM,R2 data;
  class WM,SES worker;
</pre>
    <p><em>Callouts:</em> (1) the Cloud Run → Cloud SQL link uses private IP only, enforced by GCP org policy <code>constraints/sql.restrictPublicIp</code> documented in <a href="../backend/database.md#L232-L245">database.md §Public vs Private IP</a>. (2) Watermill SSE fan-out is bridged via Redis pub/sub to multi-process hubs per <a href="../features/realtime-notifications-sse.md">realtime-notifications-sse.md</a>.</p>
  </article>
  <!-- Mermaid loader — SRI-verified CDN pin; load after DOM so <pre class="mermaid"> is parsed -->
  <script type="module">
    const SRI = 'sha384-Zl/rzYjPOfH07V+Xs2R49cFxvR3Bz+u+oU4m3tXq0s3W1v2n3O2k+h3aP5w6B4XK'; // replace with real pin
    await import('https://cdn.jsdelivr.net/npm/mermaid@10.9.0/dist/mermaid.esm.min.mjs')
      .then(({ default: mermaid }) => {
        mermaid.initialize({
          startOnLoad: true,
          securityLevel: 'strict',     // NEVER 'loose' in public docs (no SVG <script>, no click javascript:)
          fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif',
          altFontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif',
          theme: 'base', themeVariables: {
            // mirror CSS custom props via JS for SSR-exported SVGs
            primaryColor:       getComputedStyle(document.documentElement).getPropertyValue('--brand-surface').trim() || '#eff6ff',
            primaryBorderColor: '#2563eb',
            lineColor:          '#475569'
          }
        });
      });
  </script>
  <!-- Accessibility enhancement: Mermaid's auto-generated alt comes from alt-text above; we add an aria-labelledby to each SVG for screen readers. -->
  <script type="module" defer>
    document.querySelectorAll('pre.mermaid').forEach(pre => {
      const prev = pre.previousElementSibling;
      if (prev && prev.classList.contains('mermaid-alt')) {
        pre.setAttribute('role', 'img');
        pre.setAttribute('aria-labelledby', 'alt-' + Math.random().toString(36).slice(2, 9));
        prev.id = pre.getAttribute('aria-labelledby');
      }
    });
  </script>
</body>
</html>
```

#### 3.1.2 Mermaid CLI (offline export & CI validation): `package.json` one-shot setup

This repository already pins `@mermaid-js/mermaid-cli` as a docs-only devDependency for batch export + headless validation.

```json
{
  "name": "blog-api-docs",
  "private": true,
  "type": "module",
  "devDependencies": {
    "@mermaid-js/mermaid-cli": "^10.9.0",
    "@mermaid-js/parser":     "^10.9.0",
    "commander":              "^12.0.0",
    "glob":                   "^10.3.0",
    "remark":                 "^15.0.0",
    "remark-gfm":             "^4.0.0",
    "sharp":                  "^0.33.0"
  },
  "scripts": {
    "docs:mermaid:validate":   "node scripts/docs/validate_mermaid.cjs docs/**/*.md",
    "docs:mermaid:export":     "mmdc --input docs/backend/ERD.md --output docs-assets/diagrams/erd.svg --outputFormat svg   --puppeteerConfig scripts/docs/puppeteer-config.json --configFile docs/guides/mermaid-theme.json",
    "docs:mermaid:export:png": "mmdc --input docs/backend/ERD.md --output docs-assets/diagrams/erd.png --outputFormat png  --puppeteerConfig scripts/docs/puppeteer-config.json --configFile docs/guides/mermaid-theme.json --width 2400"
  }
}
```

#### 3.1.3 Shared brand theme config: `docs/guides/mermaid-theme.json`

Centralises the blog-api brand palette for every diagram export, so `mmdc` renders and live-browser renders produce identical output. Update this file **once** when the brand is refreshed — every diagram across every doc updates automatically.

```json
{
  "$schema": "https://cdn.jsdelivr.net/npm/mermaid@10.9.0/dist/mermaid.schema.json",
  "theme": "base",
  "themeVariables": {
    "primaryColor":        "#eff6ff",
    "primaryTextColor":    "#0b1220",
    "primaryBorderColor":  "#2563eb",
    "lineColor":           "#475569",
    "secondaryColor":      "#f8fafc",
    "tertiaryColor":       "#eef2ff",
    "successBkgColor":     "#dcfce7",
    "successBorderColor":  "#16a34a",
    "warningBkgColor":     "#fff7ed",
    "warningBorderColor":  "#d97706",
    "dangerBkgColor":      "#fee2e2",
    "dangerBorderColor":   "#dc2626",
    "fontFamily":          "Inter, ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
    "fontSize":            "13px",
    "noteBkgColor":        "#fff7ed",
    "noteBorderColor":     "#d97706",
    "sequenceNumberColor": "#ffffff"
  },
  "flowchart": {
    "useMaxWidth": true,
    "htmlLabels":  false,
    "curve":       "basis",
    "nodeSpacing": 40,
    "rankSpacing": 55,
    "padding":     10
  },
  "sequence": {
    "showSequenceNumbers": true,
    "mirrorActors":        true,
    "wrap":                true,
    "width":               140,
    "height":               40,
    "messageMargin":        10
  },
  "er": {
    "layoutDirection":     "LR",
    "entityPadding":       12
  },
  "gantt": {
    "topPadding":          30,
    "gridLineStartPadding": 25,
    "fontSize":             12,
    "sectionFontSize":      14
  },
  "class": {
    "useMaxWidth": true
  },
  "securityLevel": "strict"
}
```

### 3.2 Responsive design rules (all diagrams ≥ Intermediate band)

Apply these six conventions in every Mermaid diagram authored for the repo:

1. **`flowchart` direction**: Large diagrams default `TB`; only `LR` if ≤ 8 nodes (LR overflows narrow viewports).
2. **`%%{init: {"flowchart":{"useMaxWidth":true, "htmlLabels":false}} }%%`** directive on every flowchart; `useMaxWidth` guarantees the SVG scales to container; `htmlLabels:false` avoids foreignObject browser compatibility bugs in Safari 15.
3. **Node text max 6 words per line**: Wrap node text with `\n` hard line-breaks. Any node exceeding 55 characters on any single line triggers a lint warning in CI.
4. **Mobile previews validated**: `mmdc --width 390 --outputFormat png` + `sharp` to generate 390px (iPhone 15 portrait) previews; CI compares pixel width of widest node against diagram width and warns if any node exceeds 95% of viewport.
5. **Breakpoints for large diagrams**: If > 12 nodes, produce a narrow-screen `@media` variant as a second `flowchart TB` simplified diagram with collapsed sub-graphs; link between them with `Note: for wide-screen complete view, scroll or open the diagram in full-screen.`
6. **Print-safe**: Always set `break-inside: avoid` via CSS on `<pre class="mermaid">`; avoid page break inside diagram; C4/ER diagrams > 1 page must be split into 2 logical fragments.

### 3.3 Skill proficiency bands

- **Beginner**: Use the 3.1.1 HTML skeleton + 3.1.3 theme JSON to embed diagrams on a static page; set `securityLevel:strict`; verify rendering in Chrome + Safari.
- **Intermediate**: Enforce responsive rules 1–6; produce paired SVG/PNG exports with `mmdc`; ensure brand palette matches `branding-palette.md`.
- **Advanced**: Implement `prefers-color-scheme` theme swap in MkDocs/Docusaurus override; WCAG-tag PDFs with correct reading order; CI-enforce node-width rules in pre-commit.

---

## Skill Area 4 — Advanced Skills: Interactive Diagrams, Performance, Rendering Bugs

### 4.1 Interactive diagrams with click events + tooltips (Advanced band only; `securityLevel:strict` compatible)

#### 4.1.1 Click events (documentation links only — NEVER `javascript:`)

```mermaid
%%{init:{"securityLevel":"strict"}}%%
flowchart LR
  A[User submits comment] --> B{Spam?};
  B -- No --> C[Insert comment row];
  B -- Yes --> D[Flag for moderation];
  C --> E([SSE notify post author]);
  click A "https://github.com/turahe/blog-api/blob/main/docs/features/comments-and-moderation.md#create-comment" _blank;
  click B "https://github.com/turahe/blog-api/blob/main/docs/features/comments-and-moderation.md#spam-pipeline"   _blank;
  click C "https://github.com/turahe/blog-api/blob/main/docs/backend/database.md#comments-table"                   _blank;
  click E "https://github.com/turahe/blog-api/blob/main/docs/features/realtime-notifications-sse.md"               _blank;
```

#### 4.1.2 Tooltips (native Mermaid `tooltip` directive)

```
flowchart TB
  DB[(Cloud SQL\nPostgres 15)]
  tooltip DB "Private Service Connect private IP only. IAM Database Authentication, mTLS cert auto-rotation 1h. See backend/database.md §Private IP.";
  DR[cloudsqlconn Dialer]
  tooltip DR "cloud.google.com/go/cloudsqlconn. WithPrivateIP()+WithIAMAuthN(). ConnMaxLifetime 1800s.";
```

#### 4.1.3 Pan + zoom on standalone pages (docs portal only, not GitHub READMEs)

Wrap Mermaid SVG in `svg-pan-zoom` wrapper (load `@svgdotjs/svg.panzoom.js` or `svg-pan-zoom@3.6`) and add zoom-in / zoom-out / reset buttons in the top-right corner of the diagram container.

### 4.2 Rendering performance for large documentation projects

Problem scale: 400+ Markdown docs, 1,200+ Mermaid blocks, 60k page-views/month docs portal.

Required optimizations (Advanced band):
- **Lazy-render diagrams below the fold** using IntersectionObserver; `mermaid.run({ querySelector: 'pre.mermaid.in-view' })` only when the `<pre>` enters the viewport.
- **SSR the diagram SVGs at build time** using `mmdc -- puppeteer headless` in MkDocs/Docusaurus build hook so client browser never runs Mermaid JS; this cuts ~150 ms/block and removes CLS. HTML preview ships inline `<svg>` data; `mermaid.min.js` is loaded only for `click` tooltips.
- **Shared SVG defs**: Post-process 30+ diagrams on a docs page to hoist duplicate `<marker end>` / `<style>` defs to a single page-level `<defs>` with `scripts/docs/svg-dedup.py`; cuts shipped SVG bytes by 25–35% on large pages.
- **CDN caching with etags**: Every build-exported `<file>-<seq>-<sha256prefix>.svg` filename embeds its source content hash, so Cloudflare caches diagrams for 1 year.
- **Diagram split convention**: `%% @max-nodes 80` header comment on every block; pre-commit `mermaid-lint` auto-splits blocks > 140 nodes into 2 fragments with a shared `@id` so they can be composed visually.

### 4.3 Troubleshooting common rendering errors

Troubleshooting guide for CI/doc-portal renders. Every error code below has an automated diagnostic in `scripts/docs/validate_mermaid.cjs`:

| Error code / symptom | Root cause | Fix | Verification command |
|---|---|---|---|
| `Parse error on line 1: extraneous input '…' expecting {…}` | Typo in diagram declaration line (e.g. `flow chart` two words, `sequencediagram` lowercase d, `erdiagram` lowercase D). | First line must match regex: `^(flowchart|sequenceDiagram|erDiagram|gantt|classDiagram|stateDiagram-v2|mindmap|pie|journey|requirementDiagram|gitGraph|architecture-beta|C4Context|C4Container|C4Component|C4Code)[\s(].*$` | `node scripts/docs/validate_mermaid.cjs my-diag.md` |
| GitHub renders blank box / "Sorry, we cannot display this diagram" | 1) Mermaid CLI version ahead of GitHub's server renderer 2) `securityLevel:loose` with inline SVG scripts 3) `init` directive with unknown keys 4) Node count > GitHub's renderer limit (~300 nodes) | 1) Pin features to GitHub Mermaid release notes 2) Always strict 3) Validate `init` against mermaid.schema.json 4) Split blocks < 140 nodes | `docker run -v $PWD:/docs minlag/mermaid-cli:10.9.0 mmdc -i /docs/x.md -o /tmp/x.svg 2>&1` |
| `Maximum call stack size exceeded` during layout | Nested subgraphs > 8 deep OR flowchart cyclic dependency back-edges + `elk` renderer | Flatten nested subgraphs to ≤ 4; add `%%{init: {"flowchart": {"ranker": "longest-path","cycleRemoval": "greedy"}}}%%`; switch ERD back to default layout | Enable `mermaid.parseError` handler in browser console; stack mentions `layout`/`dagre` → too deep. |
| Cut-off text on Safari / iOS / small screens | `htmlLabels: true` with long `foreignObject` text + overflow hidden | Always `htmlLabels:false` + wrap text with `\n` + 6 word/node rule (§3.2 rule 3). | mmdc iPhone 390px export + pixel inspection. |
| C4Context diagrams missing person/actor icons | Using old `%%{init: {"theme": "neutral"}}%%` with custom `Person` macro removed in 10.x | Use native `C4Person / Person(name, desc)` keywords (C4 plugin v2). Load `@mermaid-js/layout-elk` optional layout plugin if required. | Check `window.mermaid.c4` object exists on docs portal. |
| ER diagram line overlap / FK labels unreadable | 20+ tables in a single `erDiagram` | Use `%%{init:{"er":{"layoutDirection":"TB"}}}%%` instead of LR; reduce `entityPadding`; split into per-module ERDs; use `C4Component` instead of erDiagram for architecture-level relationships. | Compare mmdc SVG exports TB vs LR. |
| Notion / Confluence diagram shows "Mermaid syntax error" popup even though GitHub renders fine | 1) `init` directive has escaped quotes 2) Tab characters (Notion/Confluence parsers break on tabs) 3) CRLF line endings + multi-line strings inside nodes | Replace tabs with spaces; normalize line endings LF; avoid multi-line string literals inside node brackets — use `\n`. | `file docs/features/my.md`; `cat -A docs/features/my.md \| head -40` for tabs. |
| PDF / print export clips diagram bottom edge | SVG overflow: hidden on the root `<svg>` + browser print A4 margin | Always `mmdc -- puppeteerConfig {"defaultViewport":{"height":0}}` + CSS `@page { margin: 1.5cm; size: A4; }`; split > 1 page diagrams. | `wkhtmltopdf /tmp/exported.html /tmp/exported.pdf && pdfinfo /tmp/exported.pdf`. |
| Gantt bars overflow off the right edge | More than 30 bars + `dateFormat YYYY-MM-DD HH:mm` with 1-hour tasks | Split Gantt into weekly sub-views; set `excludes weekends`; `axisFormat %b %d` only, no HH:mm on monthly views. | mmdc export PNG at 2400px width. |
| GitGraph merge commits rendered as straight lines `|` instead of `\\|/` branch crosses | Legacy `gitGraph` (v1) syntax; newlines inside message strings | Use `gitGraph` with explicit `branch dev` → `commit id:"msg"` → `merge main tag:"v1.0"`; keep message ≤ 40 chars. | `@mermaid-js/parser@10.9.0 parse` validation. |
| Dark mode diagram contrast ratio < 4.5:1 (WCAG failure) | Light theme text `#0b1220` used unmodified on dark surfaces | Add `@media (prefers-color-scheme: dark) themeVariables {}` override with `primaryTextColor:"#e5ecfa"` and light stroke colors. | `axe-core` browser automation; `scripts/docs/a11y_contrast.mjs` batch-test every exported SVG. |

### 4.4 Skill proficiency bands

- **Beginner**: Use 4.3 table to resolve the 3 most common errors (parse error first line, blank GitHub box, Safari cutoff). Fix ~ 5 diagrams from past PRs.
- **Intermediate**: Implement lazy render with IntersectionObserver for docs portal; apply brand theme + responsive rules; add `@max-nodes 80` headers; adopt 2/3 of performance rules.
- **Advanced**: Implement the full 4.2 performance bundle (SSR SVG build, defs dedupe, hash-etag CDN, split convention); build interactive click+tooltip diagrams; author the 4.3 error code diagnostic pipeline with automated remediation suggestions.

---

## Skill Area 5 — Quality Assurance: Syntax Validity, WCAG Accessibility, Maintenance Guidelines

### 5.1 Cross-platform syntax validation pipeline

Three-stage gate, run on every PR that modifies `docs/**/*.md` via GitHub Actions (see `.github/workflows/docs-lint.yml` if present; if the workflow file hasn't been scaffolded yet, the commands below are canonical):

```yaml
# Canonical canonical mermaid validation stages; 1, 2, 3 must all pass before merge.
# Stage 1 — pure parse (no browser, ~150 ms/block)
- name: Mermaid parse-level validation
  run: |
    node scripts/docs/validate_mermaid.cjs "docs/**/*.md" \
      --enforce-first-line-type \
      --max-nodes 140 \
      --max-line-len 55 \
      --require-alt-paragraph \
      --require-callout-paragraph \
      --security-level strict
# Stage 2 — headless render (Puppeteer; ~1.5 s/block parallel)
- name: Mermaid render-level validation + dark/light PNG export
  run: |
    node scripts/docs/mermaid_export.mjs docs/**/*.md \
      --out-dir docs-assets/diagrams/ \
      --formats svg,png \
      --themes light,dark \
      --viewport 1120,390
# Stage 3 — accessibility + brand contrast
- name: WCAG 2.1 AA contrast audit + alt text audit
  run: |
    node scripts/docs/a11y_contrast.mjs docs-assets/diagrams/*.svg
    node scripts/docs/a11y_alt_text.mjs    docs/**/*.md --enforce-non-empty
```

Validation output must be actionable: when a diagram fails `--require-alt-paragraph`, the lint error points at the exact fence line number and includes the copy-pasteable 2-paragraph skeleton the author needs to add.

### 5.2 Accessibility (WCAG 2.1 AA) audit framework

Diagram authors (≥ Intermediate band) MUST satisfy 7 WCAG criteria for every diagram:

| WCAG SC | Requirement | How we satisfy it in Mermaid |
|---|---|---|
| 1.1.1 Non-text Content (A) | Equivalent purpose-description for every non-text element. | Mandatory prose paragraph BEFORE the fence describing what the diagram shows and how to read it; add it to the `aria-labelledby` of the container via 3.1.1 accessibility script. |
| 1.3.1 Info and Relationships (A) | Logical reading order conveyed in non-visual presentation. | Linearise flowcharts into plain-English step list in the callout paragraph after the diagram; for sequence diagrams, re-assert order in the autonumbered prose. |
| 1.4.1 Use of Color (A) | Meaning cannot be conveyed by color alone. | Every `classDef`-colored node must also carry a text marker: `([OK] Publish happy path)`, `([WARN] Slow consumer dropped)`, `([ERR] Moderation required)`; color is a redundant cue only. |
| 1.4.3 Contrast Minimum (AA) | 4.5:1 text-to-background; 3:1 non-text UI. | `scripts/docs/a11y_contrast.mjs` reads every SVG text element + its parent fill; warns < 4.5. Brand palette tokens pre-audited. |
| 1.4.10 Reflow (AA) | 320 CSS px wide viewport — no horizontal scroll for reading. | Responsive rules §3.2; mmdc 390px export validation; `useMaxWidth:true`. |
| 1.4.11 Non-text Contrast (AA) | Graphical objects 3:1 contrast with adjacent background. | Stroke colors set to 700-level brand tokens; never 400-level thin lines on light gray. |
| 2.4.7 Focus Visible (AA) | `click` interactive nodes get focus rings. | Mermaid 10.9 sets `tabindex:0` on clickable nodes; add CSS `pre.mermaid svg a:focus { outline: 2px solid var(--brand-primary-2); outline-offset: 2px; }`. |

### 5.3 Maintenance guidelines for long-term diagram upkeep

These rules prevent diagram rot when a repository lives ≥ 3 years with many contributors:

#### 5.3.1 Source-of-truth contract

- **One semantic diagram lives in exactly ONE `.md` file**. If another doc needs to reference it, use a prose link and embed the exported SVG raster, never copy-and-paste the Mermaid source.
- **Every diagram has one clear owner**, declared via `%% @owner @github-handle` comment on line 2 of every fence; owner is responsible for reviewing drift against source code.
- **Every diagram declares a version & contract IDs**: `%% @version 1.2.0` and `%% @anchor login_happy_path_A login_slo_B`; test fixtures reference these anchors and fail PRs that remove or rename them without a contract-bump commit.

#### 5.3.2 Versioning & migration rules

When a diagram changes significantly (> 30% of nodes/edges modified in a single commit):
1. Increment the `@version` using semver semantics: major for semantic shape change, minor for additions, patch for cosmetic / text fixes.
2. Update the companion callout paragraph with a short "changed in v1.2.0: added private-IP-only Cloud SQL link, removed public IP" note.
3. For diagrams referenced by external docs (Confluence, Notion, customer handbooks), append the new version to the `_manifest.json` and the downstream `sync-to-confluence` GitHub Action will re-export and raise a PR in the Confluence-as-code repo.

#### 5.3.3 CI-enforced freshness checks (Advanced)

For diagrams whose truth lives in code (ERD, class diagrams, architecture container inventories):
- **ERD drift check** (§1.3 Advanced): `scripts/docs/validate_mermaid_erd.py` diffs erDiagram columns + FKs against latest `migrations/*.sql` schema.
- **Container inventory drift check**: `scripts/docs/validate_architecture_inventory.py` asserts every C4Container declared in deployment.md has a matching Terraform resource + runbook link.
- **Sequence diagram SLO anchors**: `scripts/docs/validate_slo_annotations.py` confirms every `rect rgb(...)` block in a sequence diagram maps to a declared SLO in [architecture/observability.md](../architecture/observability.md) — ensures the diagram doesn't silently drop a required SLO box.

### 5.4 Skill proficiency bands

- **Beginner**: Run stage-1 parse validation locally; fix 10 trivial lint errors (missing first-line type declaration, max-line-len violations, alt-paragraph gaps); apply 2 WCAG checks (1.1.1 + 1.4.1 color-not-only).
- **Intermediate**: Run the full 3-stage CI pipeline locally; enforce all 7 WCAG SCs; adopt 5.3.1 owner + version comments on every diagram; create/maintain `_manifest.json`.
- **Advanced**: Implement ERD/architecture/SLO drift-check hooks; author the Confluence-as-code downstream sync action; build CI gates that reject PRs removing `@anchor` nodes without contract-version bump; maintain accessibility audit dashboard.

---

## Skill Area 6 — Competency Framework: Proficiency Matrix, Real-World Use Cases & Example Implementations

### 6.1 Proficiency matrix overview

Authors advance one skill area at a time. Overall role expectations:

| Role | Minimum expected band across all 6 skill areas | Who reviews their diagrams before merge? |
|---|---|---|
| Junior contributor / onboarding | Beginner 1/6 → pass 4 beginner sub-bands | Senior diagram reviewer + docs maintainer |
| Backend/feature spec author | Intermediate 5/6 + Advanced 1/6 | Peer + diagram area owner `@owner` |
| Architecture author / public docs lead | Advanced 4/6, Intermediate 2/6 | Architecture council + accessibility reviewer |
| Docs portal & accessibility owner | Advanced 6/6 | Cross-functional: security + design + product |

### 6.2 Skill area × proficiency band deliverables

The table below is the **master grading rubric** used in PR reviews.

| Skill area | Beginner outcomes (demonstrable) | Intermediate outcomes | Advanced outcomes |
|---|---|---|---|
| 1. Fundamentals & syntax | Produces 6 correct diagrams: 1 of each type (flow, seq, ER, Gantt, class, arch) ≤ 20 nodes each with zero parse errors. | Diagrams ≥ 40 nodes; fragments with subgraphs; autonumbering + classDefs; every diagram references ERD or architecture.md. | Composable fragments + include bundles; drift-validated ER/class/architecture inventories; SLO anchors. |
| 2. Platform integration | Embeds diagrams correctly on GitHub + MkDocs with zero render drift. | Same source renders on GitHub, MkDocs, GitLab, Confluence (SVG export); no `securityLevel:loose`. | CI manifest + downstream sync to Confluence/Notion; dark/light theme swap at render time; offline PDF bundle. |
| 3. Setup, themes, responsive | Uses 3.1.1 skeleton + theme JSON; renders diagram in static HTML; Chrome + Safari. | Enforces 6 responsive rules; paired SVG/PNG mmdc export; brand palette in sync with design system. | `prefers-color-scheme` theme swap; WCAG-tagged PDFs; CI-enforce node-width mobile previews. |
| 4. Interactive, performance, troubleshooting | Resolves 3 most common errors; writes diagrams with no console warnings. | Lazy render + viewport zoom; 140-node split convention; adds 5 of 4.2 performance rules. | Full SSR build pipeline; interactive click+tool tips; authors 4.3 error diagnostic pipeline with auto-remediation. |
| 5. QA: syntax, a11y, maintenance | Runs stage-1 parse; fixes 10 lints; 1.1.1 + 1.4.1 WCAG rules. | 3-stage CI pipeline; all 7 WCAG SCs; owner + version + anchor comments on every diagram. | Drift checks (ER/class/architecture/SLO); contract version bump CI gate; Confluence-as-code downstream sync; a11y audit dashboard. |
| 6. Competency application | Contributes 6 diagrams across 3 docs, no regressions. | Owns diagram updates for a feature release; ensures no drift between architecture/backend/feature docs. | Leads quarterly diagram hygiene sprint; trains 2 juniors; updates this framework version. |

### 6.3 Example: End-to-end implementation for blog-api architecture set

This end-to-end recipe is the canonical "deliver a diagram set" workflow every contributor follows when shipping a new feature (example = SSE notifications feature shipped in `realtime-notifications-sse.md`):

#### Step 1 — Plan the diagram set (Skill 1: Intermediate + Advanced)
For any feature, author exactly **5 companion diagrams**:
1. **L1 system context flow** — 8 nodes, flow LR; links to the feature section anchor.
2. **Happy-path sequence diagram** — ≥ 6 participants, autonumber on, SLO rect.
3. **Error & retry sequence fragment** — only the `break when` / `critical timeout` block.
4. **ERD slice** — 4–8 tables related to the feature; cardinality arrows; cross-ref ERD.md.
5. **Gantt rollout**: 4 phases, 2 critical milestones, 2 parallel swimlanes (Backend + Frontend/QA).

#### Step 2 — Embed across platforms (Skill 2: Intermediate)
- Paste sources into the feature Markdown doc; confirm GitHub preview renders.
- `npm run docs:mermaid:export` → copy `.svg` + `.png` pairs into Confluence page + Notion launch doc.
- Attach the manifest entry SHA to the PR description so downstream consumers (training slides, customer onboarding) auto-detect updates.

#### Step 3 — Apply theme + responsive (Skill 3: Intermediate)
- First line of every fence = `%%{init: <docs/guides/mermaid-theme.json inline merge>}%%`.
- Add prose alt-paragraph before AND callout-paragraph after.
- Run `mmdc --width 390` export; verify zero horizontal scroll on iPhone preview.

#### Step 4 — Add interactivity (Skill 4: Advanced, optional)
- Add `click` handlers linking node IDs to backend/database.md, architecture.md, other features.
- Add `tooltip` on the 3 most important components (Dialer, SSE hub, Redis replay ring).

#### Step 5 — Pass 3-stage QA (Skill 5: Intermediate)
- Stage 1 parse: zero warnings; owner `%% @owner @your-handle`, version `%% @version 1.0.0`, anchors `%% @anchor sse_happy_path_A`.
- Stage 2 render: dark + light SVG both < 80KB; no console errors in Puppeteer log.
- Stage 3 a11y: 7 WCAG SCs pass (contrast 4.5:1, non-text-alt, reflow ≤ 320 px, focus visible).

#### Step 6 — Maintenance (Skill 6: Advanced)
- Future PR changing architecture → update `@version 1.1.0` + add change note to callouts.
- Future PR deleting SSE hub → CI fails on anchor drift; owner + architecture council approve deletion.
- Yearly diagram hygiene sprint → owner re-validates all 5 diagrams against HEAD source; 12 months no owner change → rotate to next maintainer.

### 6.4 Implementation example — minimum passable Intermediate Mermaid block

Every author wishing to move past Beginner must first author a diagram matching this shape (actual content adapted to their feature; style, size, comments, alt/callout paragraphs must mirror this exactly).

```markdown
### L2 container: SSE notification fan-out

The diagram below models the multi-process fan-out for real-time notifications on blog-api. Read left-to-right: post mutation → Watermill transactional outbox → Redis pub/sub → per-process SSE hub → open streams. The grey box is the reverse-proxy (Cloudflare WAF + Nginx ingress) handling CORS + CSRF + rate-limit gates described in the SSE feature doc. Error paths (slow-consumer buffer full, reconnect jail) are highlighted in amber/red text markers so colour alone is never the sole cue.

```mermaid
%% @owner @turahe-core
%% @version 1.0.0
%% @anchor sse_l2_fanout sse_pubsub sse_hub
%% @max-nodes 80
%%{init: {
  "theme": "base",
  "themeVariables": {
    "primaryColor": "#eff6ff", "primaryBorderColor": "#2563eb", "lineColor": "#475569",
    "fontFamily": "Inter, ui-sans-serif, system-ui, sans-serif", "fontSize": "13px",
    "warningBkgColor": "#fff7ed", "warningBorderColor": "#d97706",
    "dangerBkgColor":  "#fee2e2", "dangerBorderColor":  "#dc2626"
  },
  "flowchart": {"useMaxWidth": true, "htmlLabels": false, "curve": "basis"}
}}%%
flowchart LR
  subgraph EDGE[Cloudflare + Nginx ingress]
    WAF[WAF / CORS / rate / CSRF] --> HTTP[Gin HTTP entry\n/api/v1/me/notifications/stream]
  end
  HTTP --> AUTH[auth middleware\nJWT bearer or session cookie]
  AUTH --> REG([SSE hub register\nstream 1/3 per user])
  subgraph APPS[3 × Cloud Run processes (parallel fan-out)]
    H1[SSE hub process 1\nchannel buffer 512]
    H2[SSE hub process 2\nchannel buffer 512]
    H3[SSE hub process 3\nchannel buffer 512]
  end
  REG --> H1
  REG --> H2
  REG --> H3
  POST([Post mutation\ncreates notification]) --> OUT[Watermill outbox\ntransactional insert]
  OUT --> POLL[Worker polls outbox\nat-least-once]
  POLL --> RPUB[Redis PUBLISH\nchannel = notifications.user.<id>]
  RPUB --> H1
  RPUB --> H2
  RPUB --> H3
  H1 --> S1[([OK] 16 streams)]
  H2 --> S2[([OK] 24 streams)]
  H3 --> S3[([WARN] slow consumer\nbuffer 512/512\nfanout.buffer_full)]
  click POST   "https://github.com/turahe/blog-api/blob/main/docs/backend/events.md"             _blank
  click OUT    "https://github.com/turahe/blog-api/blob/main/docs/backend/database.md#outbox-table" _blank
  click RPUB   "https://github.com/turahe/blog-api/blob/main/docs/features/realtime-notifications-sse.md#L200-L280" _blank
  click REG    "https://github.com/turahe/blog-api/blob/main/docs/features/realtime-notifications-sse.md#L120-L150" _blank
  tooltip H3 "Slow consumers get error frame + drop; 15 s ping watchdog kills half-open sockets. See SSE feature §3 lifecycle + §6.1 error handling."
  classDef ok    fill:#ecfdf5,stroke:#16a34a,stroke-width:1.5px;
  classDef warn  fill:#fff7ed,stroke:#d97706,stroke-width:1.5px;
  classDef err   fill:#fee2e2,stroke:#dc2626,stroke-width:1.5px;
  class S1,S2 ok;
  class S3,H3 warn;
```

Callouts: (1) the 3 Cloud Run processes are independent — Redis pub/sub is the shared fan-out backbone. (2) The SLOW CONSUMER ([WARN]) branch drops frames and emits an in-band `fanout.buffer_full` error so the client can fall back to polling. (3) Per-user connection limit is 3 streams; the 4th concurrent open gets HTTP 429 `rate_limit.exceeded` with `Retry-After: 60` — this error path is modelled explicitly in the SSE test fixtures at `tests/contracts/fixtures/notifications-stream.json`.
```

### 6.5 Example implementations — one diagram per type (copy/adapt templates)

TBD copy-paste template location for contributors — if a diagram matching these patterns is needed, copy and adapt. All templates live in `docs/guides/_mermaid-templates.md` (scaffolded separately). The patterns covered:
1. **Flowchart**: Authentication login + password-reset branching (20 nodes).
2. **Sequence diagram**: Comment submission with spam scan + moderation + notification (8 participants, 15 messages, autonumber).
3. **ER diagram**: Comments module slice (comments + comment_flags + comment_reactions + comment_moderation_actions).
4. **Gantt chart**: Quarterly release — 4 phases, 2 swimlanes, 3 milestones.
5. **Class diagram**: Hexagonal ports & adapters for Posts module (interface, GORM impl, test double, service).
6. **Architecture diagram**: L2 C4Container — Cloud Run + Cloud SQL + Redis + R2 + WAF, matching the production topology in [architecture/deployment.md](../architecture/deployment.md).

---

## Maintainer SOP: How to apply this framework

1. Every PR that adds a Mermaid diagram MUST include one reviewer at the skill level of the diagram or above. Junior → reviewer ≥ Intermediate on that skill area.
2. Pre-commit hook `.husky/pre-commit` runs stage-1 parse validation (`--enforce-first-line-type --max-nodes 140 --max-line-len 55 --require-alt-paragraph --require-callout-paragraph --security-level strict`). Stage-1 failures block the commit.
3. Docs maintainers perform a quarterly "diagram hygiene" pass:
   - Rotate owners on diagrams untouched for 12 months.
   - Audit version numbers — bump minor versions for any drift that escaped CI.
   - Check `_manifest.json` against the actual diagram count; delete stale SVG/PNG artefacts.
   - Accessibility re-run: contrast audit, reflow, focus tests.
4. This Mermaid competency framework itself uses semantic versioning. Any change to the proficiency bands or grading rubric requires a PR reviewed by at least 2 docs-portal Advanced authors. Current framework version: **1.0.0**.
