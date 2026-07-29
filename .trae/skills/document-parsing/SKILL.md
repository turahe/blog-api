---
name: "document-parsing"
description: "Parses multi-format docs (PDF/DOCX/TXT/HTML/JSON/XML/MD) extracting text, metadata, structure, and media references. Invoke whenever raw document bytes must become a structured envelope."
---

# Document Parsing & Ingestion

Converts raw document bytes in supported formats into a single, canonical `DocumentEnvelope` used by all downstream skills. This is the **first step** in any document-processing pipeline; call it before analysis, extraction, summarization, compliance, or Q&A.

## Supported Input Formats (Allowlist)

| Format        | Extensions                 | Primary method            | Fallback / recovery                       |
|---------------|----------------------------|---------------------------|-------------------------------------------|
| PDF           | `.pdf`                     | Native text extraction    | OCR if text layer empty or < 50 chars/page |
| DOCX / OOXML  | `.docx`, `.dotx`           | XML unzip + parse         | Plain-text salvage on malformed ZIPs      |
| Plain text    | `.txt`, `.md`, `.log`      | Encoding sniffer (UTF-8/16/32, latin-1) | Re-sample chunks on decode errors |
| HTML          | `.html`, `.htm`            | DOM tree parse, strip tags | Boilerplate removal via text-to-tag ratio |
| JSON          | `.json`                    | RFC 8259 JSON parser      | JSONLines detection + line-scope recovery |
| XML           | `.xml`                     | XML parser + schema sniff | Line-by-line salvage on ill-formed close tags |
| Markdown      | `.md`, `.markdown`         | CommonMark AST            | Plain text pass if AST parser aborts      |

Unsupported formats (e.g., `.doc` binary, images without OCR pipeline) fall into the `unsupported_format` error class per the Document Intelligence umbrella standard.

## Extraction Targets

For every successfully parsed document, populate these fields inside `data.document`:

1. **Full text** (`full_text`): Complete text stream in reading order, with page/chunk anchors in `text_spans` for downstream citation.
2. **Metadata** (`metadata`):
   - `title`, `author`, `subject`, `keywords`, `creator`, `producer` (native when present; inferred otherwise)
   - `created_at`, `modified_at`, `content_type`, `content_language` (BCP 47)
   - `page_count`, `word_count`, `character_count`
   - `detected_encoding`, `source_hash_sha256`
3. **Structural elements** (`structure`):
   - Headings (`h1…h6` with id, text, level, page, span_start/end)
   - Paragraphs (`p` with id, text, page, parent_heading_id)
   - Lists: ordered (`ol`) and unordered (`ul`) with items, nesting depth
   - Tables: 2-D JSON array `cells[row][col]` with header flags, merged-cell metadata, caption
   - Footnotes / endnotes with anchors back to referencing paragraph
4. **Embedded media references** (`media_refs`):
   - `id`, `kind` (image, chart, attachment, audio, video)
   - `caption`, `alt_text` (when present)
   - `content_type`, `size_bytes`, `inline_position`
   - For extractable assets: `storage_key` or local blob reference; mark `redacted: true` if privacy rules prevent extraction.

## Canonical Output Shape

```json
{
  "data": {
    "document": {
      "document_id": "uuid",
      "full_text": "...",
      "metadata": { "...": "..." },
      "structure": {
        "headings": [{"id":"h1","level":1,"text":"Intro","page":1,"start":0,"end":400}],
        "paragraphs": [{"id":"p1","text":"...","parent_heading_id":"h1","page":1}],
        "lists": [{"id":"l1","ordered":true,"items":[{"text":"...","depth":0}]}],
        "tables": [{"id":"t1","caption":"...","cells":[[{"text":"A","header":true}]]}]
      },
      "media_refs": [{"id":"m1","kind":"image","caption":"..."}],
      "text_spans": [{"page":1,"start":0,"end":2800}]
    }
  },
  "meta": {
    "document_id": "...",
    "source": "...",
    "page_count_or_chunk_count": 10,
    "skills_applied": ["document-parsing"],
    "telemetry": [{ "skill": "document-parsing", "...": "..." }],
    "warnings": [
      {"code": "ocr_fallback", "message": "Pages 3-5 required OCR; confidence 0.87"},
      {"code": "table_merged_cells", "message": "Table t4 has non-rectangular spans; rendered best-effort"}
    ]
  },
  "error": null
}
```

## Large-Document Strategy (≤ 10,000 pages)

- **Streaming parse:** Process pages/chunks sequentially; keep only current + previous page in memory for cross-page paragraph reassembly.
- **Chunk size:** Target ≤ 8k tokens; 15% overlap across chunk boundaries to avoid splitting sentences.
- **Parallel sub-steps:** Metadata extraction and media-ref enumeration run on already-parsed pages while subsequent pages continue parsing.
- **Progress:** Emit `meta.warnings` or intermediate telemetry entries every 1000 pages so the orchestrator remains aware of liveness.

## Error Handling

| Class               | Trigger                                                      | Recovery                                                    |
|---------------------|--------------------------------------------------------------|-------------------------------------------------------------|
| corrupted_file      | Open/parse aborts mid-stream, checksum mismatch             | Re-read with permissive parser; return partial envelope + `recovery_attempts: ["..."]` |
| unsupported_format  | MIME/extension not in allowlist and no sniffer match         | List sniffed types and extensions tried; suggest converter. |
| unreadable_content  | Format valid but zero text and OCR unavailable or low conf   | Return envelope with metadata only; mark `confidence: < 0.3`. |

Always log byte-level stats (bytes_read_successfully / total_bytes) in error.details.

## Validation & Test Cases

Every invocation of this skill is expected to self-check:

- Structural integrity: every paragraph references a heading id that exists or is null (orphans reported as warnings).
- Table cells all have the same number of columns per row; non-rectangular tables emit a warning and are padded with `null`.
- Reconstruction check: concatenating the paragraph+list+table+heading texts should equal ~100% of `full_text` (± boilerplate/footnotes).

### E2E Test Suite

- **Legal contract (PDF):** Correctly captures defined-terms headings, numbered clause lists, signature-block tables, and exhibits.
- **Technical manual (DOCX):** Accurate heading hierarchy, procedure step lists, parameter tables, and figure captions.
- **Academic paper (PDF):** Abstract/introduction/methods/results sections, reference list structure, table/figure bounding.
- **Business report (HTML export):** Boilerplate removal, KPI table extraction, section levels.

Success criteria for parsing-layer accuracy feed into downstream ≥ 95% entity and ≥ 90% summary capture targets.
