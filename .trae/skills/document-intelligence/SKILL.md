---
name: "document-intelligence"
description: "Umbrella orchestrating parsing, analysis, extraction, summarization, correlation, compliance & Q&A for document collections. Invoke for any end-to-end document workflow or sub-skill routing."
---

# Document Intelligence

Umbrella orchestration skill that coordinates the full-document sub-skill pipeline. Use this entry point when the user requests any multi-step document workflow or does not explicitly specify a sub-skill; this skill selects and composes the appropriate sub-skill sequence from the toolkit below.

## Sub-skill Toolkit

1. **document-parsing** — Multi-format ingestion and structural extraction (PDF/DOCX/TXT/HTML/JSON/XML/Markdown).
2. **document-content-analysis** — NLP understanding: topics, entities, sentiment, argument mapping, section relationships.
3. **document-info-extraction** — Structured data points (names, dates, amounts, requirements, action items) normalized to JSON/CSV.
4. **document-summarization** — Multi-tier summaries: executive, section-by-section, key-takeaway lists.
5. **document-cross-correlation** — Collection-level analysis: connections, overlap, duplicates, dependencies.
6. **document-compliance-checker** — Regulatory / policy scan with violation flagging and audit reports.
7. **document-query** — Conversational Q&A over any document section with cited section sources.

## Standard Processing Workflow (Default)

For any end-to-end request, run the pipeline in this order and combine outputs:

1. **Parse** → emit canonical `DocumentEnvelope` with text/structure/metadata/media.
2. **Analyze** → augment envelope with topics, entities, sentiment, section graph.
3. **Extract** → append structured records keyed by semantic type; normalize to JSON.
4. **Summarize** → produce executive, section, and key-takeaway tiers (target ≥ 90% critical capture).
5. **Compliance (if requested)** → run ruleset, zero-false-negative pass, append audit report.
6. **Query (if user asks questions)** → context-aware answers with citations.
7. **Cross-correlation (for >1 document)** → build collection graph, flag overlaps/duplicates/dependencies.

## Target Non-Functional Requirements (Apply to All Sub-skills)

- **Scale:** Handle documents up to **10,000 pages** without accuracy degradation. Use chunked processing (≤ 8k tokens per chunk) with chunk-overlap (10–15%) and aggregation steps. For very long texts, build a hierarchical index (section-level → subsection-level) before running heavy tasks.
- **Throughput SLA:** Target processing speed **under 1 minute per 100 pages** in production. Track latency per sub-skill and report in meta block.
- **Accuracy SLA:**
  - Entity extraction and key information identification: **≥ 95%** on test sets.
  - Summarization critical-content capture: **≥ 90%** as validated by human reviewer checklists.
  - Compliance controlled test cases: **zero false negatives** (false positives allowed but must be justified with rule evidence).
- **Error Handling:** Three classes —
  - `corrupted_file`: bytes unreadable or damaged; report the file path, bytes-read %, and attempted recovery steps.
  - `unsupported_format`: content-type / extension not in allowlist; list attempted sniffers and the detected MIME.
  - `unreadable_content`: format recognized but text/OCR empty or garbled; return raw sampled bytes and confidence score.
  Always return a structured error envelope with `error.code`, `error.message`, `error.details`, `error.recovery_suggested` — never a bare exception.
- **Logging & Monitoring:** Every sub-skill invocation appends to the `meta.telemetry` block:
  ```json
  {
    "skill": "<sub-skill-name>",
    "started_at": "...",
    "ended_at": "...",
    "pages_or_chunks": 0,
    "success": true,
    "warnings": [],
    "latency_ms_per_100_pages": 0
  }
  ```
  Success rates and error histograms must be computable from these fields.
- **Data Privacy & Security:**
  - Apply **data minimization** — retain only the fields the user requested.
  - Tag every output record with `sensitivity` enum (`public`, `internal`, `confidential`, `restricted`) derived from document classifier or explicit user tagging.
  - Do not log or embed raw secrets (PII, credentials, PHI); emit `pii_redacted: true` when redaction rules fire.
  - For sensitive documents, prefer on-device models and deterministic hashes over raw storage of content in telemetry.
- **E2E Test Coverage Matrix (run before trusting output on a new domain):**
  - Legal contracts — entity extraction, obligation/action item lists, compliance rule hits.
  - Technical manuals — heading/tree accuracy, table capture, procedure extraction.
  - Academic papers — abstract matching, citation graph, methodology/results separation.
  - Business reports — executive summary accuracy, KPI capture, forward-looking statements.

## Canonical Output Envelope

All sub-skills return data wrapped in:

```json
{
  "data": { "...": "..." },
  "meta": {
    "document_id": "<uuid>",
    "source": "<file path or URI>",
    "page_count_or_chunk_count": 0,
    "skills_applied": ["..."],
    "telemetry": [],
    "warnings": []
  },
  "error": null
}
```

## Routing Guidance for the Orchestrator

| User Request Signal                                   | Sub-skill Order                                         |
|-------------------------------------------------------|---------------------------------------------------------|
| "read this PDF / give me the content / parse this"    | document-parsing → (optional content-analysis)          |
| "understand this doc / what's it about / sentiment"   | document-parsing → document-content-analysis            |
| "get names/dates/amounts/requirements/action items"  | document-parsing → document-info-extraction             |
| "summarize / TLDR / executive summary / key points"   | document-parsing → (analysis) → document-summarization  |
| "compare these docs / find duplicates / overlaps"     | parse all → document-cross-correlation                  |
| "check compliance / audit against policy"             | parse + extract → document-compliance-checker           |
| "ask questions about this / Q&A / search inside"      | parse + analysis index → document-query                 |

## Invocation Examples

- *"Please process this 400-page legal contract, summarize obligations, extract parties/dates, and flag GDPR issues."* → Use document-intelligence umbrella to chain: parsing → content-analysis → info-extraction (parties, dates, obligations) → summarization (exec + key takeaways) → compliance (GDPR ruleset).
- *"Can I ask a few questions about the product requirements doc?"* → Build parsing + analysis index once; hand repeated Q&A calls to document-query.
- *"Compare these 6 vendor proposals and tell me which overlap in scope."* → Parse all 6; cross-correlation for overlap/dependency graph; summary per vendor.
