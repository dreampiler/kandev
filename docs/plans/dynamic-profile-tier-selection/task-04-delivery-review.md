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

### Delivery

Fork PR **#44** (`dreampiler/kandev`, base `main`, head `kd/dyn-tier-selection`),
https://github.com/dreampiler/kandev/pull/44. Head at push: `937f1b9a4a`.

`origin/main` had advanced to `42f572f0fc` (the Hermes install-detection PR
already recorded as orthogonal: different packages, no shared file). It was
merged in rather than rebased, which produces the same squash-merge result on
this fork. The merge was clean, and `go build ./...` plus the dynamic and
settings-controller suites were re-run against the merged tree before pushing.

Nothing was pushed before this PR, and the installed runtime, the shared
checkout and the upstream repository were not touched.

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

### Three-model review, run 2 of 3 (DeepSeek 4.1 Flash, `openrouter/deepseek/deepseek-v4.1-flash`)

Snapshot `1d9b5b4727`, same fork base `c8d4c923c`. Receipts: route `openrouter`,
model `deepseek/deepseek-v4.1-flash`, one `opencode run` process, exit 0, no
provider fallback. It reported no blockers and ten findings, four of them real
defects this branch introduced.

#### Fixed

1. **Major — the tier failure direction was dropped on one write path.**
   `dynamicSelectionPayload` spread `selection.tier` verbatim into the wire body,
   so the request carried `onFailure` while the Go tag is `on_failure`. The
   decoder does not match the two, `normalizeDynamicTierPolicy` then applied the
   default, and a "next tier" choice was silently stored as "same tier next".
   The editor's own save path already emitted the correct name, so the two paths
   disagreed. The tier object is now spelled out field by field, and the
   round-trip test asserts `on_failure` instead of the shape that hid the bug.

2. **Major — a joined row's model options were never validated.**
   `normalizeDynamicSelection` returned early for a joined row, so its cost
   class, usage source, reserved share and manual windows were stored unchecked.
   A row owns its model options even when it does not own a tier, so they now go
   through `normalizeDynamicModelPolicy` on that branch too.

3. **Major — a malformed reset anchor was accepted at save time.**
   Validation checked only that the anchor was non-empty, short, and paired with
   a known timezone, so `"ninety"` or `"25:99"` saved successfully and then
   resolved to unknown usage at runtime. Save now parses the anchor with the
   same `agentusage.ParseResetAnchor` the runtime uses, turning a silent
   "usage unavailable" into a save error.

4. **Major — a partial token measurement was reported as a confident pace.**
   The money branch rejected a total with unpriced or incomplete events while the
   token branch ignored them. Both units now reject an incomplete total, so
   AC-003.4's partial-accounting rule holds for either unit.

#### Accepted as recorded residual risk

5. **Automatic usage is attributed to the host account for remote candidates**
   (`backendapp/usage_adapter.go`). Now implemented; see the eleventh increment.

6. **Preview cannot see route health.** `previewEligibility` marks only disabled
   rows ineligible, so a preview can name a candidate whose binding currently has
   an open circuit. AC-005.2 conditions preview/selection agreement on matching
   health inputs, and the preview provider has no health seam at all.
   - **Cause:** no circuit-health input on `DynamicPreviewProvider`.
   - **Impact:** a preview can disagree with the selection it predicts, during a
     circuit window only.
   - **Next owner:** this task, as a scoped follow-up work order.
   - **Resume condition:** the provider receives circuit state and the existing
     preview/selection parity test covers an open circuit.

7. **The recorded lower bound is not retained.** An incomplete money or token
   total returns unknown and drops the window, so `PaceScore.HasRecord` never
   becomes true for manual windows and the branch meant to keep a partial figure
   visible is unreachable. An existing test pins that behaviour, so fixing it
   changes an assertion this branch wrote.
   - **Cause:** `recordedFraction` returns only a fraction, not the recorded
     total, so the caller cannot distinguish "nothing recorded" from "partially
     recorded".
   - **Impact:** partial accounting is shown as plain unknown rather than as a
     labelled lower bound.
   - **Next owner:** this task, as a scoped follow-up work order.
   - **Resume condition:** the window result carries the recorded total and
     completeness separately from the fraction.

Findings 5 and 6 of the first review (no model-scoped window filter; keep-model
off having no runtime effect) are also confirmed by this run and remain recorded
above.

### Three-model review, run 3 of 3 (Opus 5.5)

Not run. Blocked on provider funding, not on model availability.

`.rv-brief.md` points at the final snapshot `e1d490931`. The
`openrouter/anthropic/claude-opus-5.5` route is routable — a no-model readiness
probe returned `OK` — but the full review is refused with "This request would
exceed your available credits given your current in-flight requests." Two
attempts were made, both exit code 1, the second after the E2E run drained
in-flight load as the error text advises. The other two configured routes for the
same model fail earlier still: `llmgateway/claude-opus-5-5` reports "Dev Plan
credit limit reached. Upgrade your plan or wait for renewal on 10/7/2026" and
`opencode/claude-opus-5-5` reports "Upstream request failed: Insufficient account
funds".

No review report was produced on any route, so there is no parser acceptance and
no inference or `modelUsage` receipt to record. The failed attempts cost nothing;
the one trivial readiness prompt is the only spend.

- **Cause:** provider funding exhausted on all three configured Opus 5.5 routes.
- **Impact:** the three-model review criterion is **not met**. It is not reduced
  to two, and PR #44 stays unmerged.
- **Next owner:** parent control, which owns the credential and cost decision.
- **Resume condition:** with funding restored, or another already-approved
  authenticated route confirmed, run the review once against `.rv-brief.md` at
  `e1d490931`, then verify its findings against the code the way the first two
  were handled, and record the actual model name, the route/inference/process
  receipts, `modelUsage`, parser acceptance and cost. The three-model completion
  criterion is not met until then.

 Formal RV, PR, runtime replacement and upstream publication were not
performed during planning.
