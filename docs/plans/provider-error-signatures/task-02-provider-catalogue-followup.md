---
id: "02-provider-catalogue-followup"
title: "Match remaining bounded provider text signatures"
status: done
wave: 2
depends_on: ["01-match-provider-signatures"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-PROVIDER-ERROR-SIGNATURES-001
acceptance_criteria:
  - AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.6
  - AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.10
  - AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.8
  - AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.9
system_design:
  - ../../specs/agents/system-design/provider-error-signatures.md
---

# Task 02: Remaining provider text signatures

## Outcome and boundary

Classify OpenCode's unlabelled free-usage line, name its two daily rate-limit
reasons, recognize its generic Zen retry-later line, and recognize the bounded
provider-neutral labeled error_type envelope. Task 01's completed prose-safety
work and historical verification remain intact.

This is an independent main-based increment. The shared fifteen-signature
catalogue and prompt-context guard are already submitted separately in #4738;
their unmerged code is not copied or required for this increment. Structured
ACP metadata remains #4601's separate contract.

Existing typed and normalized failures and higher-priority credit, payment and
quota rules keep precedence. Only explicitly marked daily patterns inspect raw
diagnostic text within the same ordered provider catalogue; recorded excerpts
remain sanitized. Provider IDs and opening-line evidence remain authoritative.

No new API, schema, retry policy, budget/reset scope, prefilter, assistant-notice
channel, setting or installed-provider operation is added. Daily
rule IDs describe the observed reason; they do not implement account/model
suspension until a new reset window. Retry-later wording supplies no reset.

## Implementation

- routingerr/rules.go: scoped signatures and ordered optional raw matching.
- routingerr/routingerr.go: pass raw text beside its sanitized excerpt.
- routingerr/provider_neutral_rules.go: labeled error_type, preserving field order independence and the existing status prefix.

All source paths are under apps/backend/internal/agent/runtime.

## Verification

The original source reproduced six missing named classifications: labeled JSON
with message-first field order and the existing status prefix, unlabelled free
usage, both named daily reasons, and Zen retry-later. The daily cases already
classified as generic rate limits; this change retains that category and names
the observed reason.

The candidate passed the disposable cases and the selected existing classifier,
provider catalogue, typed priority, prose, runtime-rule, normalization, reset
and routing-policy controls with the race detector (2.625s). Stored excerpts
are compared against the existing sanitized stderr-plus-newline-plus-stdout
contract. No installed provider or operating policy change is claimed.

The unmodified Windows-only fixture
TestRemediateNpxCache_RemovesHashDirAndPreservesParent failed on both baseline
and candidate: it sets HOME to a temporary directory, while Windows resolves
the real user profile and correctly refuses that outside-cache path. Full
package success is not claimed. That unrelated remediation code and protection
remain unchanged; focused classifier validation is the result above.

Normal commit hooks and updated-head hosted CI are separate publication results.
Combined behavior with unmerged #4601/#4738 source has not been executed and is
not a prerequisite or pass claim here. Disposable cases remain outside automatic
repository collection.

The new daily-reason criterion is 001.10. Existing criterion 001.7 in #4738
continues to mean guarded context-length recovery; it is not redefined here.

## Committed regression coverage

The existing signature_rules_test.go now includes two table-driven tests with
25 cases for the new signatures, accepted wrappers and JSON field orders,
non-diagnostic prose, typed and text priority, sanitized joined stderr/stdout,
and unknown retry-later reset. The integrated main-based classifier suite passed directly from the repository
with fts5 and race detection (3.962s), including the upstream Codex retry and
interactive-policy controls. No test framework or scheduled job is added.
