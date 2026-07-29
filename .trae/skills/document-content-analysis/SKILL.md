---
name: "document-content-analysis"
description: "Applies NLP to understand document context: key topics, entities, sentiment, arguments, and section-level relationship graphs. Invoke after document-parsing when comprehension or context is needed."
---

# Document Content Analysis & Understanding

Deep semantic analysis pass over an already-parsed `DocumentEnvelope`. Produces topics, entities, sentiment, argument/claim structure, and a logical relationship graph between sections. This is a required upstream step for high-quality extraction, summarization, compliance, and Q&A — call it whenever the request depends on *meaning* rather than just surface text.

## Input Prerequisites

- `document-parsing` output containing: `document_id`, `full_text`, `structure.headings`, `structure.paragraphs`, `metadata.content_language`.
- If the user provides raw text or a short doc, call document-parsing implicitly first; never require the user to manually chain.

## Analysis Outputs (All Populated)

### 1. Key Topic Model (`topics`)

Per section and per document:

- `topic_id`, `label` (short human-readable phrase), `keywords[]` (≤ 12), `confidence`
- `section_coverage[]`: heading_ids where this topic is dominant
- `related_topics[]`: topic_ids with overlap > threshold
- Strategy: use hierarchical clustering over embeddings; label via centroid keyword extraction + LLM post-labeling for human-readable names.

### 2. Entity Extraction (`entities`)

Entity types with schema normalization:

| Type             | Normalized shape                                                          |
|------------------|---------------------------------------------------------------------------|
| Person           | `{name, given_name, family_name, aliases[], occurrences[{span, page}]}`   |
| Organization     | `{name, aliases[], legal_name?, sector?, occurrences[]}`                  |
| Location         | `{name, country_code?, geo_json?, occurrences[]}`                         |
| Date / DateTime  | `{text, iso8601, granularity: year|month|day|time, occurrences[]}`       |
| Money / Amount   | `{value, currency_iso4217, unit?, occurrences[]}`                         |
| Percentages, URLs, Emails, Phone numbers | Standard RFC normalization + occurrences |
| Custom (user supplied) | Pass a schema map in the request; skill maps spans to user types         |

- Consensus rule: combine dictionary-based, regex-based, and model-based extractors; resolve disagreement with weighted voting (≥ 0.95 precision target).
- Cross-document normalization (when correlation skill is used later): entity IDs are stable across docs via canonical hashed name + type.

### 3. Sentiment & Tone (`sentiment`)

At both document-level and section-level (heading_id):

- `polarity`: `positive | negative | neutral | mixed`
- `score`: float [-1.0, 1.0]
- `emotion_tags[]`: top-3 from [joy, trust, fear, surprise, sadness, disgust, anger, anticipation] (Plutchik)
- `formality_score`: float [0,1] where 1 = highly formal/legal/academic
- Confidence tags: hedging language indicators (`it is believed that`, `may`, `could`), forward-looking statements, risk-factor flags.

### 4. Main Arguments & Claims (`arguments`)

- `claim_id`, `claim_text`, `claim_type`: `thesis | evidence | counter | conclusion | recommendation`
- `attribution`: who made the claim (person/org or implicit author)
- `evidence_spans[]`: paragraph/text spans supporting the claim
- `section_id`: where the claim appears
- Build a directed claim graph: edges = `supports | contradicts | is_evidence_for | responds_to`.

### 5. Section Relationship Graph (`section_graph`)

Nodes are heading_ids. Edges:

- `follows_in_argument` (sequential logical flow)
- `cross_references` (explicit "see Section 3")
- `defines_term → uses_term` (defined term to usage site)
- `depends_on` (e.g., prerequisites, method → results dependency)

Serialize as edges list `{from, to, type, confidence}`.

## Output Envelope

Merge into canonical envelope under `data.analysis`:

```json
{
  "data": {
    "analysis": {
      "topics": [],
      "entities": { "people": [], "organizations": [], "locations": [], "dates": [], "amounts": [], "custom": {} },
      "sentiment": { "document": {}, "by_section": [{ "heading_id":"h1", "...": "..." }] },
      "arguments": { "claims": [], "claim_graph_edges": [] },
      "section_graph": { "edges": [] }
    }
  },
  "meta": { "...": "...", "skills_applied": ["document-parsing","document-content-analysis"] }
}
```

## Scale Strategy (≤ 10,000 pages)

- Compute embeddings per chunk; aggregate section-level scores with weighted pooling.
- Entity linking runs once per chunk; deduplicate cross-chunk using cosine + canonicalization rules.
- Claim graph builds per-section first; cross-section edges in a second pass on candidate pairs only (O(n²) per doc → O(n) by filtering).
- Stream sentiment per chunk and reduce (weighted average + max for polarity).

## Error Handling

- Empty/low-quality text: return empty arrays with `meta.warnings` including `confidence`.
- Unsupported language: attempt polyglot entity model; if below threshold, report `unreadable_content` with `details.detected_language`.
- Partial failures: always return partial analysis; tag affected sections with `analysis_confidence < 0.7` in meta so downstream skills can downgrade or retry.

## E2E Test Matrix

- **Legal contracts:** Parties, dates, money 95%+; defined terms correctly identified; clause dependency graph matches manual graph.
- **Technical manuals:** Procedure dependencies match manual topology; parts / units / quantities normalized.
- **Academic papers:** Claims and evidence structure maps to manual section typology; citation entities captured.
- **Business reports:** Forward-looking + risk-factor recall ≥ 95%; sentiment matches reviewer consensus on KPI sections.

Logging per Document Intelligence standards; track precision/recall on test suites via telemetry so success-rate dashboards can compute trend lines.
