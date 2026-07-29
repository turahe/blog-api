---
name: "document-summarization"
description: "Multi-tier summarization: executive summary, section-by-section summaries, key-takeaway lists. Invoke for TL;DR, briefs, digests, or condensed long-document reviews."
---

# Document Summarization

Generates three tiers of summaries from a parsed + optionally analyzed document. Human-validated critical-content capture target: **≥ 90%**.

## Summary Tiers (Always Produce All Three; User May Select)

### 1. Executive Summary (`executive`)

For C-suite / decision-makers. Constraints:
- Length: ~5% of document (max 2 pages regardless of doc size; cap at 1000 words for 10,000-page docs)
- Structure:
  1. `doc_type` + `document_scope` (1-2 sentences)
  2. `core_findings` (3-7 bullets)
  3. `key_decisions_required` (if any, with owners + due dates pulled from extraction)
  4. `major_risks` (ordered by severity/likelihood using sentiment + requirements analysis)
  5. `recommended_next_actions` (top 3)
- Must cite section references for each claim so downstream readers can locate evidence.

### 2. Section-by-Section (`by_section`)

Per heading_id:
- 3-7 sentence narrative summary covering purpose, claims, evidence, conclusions in that section.
- Inline extracted entities / figures relevant to that section.
- Preserves parent→child ordering from the heading tree.

### 3. Key Takeaways (`key_takeaways`)

Ordered list, 5-25 items depending on length:
```json
{
  "id": "kt1",
  "text": "...",
  "category": "finding | decision | risk | action_item | uncertainty",
  "impact": "low | medium | high | critical",
  "evidence_citations": [{"heading_id":"h4","span_start":...,"span_end":...,"page":12}]
}
```

For the longest documents (≥ 2000 pages): group key takeaways by `topic_id` from content-analysis topics; add a short per-topic header.

## Quality & Faithfulness Rules

- **No hallucination:** every summary sentence must cite ≥ 1 evidence span (except meta framing like "This document is a report...").
- **Faithfulness metrics:** compute extractive coverage % + abstractive consistency via NLI with evidence; if < 0.9, flag in warnings and trigger re-summarization with tighter evidence constraints.
- **Tone preservation:** retain original formality/skepticism; do not soften hedged language into hard claims.
- **Negative claims / denials:** include explicitly (high risk of being dropped by generic summarizers).

## Output Envelope

```json
{
  "data": {
    "summary": {
      "tiers": {
        "executive": {
          "markdown": "...",
          "structure": { "scope":"...", "core_findings":[], "...":"..." }
        },
        "by_section": [
          {"heading_id":"h1","title":"Intro","markdown":"...","entities_involved":[]}
        ],
        "key_takeaways": []
      },
      "faithfulness": {
        "coverage_pct": 0.91,
        "consistency_score": 0.96,
        "evidence_citations_count": 74
      }
    }
  },
  "meta": {
    "skills_applied": ["document-parsing","document-content-analysis","document-summarization"],
    "warnings": [
      {"code":"low_coverage","message":"Section Appendix A summary dropped below coverage threshold; condensed to bullets."}
    ]
  }
}
```

Human-readable markdown payloads are always returned alongside structured JSON for UI rendering.

## Large-Document Strategy (≤ 10,000 pages)

1. First pass: per-section summaries (by_section tier) in parallel chunks.
2. Second pass: section-summary clustering → topic-group summaries.
3. Third pass: topic summaries merged into the executive tier + flattened key-takeaways list.
4. Evidence back-links always point to original spans, never to intermediate summaries, to preserve providence.

Streaming telemetry reports progress at 25%/50%/75%/100% checkpoints in meta.

## Human Reviewer Validation Checklist (≥ 90% Capture)

For every critical-element type defined per domain:
- **Legal:** All named parties, material dates, payment sums, termination triggers, indemnification caps.
- **Technical:** Architecture scope, critical interfaces/APIs, performance thresholds, operational constraints.
- **Academic:** Research question, methodology innovations, key results magnitudes, limitations, funding/conflicts.
- **Business:** Revenue/profit figures, strategic priorities, market guidance, major risks, leadership + org changes.

Treat "missed by summary" as false-negative; any 2/20 missed critical items triggers `meta.warnings` with code `critical_capture_below_threshold` and suggested section re-read.

## Error Handling

- Very short docs (< 1 page): collapse tiers; still return all three but note "short document" with a warning.
- Analysis dependency missing: implicitly call content-analysis first.
- Chunk failures: summarize available sections, note skipped sections with reason in warnings.
