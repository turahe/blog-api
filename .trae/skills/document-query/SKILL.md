---
name: "document-query"
description: "Conversational Q&A over any document section: natural language questions, context-aware answers, cited section sources. Invoke for search, question answering, or lookup inside a parsed document set."
---

# Document Query & Response Generation

Interactive natural language interface over one or more `DocumentEnvelope`s (plus optional analysis and extraction enrichments). Provides grounded, context-aware answers with citations for every non-trivial claim, rather than free-form generation.

## Invocation Triggers

Use this skill when the user:
- Asks a question starting with "What/When/Who/How much/Does it say… about X?"
- Searches inside documents for phrases, sections, clauses, tables.
- Requests comparisons between specific clauses in specific documents.
- Wants quoted sections with stable references back to original pages/paragraphs.

## Context Retrieval Stack

Reuse parsed document structure plus precomputed indexes from upstream skills. For each question:

1. **Intent classification:** `factoid | list_extractive | yes_no | comparison | summary_scope | definition | how_to | find_span`
2. **Hybrid retrieval:**
   - **Lexical (BM25):** over paragraphs + headings + table captions
   - **Semantic (embeddings):** top-K paragraphs by cosine similarity (default K = 16)
   - Re-rank combined; apply section graph (related headings, cross-refs) to expand to necessary supporting context.
3. **Scope filters (user can supply):** `document_ids[]`, `heading_ids[]`, `pages[]`, `entity_kinds[]`, `after_date` / `before_date`.

## Answer Format

Always produce a structured answer with:

```json
{
  "data": {
    "answer": {
      "question": "...",
      "intent": "factoid",
      "text": "...",
      "confidence": 0.96,
      "type": "direct | aggregated | inferred | unanswerable",
      "citations": [
        {
          "id": "c1",
          "document_id": "doc_A",
          "source": "contract.pdf",
          "page": 12,
          "heading_id": "h3-2-duties",
          "paragraph_id": "p212",
          "span_start": 18200,
          "span_end": 18550,
          "quote": "Licensee shall report usage within 15 days of month end."
        }
      ],
      "follow_up_questions": [
        "What are the usage reporting penalties if submitted late?",
        "Is this clause different in the 2024 amendment?"
      ],
      "unanswerable_reason": null
    }
  },
  "meta": {
    "skills_applied": ["document-parsing","document-content-analysis","document-query"],
    "retrieval_debug": {
      "chunks_scored": 2140,
      "citations_considered": 16,
      "citations_used": 2
    },
    "warnings": []
  }
}
```

### Citation Rules (Guarantees)

- Every direct answer contains **at least one citation** unless the answer is pure conversation framing (e.g., "Okay, let's look at your documents.").
- Each citation includes exact `quote` (original, unmodified text snippet) plus a machine pointer (page + heading + span) so consumers can highlight the location in UI.
- Aggregated/inferred answers label themselves as `type: inferred` and cite all inputs to the inference.
- `unanswerable` returns explicit reasons:
  - `not_in_collection`: all retrieval scores below threshold
  - `contradictory`: cite A and cite B disagree; list both
  - `ambiguous_question`: ask 1-2 clarifying sub-questions

## Multi-Document & Comparative Questions

For questions spanning documents (e.g., "Compare the indemnification clauses across the three MSAs"):

- Answer structured per-document with a summary table:
  - Row per document, columns for key attributes (cap, survival, carveouts).
  - Cell-level citations.
- Cross-doc contradictions surfaced at top under a `conflicts_found[]` array with citations on both sides.

## Conversational Session State

Support multi-turn:
- Accumulate explicit filters (scoping docs, sections) and implicit context (pronoun resolution against prior answers entities).
- Reset session with `forget_context: true` per user.
- Memory: retain recent answer entities + cited heading_ids; cap at last 10 turns.

## Scale Strategy (≤ 10,000 pages / big collections)

- Build section-level and paragraph-level FAISS/HNSW indexes one time per `document-intelligence` session; reused for all turns.
- Streaming retrieval: hybrid search returns re-ranked top-K under 2s for 1M-paragraph corpus (per latency SLA context of 1 min / 100 pages for whole-doc tasks; Q&A path is expected to be an order of magnitude faster).
- Caching: repeated question → same answer (by question hash + document set hash) cache hit.

## Error Handling

- Empty / no citations returned: fallback to broader retrieval, drop filters, lower threshold; if still nothing, `unanswerable` with explicit steps.
- Corrupted index: rebuild from raw envelope transparently; emit warning.
- Out-of-scope questions (legal/medical/financial advice disclaimers): append jurisdiction-appropriate disclaimer and mark `type: inferred` with low confidence if the question asks for advice, not factual lookup.

## E2E Test Set (QA Coverage)

- Legal contract Q&A: party names, indemnification caps, termination notice windows, payment terms (≥ 95% exact).
- Technical manual Q&A: procedure steps, parameter ranges, error codes, escalation paths (≥ 95% exact).
- Academic paper Q&A: sample sizes, p-value thresholds, methodology limitations, dataset citations (≥ 95% exact).
- Business report Q&A: quarterly guidance, operating margin %, named leadership changes (≥ 95% exact).

Telemetry logs: question intent, retrieval hits, answer type, confidence band, and explicit correctness when user supplies thumbs-up/down feedback for continuous improvement.
