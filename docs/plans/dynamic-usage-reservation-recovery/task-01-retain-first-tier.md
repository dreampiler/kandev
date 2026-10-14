---
id: "01-retain-first-tier"
title: "Retain the first eligible tier during usage gaps"
status: in_progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-COSTS-PROVIDER-USAGE-001
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-003.2
  - AC-AGENTS-TIER-SELECTION-003.5
  - AC-AGENTS-TIER-SELECTION-003.7
  - AC-COSTS-PROVIDER-USAGE-001.5
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
  - ../../specs/costs/system-design/provider-account-usage.md
---

# Task 01: Retain the first eligible tier during usage gaps

## Summary

Use recent existing account observations for reservations and defer partial
reservation exclusion without usable evidence. Preserve actual eligibility
restrictions and verify the live selection boundary and preview.

## In scope

The two production predicates, source-adjacent regression tests, minimal
existing Agents/Costs contract amendments, and a short existing public-guide
explanation. Follow the controller's policy B
checkpoint in the task plan and [plan rationale](plan.md#user-decision-and-design-alignment).

## Out of scope

Live configuration/DB/deployment, credential-file reads, a second cache or
persistence, tier/model reordering, shared/upstream checkout writes, full or
repeated tests/builds, baseline test reruns, E2E, CI changes, new gates/tools,
stash, force push, and manual run_code_review.

## Acceptance

1. Recent valid cached evidence enforces reservation; no-value/expired evidence
   remains unknown and retains partially reserved first-tier candidates.
2. Full reserve, known thresholds, scope/window filtering and actual eligibility
   remain effective; live selection and preview agree without changing raw pace.
3. Focused checks pass and the fork PR reports evidence, risk, classification and
   overlap before controller-directed independent review and integration.

## Verification

From `apps/backend`, execute each command separately. Run new/changed regression
names once for RED, then the focused GREEN sets once after production edits.

```powershell
go test -trimpath ./internal/agent/runtime/dynamic -run '^(TestReservationExclusion|TestReservationSelection.*)$' -timeout=10m
go test -trimpath ./internal/backendapp -run '^TestDynamicUsageReservation.*$' -skip '^(TestBindBootstrapListenersServesLivenessBeforeSwap|TestBindBootstrapListenersEchoesDesktopHealthToken|TestHandlerSwitchSwapsWithoutRebind|TestBindBootstrapListenersFailsWhenPortUnavailable|TestCloseBoundListenersReleasesPortOnStartupFailure|TestCloseBoundListenersNoopWhenListenersNil)$' -timeout=10m
```

```powershell
go test -trimpath ./internal/agent/runtime/dynamic -run '^(TestReservationExclusion|TestReservationSelection.*|TestRankTier.*|TestPaceFromWindows.*|TestDynamicTierTransitionChainRanksOnlyTheFirstTierWithACandidate|TestDynamicTierTransitionChainUnknownUsageChoosesAmongPeers|TestPreviewSelectionMatchesTheEngineForTheSameInputs)$' -timeout=10m
go test -trimpath ./internal/backendapp -run '^(TestDynamicUsageReservation.*|TestSnapshot.*|TestApplicableAutomaticWindows.*|TestRowWithoutManualWindowsRanksOnTheProfileUsage|TestExhaustedWindowWithoutResetRanksAsBusy)$' -skip '^(TestBindBootstrapListenersServesLivenessBeforeSwap|TestBindBootstrapListenersEchoesDesktopHealthToken|TestHandlerSwitchSwapsWithoutRebind|TestBindBootstrapListenersFailsWhenPortUnavailable|TestCloseBoundListenersReleasesPortOnStartupFailure|TestCloseBoundListenersNoopWhenListenersNil)$' -timeout=10m
go build -trimpath ./internal/agent/runtime/dynamic ./internal/backendapp
go vet ./internal/agent/runtime/dynamic ./internal/backendapp
golangci-lint run ./internal/agent/runtime/dynamic ./internal/backendapp --new-from-rev=c6b90cd1c7 --timeout=10m
```

Use the actual PR base SHA if latest origin/main changes. From repo root, use
existing `python scripts/list-docs.py validate`, `python scripts/lint-spec-files.py --all`,
and `git diff --check` for edited records. No new validation system is introduced.
The six bootstrap/listener tests are explicitly skipped and outside the run set.

## Files likely touched

- `apps/backend/internal/agent/runtime/dynamic/ranking.go`
- `apps/backend/internal/agent/runtime/dynamic/ranking_test.go`
- `apps/backend/internal/agent/runtime/dynamic/reservation_selection_test.go`
- `apps/backend/internal/backendapp/dynamic_usage_snapshot.go`
- `apps/backend/internal/backendapp/dynamic_usage_reservation_test.go`
- Existing Agents tier-selection requirement/design and Costs provider-usage design.

## Dependencies

Controller checkpoint reply received; no preceding work order. Normally merge
latest origin/main before PR. Preserve #302's behavior if it has landed.

## Risks

Partial reserved capacity cannot be guaranteed while usage is unknown. Preserve
the existing operational 0% reserve setting and actual resource exclusions.

## Parallelism

Sequential; no delegation.

## Inputs

Agents AC-003.2/003.5/003.7, Costs AC-001.5, existing shared usage cache/adapter,
`chain_test.go`, `preview_test.go`, and account-bound snapshot fixtures.

## Results

| Check | Result |
| --- | --- |
| Focused dynamic RED command above | Expected failure: unknown partial reserve excludes the first tier |
| Focused backendapp RED command above | Expected failure: absent/invalid/expired observations and first-tier choice |
| Focused dynamic GREEN command above | Passed, exit 0 |
| Additional backendapp RED, `-run '^TestDynamicUsageReservationFreshFetchUsesOnePostLookupClock$'` with the same skip/timeout flags | Expected failure: the pre-lookup clock rejects a fresh response as future-dated |
| Focused backendapp GREEN command above | Passed on final code, exit 0; recent 429/stale cases are preservation coverage |
| Changed-package build and vet commands above | Passed, exit 0; only backendapp was rechecked after its clock correction |
| Initial changed-package lint with `--timeout=5m` | Exit 1: reported `0 issues` then timeout; not a pass |
| Final changed-package lint with `--timeout=10m` | Passed, exit 0, `0 issues`; an earlier retry was refused by the shared lint lock |
| Documentation catalog and specification structure checks | Passed, exit 0 |
| Existing PR documentation coverage preflight on the changed files | Covered, no errors |
| Public-doc validator | Appears pre-existing: `coverage.json does not account for MCP tool: report_to_handoff_source_kandev`; no baseline rerun |
| Latest origin/main normal merge | Already up to date at `c6b90cd1c7bdda85ddd59d30fc3172e91085b298` |

No test was repeated without a code change, and no full suite/build or E2E ran.
The backendapp rerun follows a production/test change: collect profile readings
before sampling one decision clock, preserving both fresh timestamps and one
shared pace time. The unchanged dynamic package's passing test was reused.
The lint time-budget extension is a one-off execution adjustment after an actual
timeout; package scope, comparison SHA, and lint thresholds are unchanged. The
shared lock was allowed to release normally. Both hooks are already active;
local workspace dependencies were installed for commitlint without changing
shared hook configuration.

PR-stage controller report and independent review remain pending and separate
from local implementation checks. No merge or step_complete before controller GO.
