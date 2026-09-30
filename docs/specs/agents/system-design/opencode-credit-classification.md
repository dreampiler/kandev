---
status: draft
system: agents
requirements:
  - REQ-AGENTS-OPENCODE-CREDIT-001
---

# OpenCode Credit-Exhaustion Classification System Design

## Purpose and boundaries

The agent system owns provider error classification. This design covers how an
`opencode-acp` credit-exhaustion stderr diagnostic is recognized and
classified. It does not change candidate ordering, the shared credential
binding circuit, reset-time parsing, or the classification rules of other
providers.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| `REQ-AGENTS-OPENCODE-CREDIT-001` | [Classification](#classification), [Failure and recovery](#failure-and-recovery) |

## Components and responsibilities

- **`internal/agent/runtime/routingerr` rules** own the per-provider regex
  table. The new `opencode.stderr.credit.v1` rule matches only the bounded
  credit-exhaustion phrases and yields a high-confidence `quota_limited`
  result with fallback allowed.
- **`routingerr.Classify`** owns the final classification result and reuses
  the existing quota routing policy unchanged.
- **`internal/agent/runtime/dynamic`** consumes the resulting quota
  classification unchanged, so dynamic profiles advance to the next candidate
  provider.

## Classification

The observed diagnostic names a credit plan and a renewal date, not a usage
period, so the existing period usage-limit rule does not match and the message
falls through to the post-start phase fallback with fallback disabled. The new
rule matches the bounded phrases only, anchored on word boundaries, and is
placed after the period usage-limit rule so an existing high-confidence match
keeps precedence. A bare "credit" is deliberately unmatched: provider text can
mention credits in an unrelated sentence, and only exhaustion wording justifies
the quota verdict.

Sanitization collapses the renewal URL and date before the message reaches the
classifier, so the rule matches on the exhaustion phrases alone and never on
the redacted URL or date segments.

## Failure and recovery

The rule only widens recognition; every other path is unchanged. A
non-matching diagnostic keeps the existing phase fallback. A matched
classification is fallback-eligible, so dynamic routing advances to the next
candidate provider on the next attempt.

## Observability

Classification results are already emitted as the existing
`agent failure classified for provider routing` log with the classifier rule
ID, so a matched credit diagnostic is distinguishable from other quota
matches.
