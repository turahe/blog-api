---
name: "document-compliance-checker"
description: "Scans documents against rules/policies, flags violations (zero false negatives in controlled tests), and emits detailed audit reports. Invoke for policy, legal, security, or standards audits."
---

# Compliance & Policy Checking

Validates one or more parsed documents against a ruleset. Hard guarantee: in controlled test cases, **zero false negatives** (false positives allowed but must be justified). Produces violation-level findings plus an executive audit report.

## Ruleset Model

Rules are provided in a structured format (JSON/YAML DSL or plain-language rules mapped internally). Core rule types supported:

```yaml
rules:
  - id: GDPR-ARTICLE-5-1a
    severity: critical
    description: Lawfulness, fairness and transparency — processing must be lawful, fair, and transparent.
    applicability:
      doc_types: [privacy_policy, vendor_contract, data_processing_agreement]
      tags: [personal_data]
    checks:
      - type: must_contain_phrase_set
        params:
          all_of: ["lawful basis", "transparency", "data subject rights"]
      - type: must_have_entity_kinds
        params:
          kinds: [organization]   # controller / processor must be named
      - type: max_retention
        params:
          scope: personal_data
          max_days: 1825

  - id: COMPANY-PROC-014
    severity: high
    description: All contracts over $1M require two authorized signatures and legal review flag.
    applicability: {doc_types: [contract], tags: [signature_block]}
    checks:
      - type: threshold_qualified_signatures
        params: {min_count: 2, min_amount_usd: 1000000}
      - type: must_contain_section
        params: {heading_pattern: "(?i)legal review|approval"}
```

### Built-in Rule Library (Always Available)

- **GDPR base pack:** 17 high-severity + 33 medium markers (Art. 5-8, 12-23, 30-33, 35 DPIA triggers).
- **SOC 2 / ISO 27001 policy base pack:** access control, change management, incident response, encryption-at-rest/in-transit phrase & entity checks.
- **Standard contract hygiene:** signature blocks, effective/termination dates, governing law, indemnification cap present, non-assignment or assignment-with-consent language.
- **Internal policy pack (user extendable):** spending limits, delegation-of-authority matrices, brand voice / forbidden terminology.

## Scan Methodology

1. **Applicability gate:** Skip rules not matching doc_type / tags.
2. **Content-level checks:** phrases, entity kinds, section presence, pattern regex.
3. **Semantic checks (LLM-backed):** requirement-level alignment with rule intent (e.g., "retention no longer than necessary" → scan retention clauses for reasonableness).
4. **Cross-document checks (optional):** referenced sub-documents present and consistent.
5. **Two-pass for zero-FN:** rules run once passively, then again on inverted negation patterns ("*no* retention clause stated" → flagged explicitly) so that silence does not escape notice.

## Findings Model

```json
{
  "finding_id": "uuid",
  "rule_id": "GDPR-ARTICLE-5-1a",
  "severity": "critical | high | medium | low | info",
  "status": "fail | warn | pass | needs_review",
  "title": "Missing lawful-basis description",
  "description": "...",
  "evidence": [
    {"document_id":"...","heading_id":"...","text_span":"[0..1200]","quote":"..."}
  ],
  "suggested_fix": "Add a section naming each processing purpose and its lawful basis (Art. 6).",
  "false_positive_likelihood": 0.08,
  "citations": { "regulation_refs": ["GDPR Art. 5(1)(a)", "Art. 6"], "policy_refs": ["COMPANY-PROC-014"] }
}
```

Critical guarantee: `severity ∈ {critical, high}` with `status = fail` must always have at least one concrete `evidence` span or an explicit `evidence_absent_reason` with rule scope. Silence = fail for controlled rules.

## Audit Report

Structured + machine-readable + human-friendly:

1. Executive: overall pass/fail, counts by severity, risk-weighted score.
2. Scope: documents scanned, ruleset version, applicability filter used.
3. Findings table with links to evidence + remediation owners.
4. Remediation roadmap: grouped remediation actions ordered by effort × severity.
5. Attestation block: timestamp, skill version, hash of doc set + ruleset for reproducibility.

## Output Envelope

```json
{
  "data": {
    "compliance": {
      "ruleset_version": "gdpr-pack:1.2 + company-proc:0.9",
      "findings": [],
      "audit_report": {
        "executive": { "...": "..." },
        "scope": { "...": "..." },
        "remediation_roadmap": []
      }
    }
  },
  "meta": { "...": "..." }
}
```

## Zero False Negative Assurance

**Protocol for controlled test cases (must pass 100%):**
- For every rule, supply at least one positive-control document (has the required element) and one negative-control document (silent on the required element).
- Skill must tag the negative-control as `fail`, positive-control as `pass`, on severity-critical rules. If `needs_review` is emitted, escalate to human; never silently downgrade to `pass`.

In production, a rule hit is downgraded only when there exists:
- An explicit opt-out override flag in the ruleset, or
- A cross-document reference containing the required clause (recorded in findings scope).

## Scale Strategy (≤ 10,000 pages)

- Rules applicability filter applied per section; sections irrelevant to rule skipped.
- Regex + phrase rules run as streaming scan; semantic rules per chunk with aggregation.
- Large collections: per-doc scan → merge dedup findings by rule_id + identical evidence → emit consolidated report.

## Error Handling

- Ruleset parse errors: abort; list rule_ids with line numbers and schema violation messages.
- Doc parse failures: mark doc `status = unevaluated` in scope; findings block notes reason.
- Low confidence NLP on a rule: emit `needs_review` with explicit reason, never `pass`.

Telemetry per Document Intelligence standards tracks: rules evaluated, findings per severity, needs_review ratio, reproducible hash, latency per rule type.
