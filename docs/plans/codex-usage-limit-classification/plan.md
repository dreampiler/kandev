---
status: planned
created: 2026-09-28
requirements:
  - REQ-AGENTS-CODEX-USAGE-LIMIT-001
system_design:
  - ../../specs/agents/system-design/codex-usage-limit-classification.md
---

# Codex usage-limit classification

## Overview

Codex ACP reports an exhausted account usage limit as a plain agent message
with a typographic apostrophe and returns only `Internal error` from
`session/prompt`. The codex quota rule only matched machine tokens, so dynamic
routing classified the failure as unclassified and stopped instead of
advancing, and manual recovery did not recognize the notice. Turn the notice
text into a high-confidence, fallback-eligible quota classification that
carries the provider's retry time.

## Technical approach

Accept both apostrophe forms in the codex quota rule so the plain notice
classifies as `quota_limited` with high confidence and allows fallback. Add a
provider-independent text parser for the `try again at <month> <day>, <year>
<h:MM AM/PM>` retry time. Apply it in `Classify` only when the classification
is `quota_limited` or `rate_limited` and the structured `ResetHint` is absent,
so an adapter-supplied hint always wins. Normalize the typographic apostrophe
in the orchestrator's manual-recovery check so a queued prompt is retained.

A quota failure still opens the shared credential binding circuit, so sibling
profiles on the same account are skipped; a focused dynamic-engineering
regression proves that reuse is unchanged.

## Delivery order

1. Add failing classification tests for both apostrophe forms and for the
   structured-hint precedence.
2. Implement the codex quota rule and the reset-hint parser.
3. Add a failing manual-recovery test and implement apostrophe normalization.
4. Add the shared-binding sibling-skip regression and run the focused backend
   checks plus the specification checks.

## Verification strategy

- `go test ./internal/agent/runtime/routingerr ./internal/agent/runtime/dynamic
  ./internal/orchestrator -count=1` covers classification, reset-hint parsing,
  structured-hint precedence, shared-binding sibling skip, and manual recovery.
- `make -C apps/backend fmt` and `make -C apps/backend lint` close formatting
  and static analysis.
- `python3 scripts/lint-spec-files.py --all` and
  `python3 scripts/list-docs.py validate` cover the specification package.
- `git diff --check` covers whitespace.

## Work orders

- [ ] [Task 01: Classify codex usage-limit notices](task-01-classify-codex-usage-limit.md)

## Risks and exclusions

- The retry timestamp is a local wall-clock value; parsing must fail closed on
  any out-of-range component.
- A structured reset hint must never be overwritten by text parsing.
- No provider rule other than codex-acp changes, and candidate ordering and
  the shared credential binding circuit are untouched.
