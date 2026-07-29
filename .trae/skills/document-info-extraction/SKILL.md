---
name: "document-info-extraction"
description: "Extracts structured fields from parsed docs: names, dates, amounts, requirements, action items. Normalizes output to JSON/CSV. Invoke to harvest records for downstream systems, reports, or tables."
---

# Information Extraction

Pulls semantically typed data points from a parsed + analyzed document and normalizes them into machine-readable records. Use this skill whenever the user asks for "fields", "records", "lists of X", "export to JSON/CSV", or any downstream-integratable dataset.

## Supported Extraction Categories

### 1. Core Entity Fields (reuse Content Analysis where possible)

Always include unless explicitly excluded:

- People, Organizations, Locations — with canonical IDs + aliases
- Dates (ISO-8601 normalized, with granularity)
- Money, Percentages, Units (length, mass, duration, data volume) — SI-normalized `value` + `unit`
- Identifiers: invoice/contract/policy IDs, ISBN/DOI/SSN-like patterns (redacted when classified as PII)
- URLs, emails, phone numbers — RFC-normalized

### 2. Requirements & Obligations (`requirements`)

Legal / policy / SLA / specification documents:

```json
{
  "requirement_id": "uuid",
  "text": "...",
  "modal": "shall | must | should | may | prohibited",
  "subject_role": "buyer | seller | licensee | user | employee | ...",
  "deadline_date": "ISO-8601 or null",
  "condition_clause_text": "...",
  "citations": [{"section": "3.2", "heading_id": "h3-2"}],
  "risk_tags": ["financial", "compliance", "security"],
  "confidence": 0.98
}
```

### 3. Action Items & Tasks (`action_items`)

```json
{
  "action_id": "uuid",
  "owner_role_or_person": "...",
  "task_summary": "...",
  "verb": "deliver | review | approve | pay | notify | ...",
  "due_date": "ISO-8601 or null",
  "status": "open | in_progress | done | overdue | unknown",
  "prerequisites": ["action_id_a"],
  "citations": []
}
```

Detect using patterns: "X shall [verb]", "responsible party", "due by", "no later than", combined with claim graph from content-analysis skill.

### 4. User-Supplied Custom Schemas

If user provides a JSON schema like `{invoice_number, line_items[{sku,qty,price}], total_paid, vendor}`, run extraction against it. Required behavior:
- Strict type coercion; list extraction boundaries use section + table context.
- `required: true` fields that are missing are reported in `meta.warnings` with suggested context (nearest matches).

## Normalization Rules

All extracted records apply:

- **Date:** ISO-8601. Relative dates anchored to `document.metadata.modified_at` or user override.
- **Money:** `{value: number, currency: ISO-4217}`. Ambiguous symbols resolved by locale + surrounding context.
- **Identifiers:** canonical string lowercase/trim, with `raw_form` preserved.
- **Floats/units:** SI-base unit for values; keep `display_unit` for human readability.
- **Dedup:** Same semantic record (same person/date/action item) → one record, multiple `occurrences[]` references.

## Output Formats

Two top-level keys always present; select CSV export at user request:

```json
{
  "data": {
    "extraction": {
      "json_records": {
        "entities": { "...": "..." },
        "requirements": [],
        "action_items": [],
        "custom_records": {}
      },
      "csv_payloads": {
        "requirements.csv": "id,text,modal,section\n...",
        "action_items.csv": "...",
        "people.csv": "..."
      }
    }
  },
  "meta": { "...": "..." }
}
```

`csv_payloads` include BOM-safe UTF-8 and RFC-4180 quoting.

## Scale Strategy (≤ 10,000 pages)

- Per-chunk extraction with per-type local dedup; global dedup after chunk processing ends.
- Cross-chunk requirements: span merging when a sentence is split on a page break (use 15% overlap + sentence-assembly heuristic).
- CSV generation: streaming row writer; never hold all rows as single string. Report row counts per type in telemetry.

## Error Handling

- Missing expected fields: return partial; list missing + candidates in warnings.
- Ambiguous values: emit record with `confidence < 0.7` and `disambiguation_candidates[]` in meta.
- Custom schema coercion failures: return structured `validation_errors[]` per record with JSON-Pointer paths.

## Target Accuracy (≥ 95%)

For the 4 E2E doc types:
- Legal contracts: party names, effective/termination dates, dollar amounts, obligations count match manual gold.
- Technical manuals: SKU/part numbers, quantities, procedure step counts, KPI limits.
- Academic papers: author list, dates, funding grant IDs, methodology parameter tables.
- Business reports: named KPIs, fiscal period labels, forward-looking obligations.

All mismatch cases must be logged with code + context in telemetry for model retraining feedback loops.
