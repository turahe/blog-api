---
name: "document-cross-correlation"
description: "Analyzes multi-document collections to surface connections, duplicate/overlapping content, and dependency graphs. Invoke when comparing proposals, contracts, versions, or any document set >1 file."
---

# Cross-Document Correlation

Operates on collections of 2+ `DocumentEnvelope`s (already parsed + analyzed). Identifies connections, overlaps/duplicates, and dependency relationships between files. Use this skill whenever the user mentions "compare", "across these docs", "consistency check", "duplicates", "dependencies between documents".

## Input

A named list of documents: `[{document_id, source, envelope}]`. Minimum collection size = 2. For single documents, warn and return empty `data.correlation`.

## Correlation Outputs

### 1. Connection Graph (`connections`)

Edges between `(document_id, section_id?)` pairs:

```json
{
  "from": {"document_id":"doc_A","heading_id":"h2-1"},
  "to":   {"document_id":"doc_B","heading_id":"h3-4"},
  "type": "cites | defines | contradicts | aligns | supersedes | references_data | shared_party",
  "weight": 0.0,
  "evidence_spans": [{"from_span":[0,400],"to_span":[180,500]}]
}
```

Heuristics per type:
- `cites`: explicit doc ID, title, section reference strings, URLs, DOI cross-links
- `defines` / `uses_term`: vocabulary alignment from normalized entities + defined-terms graph
- `contradicts`: opposing polarity claims on shared topic
- `aligns`: same claim or numeric figure with ≤ tolerance % delta
- `supersedes`: explicit version/date chains + legal supersession language
- `references_data`: shared dataset IDs, SQL table names, metric/KPI identifiers
- `shared_party`: shared organization/person entity IDs

### 2. Overlap / Duplicate Detection (`overlaps`)

```json
{
  "pair": ["doc_A", "doc_B"],
  "kind": "near_duplicate | partial_overlap | lifted_content | shared_reference",
  "similarity_score": 0.0,
  "spans": [
    {"doc_A":{"heading_id":"...", "text":"..."}, "doc_B":{"heading_id":"...","text":"..."}}
  ],
  "earliest_document_id": "doc_A",
  "note": "Verbatim 4-paragraph lift from Appendix; no attribution in doc_B."
}
```

Methods:
- Semantic: embedding cosine per-chunk centroid (thresholds: ≥ 0.97 lift, ≥ 0.9 partial)
- Lexical: N-gram winnowing / Rabin-Karp for long verbatim runs
- Structural: heading + table schema fingerprints

Always flag with `earliest_document_id` to surface provenance.

### 3. Dependency Map (`dependencies`)

For collections ordered (e.g., contract → amendments → SOWs → invoices):

```json
{
  "nodes": [{"document_id": "...", "role": "master | amendment | sow | invoice | attachment"}],
  "edges": [{"from": "doc_contract", "to": "doc_sow_1", "type": "has_statement_of_work"}],
  "orphans": ["doc_invoice_9_unmatched"]
}
```

Detect roles via metadata + title regex + content classifiers.

### 4. Consistency Alerts (`consistency_alerts`)

- **Numeric drift:** same-named metric/KPI across docs with delta > tolerance (user-supplied tolerance or default 2%).
- **Date drift:** same event (contract start, delivery) with conflicting ISO dates.
- **Entity drift:** same legal/company alias resolved to conflicting canonical IDs.

Each alert cites the documents and exact spans.

## Output Envelope

```json
{
  "data": {
    "correlation": {
      "document_ids": ["doc_A","doc_B","doc_C"],
      "connections": [],
      "overlaps": [],
      "dependencies": {},
      "consistency_alerts": []
    }
  },
  "meta": { "...": "..." }
}
```

## Scale Strategy (≤ 10,000 total pages across any number of docs)

- Embed all chunks first into shared vector index (single-pass); compare via nearest-neighbor search (approximate with HNSW).
- Pairwise comparisons limited to candidates from topic + shared-entity filters.
- Chunk-by-chunk dedup streaming writer: rows emitted in CSV for audit when N > 100 docs.
- Telemetry records: `pairs_compared`, `pairs_filtered`, `similarity_comparisons_count`, `elapsed_ms`.

## Error Handling

- One document unparseable: skip it, list skipped in warnings, still correlate remaining set.
- Shared language baseline: high overlap detected from domain-standard clauses (e.g., legal boilerplate) → mark as `shared_reference` instead of duplicate using a boilerplate whitelist.

## E2E Test Set Coverage

- **Vendor proposals (6 docs):** correctly links shared assumptions, flags near-duplicate solution descriptions, numbers any inconsistent pricing.
- **Contract + amendments + SOW:** dependency map roles correct; supersession chain aligns with dates; dollar totals consistent.
- **Paper versions (preprint vs published):** overlaps surface methodology blocks lifted; contradictions between results tables tagged.
- **Business report series (quarterly x 4):** consistency alerts catch KPI re-statements between Q2 restatement and Q3.
