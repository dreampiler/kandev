---
id: "04-delivery-review"
title: "Complete focused evidence and three-model review before delivery"
status: pending
wave: 4
depends_on:
  - "03-settings-interaction"
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-002
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-AGENTS-TIER-SELECTION-004
  - REQ-AGENTS-TIER-SELECTION-005
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-001.1
  - AC-AGENTS-TIER-SELECTION-002.1
  - AC-AGENTS-TIER-SELECTION-003.1
  - AC-AGENTS-TIER-SELECTION-004.1
  - AC-AGENTS-TIER-SELECTION-004.2
  - AC-AGENTS-TIER-SELECTION-005.2
  - AC-AGENTS-TIER-SELECTION-005.3
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
---

# Task 04: Focused Delivery Evidence and Review

## Summary

Reconcile the actual implementation and focused results with this package,
document user behavior, and complete the explicitly requested three-model RV.
Hand off for the next replacement and subsequent upstream issue in that order.

## In scope

Result consolidation, docs-maintainer pass, actual defect corrections and their
affected checks, three-model review of the same final snapshot, fork-only
delivery handoff and recorded replacement prerequisites.

## Out of scope

Broad unrelated audits, new features, operational profile values, direct runtime
replacement and any upstream PR before the issue. Planning does not authorize
formal review calls or external publication in this turn.

## Acceptance

1. Each AC has actual focused evidence in the plan, including persisted
   settings/reasons, no-revisit chains and phone rendering; public help agrees.
2. Three distinct actual model reviews cover the same artifact and original
   requirements, with route/provider/process receipts, modelUsage and disposition
   of valid findings. Unavailable or empty review results remain incomplete.
3. Control receives the artifact locations, exact checks/results, open risks
   and fork integration state. Replacement and upstream issue remain ordered
   owner-controlled operations; no upstream PR precedes the issue.

## Verification

Reuse completed Task 01-03 evidence. Rerun only checks affected by review fixes,
changed inputs or a changed base. From repository root for final document changes:

```powershell
python scripts/list-docs.py validate
python scripts/lint-spec-files.py --all
node scripts/validate-public-docs.mjs
git diff --check
```

Use the repository's local `pr-docs.cjs::validateCoverage` preflight for complete
work-order references. Read `/docs-maintainer` before public doc edits.
Review scope: legacy/round-trip compatibility, grouping mutations, numeric/reset
and account correctness, policy precedence and durable exclusions, preview parity,
mobile behavior and settings lock. The parent supplies the then-available formal
three-model execution route; record its exact invocation and actual model names
at that time. Do not invent a command or mark this step done based on three
model names in prose. No subagent or separate session is authorized by this file.

## Files likely touched

- This plan and work-order result sections
- Paired draft requirements/design, promoted only when implemented and verified
- `docs/public/office-provider-routing.md` and the existing dynamic-profile help
  located by `/docs-maintainer`
- Only implementation files with a demonstrated requirement violation/regression

## Dependencies and risks

Requires all previous work orders and their evidence. Formal RV availability,
review cost and replacement authority are control responsibilities; unavailable
review does not invalidate completed local checks but prevents delivery completion.
Fork PR/integration follows the then-current authorized workflow. Upstream issue
publication needs explicit authority after replacement, not an automatic tool call.

## Results

Pending. Formal RV, PR, runtime replacement and upstream publication were not
performed during planning.
