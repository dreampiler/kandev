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

### Three-model review, run 1 of 3 (GPT-6 Sol, `openrouter/openai/gpt-6-sol`)

Snapshot `2c83becce7`, fork base `c8d4c923c`, 83 files changed. Receipts: route
`openrouter`, model `openai/gpt-6-sol`, one `opencode run` process, exit 0, no
provider fallback. The run produced 14 findings; each was verified against the
code rather than accepted on its word.

The reviewer answered in Korean and its console transcript was written through a
lossy code page, so the prose is partly unreadable. Every finding's severity,
`file:line` and claimed trigger survived, and those were the basis for
verification.

#### Fixed

1. **Blocker — money allowance scaled by the wrong constant.**
   `dynamic_usage_snapshot.go` multiplied a USD limit by 100 before dividing a
   `cost_subcents` total. The ledger stores hundredths of a cent
   (`internal/common/costs/pricing.go`), so one dollar is 10,000 of them. A $1
   allowance reported as fully used at $0.01, and every money pace was 100x its
   true value. The existing test asserted 5,000 subcents against a $100 limit is
   half, which encoded the same wrong constant and so passed. Now scaled by a
   named `subcentsPerDollar`, with the tests corrected and a new
   `TestRecordedFractionUsesTheLedgerSubcentUnit` pinning the unit against the
   ledger's contract.

2. **Blocker — an explicit keep-model `false` was stored as `true`.**
   `profile_crud.go` hardcoded `KeepModelWhileRunning: true` on update. Now
   resolves the request through the same `keepModelWhileRunning` helper the
   create path uses, with
   `TestDynamicProfileUpdateStoresTheRequestedKeepModelPreference` covering both
   the explicit `false` and the omitted-preserves case.

3. **Blocker — the unclassified fallback ignored tiers and dropped the chain.**
   `firstSelectableUnclassifiedSuccessor` scanned the flat candidate list and
   `unclassifiedSuccessorRoute` built a fresh route state with no
   `PolicyStateJSON`, so `next_tier` could select a peer it was meant to skip and
   the durable chain was lost on every unclassified transition. The design
   requires this path to "consume the same tier selection without widening
   admission". It now runs the ordinary `ResolveFallbackSelection` plan and
   `firstSelectable` against the unclassified path's own narrower eligibility
   map, carries the chain forward, and persists the bounded tier reason with its
   direction suffix. `unclassified_tier_test.go` pins both directions and the
   exhausted-chain case. The usage snapshot is taken before the engine lock,
   which is where it had to move once the shared selector needed it.

#### Rejected

- **"A merge must keep the lower tier's policy"** (`dynamic-agent-tiers.ts`).
  AC-001.4 states the opposite: "A merge uses the upper tier's policy." Model
  settings, which AC-001.4 does protect, travel with the concrete candidate.
- **"A calendar-month window double-counts the previous month"**
  (`usage/windows.go`). With an anchor before today the window is
  `[previous month's reset, this month's reset)`, which is the period the
  allowance actually covers.
- **"TierSearchOrder must honour the stored LastTierHeadID"** (`chain.go`).
  The order is derived from the failing candidate's own tier, which is the same
  boundary and cannot drift from a stored field.
- **"An incomplete money total is reported as a usable fraction."**
  `recordedFraction` returns unknown when `UnpricedCount` or `IncompleteCount`
  is non-zero, and `TestSnapshotManualIncompleteTotalsStayUnknown` pins it.

#### Accepted as recorded residual risk

5. **Automatic usage does not filter a window by the candidate's model.**
   `automaticScore` calls `window.UsableFor("")`, so an identified window scoped
   to a different model on the same account is accepted. `dynamic.Candidate`
   carries no model ID, so closing this means plumbing one through the resolver,
   the engine and the snapshot. It is a real gap against AC-003.3's "match the
   candidate's actual supported binding", but it is a cross-layer change that
   should not be made as an unverified late fix.
   - **Cause:** no model identity reaches the ranking seam.
   - **Impact:** an account whose provider reports model-scoped windows can score
     a candidate against a sibling model's consumption. Account isolation itself
     is correct: each candidate reads only its own binding.
   - **Next owner:** this task, as a scoped follow-up work order.
   - **Resume condition:** plumb the candidate's model ID and filter on it, with
     a case where a sibling model's window is rejected as unknown.

6. **`keep_model_while_running: false` has no runtime effect yet.**
   The stored preference round-trips and is honoured in the preview, but no idle
   re-evaluation path reads it. The reviewed plan puts re-evaluation at the next
   idle user-turn boundary, which the conductor does not implement.
   - **Cause:** the idle-turn comparison was not wired.
   - **Impact:** turning the switch off stores the preference without changing
     behaviour. The public guide and the settings copy were corrected so neither
     promises a mid-session change.
   - **Next owner:** this task, as a scoped follow-up work order.
   - **Resume condition:** the idle user-turn boundary compares capacity when the
     preference is off, with a case proving an admitted turn is never interrupted.

### Outstanding

Reviews 2 and 3 (Opus 5.5 and DeepSeek 4.1 Flash) have not run, so this is not a
completed three-model review. Two of the three reviewed models remain, and a
finding they raise may change the material above.
 Formal RV, PR, runtime replacement and upstream publication were not
performed during planning.
