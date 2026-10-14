---
created: 2026-10-14
status: in_progress
requirements:
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-COSTS-PROVIDER-USAGE-001
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
  - ../../specs/costs/system-design/provider-account-usage.md
legacy_specs: []
---

# Implementation Plan: Retain first-tier candidates during usage gaps

## Overview

One fork PR bounds automatic usage by its original observation time and defers
partial reservation admission when usage is unknown. Recent valid cached values
continue to enforce reservations. Explicit full reservations and actual resource
exclusions retain their meaning. Agents owns admission; Costs owns usage facts.

## User decision and design alignment

Owner decision, translated from the Operations chat on 2026-10-14 at 03:3x KST:
"Proceed with 1, option B first, and implement option A afterward."
In that Operations decision, option B means setting the Opus/Sol operational
reserved shares from 2% to 0%; option A means this code repair. These labels differ
from the policy alternatives below. The task plan retains the original Korean
quote. This PR does not change or undo the operator setting.

The controller's checkpoint reply for task 0037ede3 approved policy B below,
the minimal owning-spec changes, and R5 fork-only classification. The original
task explicitly required this checkpoint reply before implementation.

The planning checkpoint read the design card and index once at
`_Prj_Knowledge/p200_index/` in the project knowledge workspace. Their indexed
integrated-design draft section 29.2 places unknown usage behind known usage
within a tier and advances only when the group is blocked. No numeric decision
ID for this repair was present. The previous Agents design explicitly excluded
unknown usage with a positive reserve, making the entire first tier disappear.
This repair supersedes that admission rule while preserving the section 29.2
pace and tier-order intent, account/profile isolation, visible operator control,
and isolated work copies. The deviation is from the former owning-design
exclusion, not from the card's isolation or recovery invariants.

## Scope and technical approach

- In `dynamic/ranking.go`, leave raw pace unchanged. Keep the 100% exclusion,
  but unknown evidence adds no exclusion for a 1-99% reserved share.
- In `backendapp/dynamic_usage_snapshot.go`, accept automatic evidence only
  when original `FetchedAt` age is nonnegative and less than 30 minutes, and
  existing account/model/window checks succeed. Gather the per-decision account
  readings before sampling the single injected decision clock, so a successful
  fresh lookup is not rejected against the clock from before its own fetch.
- Reuse the existing cache and observed-store response; do not add state,
  persistence, schedulers, flags, or provider requests.
- Verify `Engine.SelectContext` and the same preview ranking with mocked provider
  boundaries. Existing circuits, disabled/unusable bindings, quota suspensions,
  and transition chains still decide eligibility.

Current-code correction: cache.go already returns a recent success as stale
after a failed refresh, and usage_adapter.go returns it as `ok`. That preservation
case already works. The confirmed defects are the unknown-reservation exclusion
and the missing original-observation-age bound; do not invent a failing stale
case or repeat cache work delivered by fork PR #90.

| Evidence | Automatic score | Reservation admission |
| --- | --- | --- |
| Own valid fresh/cached observation, age under 30 minutes | Known, original timestamp | Apply observed threshold |
| No value, missing/future time, age at least 30 minutes, invalid window/scope | Unknown | Defer 1-99% check; exclude 100% |
| Remote execution with host-only automatic reader | Unknown; do not query host credentials | Existing binding/eligibility remains authoritative |
| Manual windows | Existing ledger policy | Same unknown/known admission rule |

## Alternatives

| Policy | Outcome and cost |
| --- | --- |
| A: Conservative unknown exclusion | Lowest change cost, but still drops an entirely unknown first tier; rejected |
| B: Defer partial reservation check | No extra state; retains unknown candidates and known-first ranking; approved |
| C: Short grace period | New timers/restart semantics and later recurrence of the same skip; rejected |

The 30-minute observation bound reuses Costs AC-001.5. The work order and owning
design retain sufficient rationale; a separate ADR adds no independent boundary.

## Tests

- `dynamic/ranking_test.go`: AC-003.5 unknown partial/full reserve and known threshold.
- `dynamic/reservation_selection_test.go`: AC-003.2/003.5 mixed known/unknown,
  entire unknown first tier, full reserve, threshold, disabled/circuit and preview parity.
- `backendapp/dynamic_usage_reservation_test.go`: AC-003.7 original timestamp
  boundaries, restart without readings, real cache success followed by 429,
  expired windows and model scope, flowing into actual selection.

No E2E, CI, new validator, full suite, repeated unchanged check, or baseline test
rerun is in scope. RED and GREEN are each run once per affected package, with the
production-code change between them. Record the recent-stale case as preservation.

## Work orders

- [ ] [Task 01: Retain the first eligible tier](task-01-retain-first-tier.md)

## Coordination and delivery

Classification: **R5: Dynamic profile tier selection**, fork-only. At planning,
read-only upstream/main `750a7a2613b141bb63f56b3b83576b76613f377c` lacks the
target ranking/snapshot and tier requirement files. No upstream PR or W091
handoff is warranted unless that boundary changes by PR time.

Potential overlap with cc6f8fe3 / fork PR #302: `ReservationExclusion`,
`profileScore`, and `applicableAutomaticWindows`. Do not touch its checkout or
branch. Fetch and normally merge latest origin/main before opening the PR,
preserving both behaviors if #302 lands first. No blanket predecessor link.

Report every PR head, base, file count, and actual verification results to the
controller. End the PR-opening turn. Step completion and merge require controller
GO and completed independent step-entry review with zero blocking findings.
This task does not delay the fourth swap; Operations owns post-swap route-record
confirmation. A verified fork merge before the cut is included without another
inclusion question.

## Verification results

Focused RED/GREEN, changed-package build/vet/lint, catalog/spec lint, and the
existing documentation coverage preflight are complete. An additional RED case
exposed fresh timestamps being compared to a pre-lookup clock; gathering readings
before one shared clock fixed it, and only the affected backendapp checks were
rerun after that change. Final lint passed after a one-off timeout extension and
normal shared-lock release. The public-doc validator reports an unrelated
existing MCP coverage gap, recorded as apparently pre-existing without a baseline
rerun. Exact commands and results are in the
[work order](task-01-retain-first-tier.md#results). Delivery and independent review
remain pending; this plan is not complete at PR creation alone.

## Risks

Unknown usage does not prove remaining quota; a partial reserve cannot be
guaranteed during an observation gap. The operator's current Opus/Sol reserves
are already 0%, reducing immediate reservation impact; this PR preserves them.
