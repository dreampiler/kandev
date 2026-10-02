---
id: "02-tier-routing"
title: "Select tier candidates from usage with durable transition chains"
status: in_progress
wave: 2
depends_on:
  - "01-selection-contract"
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-AGENTS-TIER-SELECTION-004
  - REQ-AGENTS-TIER-SELECTION-005
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-003.1
  - AC-AGENTS-TIER-SELECTION-003.2
  - AC-AGENTS-TIER-SELECTION-003.3
  - AC-AGENTS-TIER-SELECTION-003.4
  - AC-AGENTS-TIER-SELECTION-003.5
  - AC-AGENTS-TIER-SELECTION-003.6
  - AC-AGENTS-TIER-SELECTION-004.1
  - AC-AGENTS-TIER-SELECTION-004.2
  - AC-AGENTS-TIER-SELECTION-004.3
  - AC-AGENTS-TIER-SELECTION-004.4
  - AC-AGENTS-TIER-SELECTION-004.5
  - AC-AGENTS-TIER-SELECTION-005.1
  - AC-AGENTS-TIER-SELECTION-005.2
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
---

# Task 02: Usage-Aware Tier Routing

## Summary

Wire usage observations, manual window totals and pure tier ranking into the
shared resolver/conductor. Persist chain exclusions and route reasons using
existing transactional route state; expose the same computation as a read-only
settings preview.

## In scope

Window durations/reset validity, candidate/account applicability, manual ledger
query, order/pace/cost comparators, failure direction, chain lifecycle, healthy
stickiness and preview authorization. Join the usage event's turn to
`task_session_turns.execution_profile_id` for concrete attribution; never aggregate
the logical dynamic profile as though it were the concrete candidate. A proven
gap in that existing contract must be reported before expanding the ledger schema.

## Out of scope

New provider clients, account discovery by guessing, scheduler polling, global
mode, billing-system replacement, UI, runtime configuration and upstream work.
Do not change the existing effect-safe or unclassified-fallback admission scope.

## Acceptance

1. Numeric pace, manual calendar totals, unknown ranking, explicit free/no-window
   and configured cost/reservation behavior match the reviewed design decisions;
   observed usage belongs to the candidate actually executing.
2. Policy wait/retry/stop precedes tier direction; exclusions survive skip,
   restart and profile edits; healthy work stays on its model; racing failures
   produce at most one successor with existing continuation safety.
3. Preview matches the decision for identical inputs without route/health/save
   mutation, and committed attempts record the selection reason.

## Verification

From `apps/backend/`, run sequentially:

```powershell
go test ./internal/agent/usage -count=1 -timeout=10m
go test ./internal/agent/runtime/dynamic ./internal/agent/runtime/routingpolicy -count=1 -timeout=10m
go test ./internal/agent/runtime -run 'Test.*Dynamic' -count=1 -timeout=10m
go test ./internal/task/repository/sqlite -run 'Test.*(DynamicRoute|DynamicTier|DynamicManual|Usage)' -count=1 -timeout=10m
go test ./internal/task/usage -count=1 -timeout=10m
go test ./internal/backendapp ./internal/orchestrator -run 'Test.*Dynamic' -count=1 -timeout=10m
go test ./internal/agent/settings/controller -run 'Test.*Dynamic' -count=1 -timeout=10m
```

Use the plan's focused backend lint command after changes settle. Run the
PostgreSQL query counterpart with an isolated `KANDEV_TEST_POSTGRES_DSN`:

```powershell
go test ./internal/task/repository/sqlite -run 'TestPostgres.*DynamicManualWindow' -count=1 -timeout=10m -v
```

Its environment-gated skip does not satisfy parity evidence.
Add the plan's named cases to existing suites: clock floor/worst window,
unknown/free ordering, valid/invalid reset, manual boundary/partial observations,
two logical sessions on one concrete model, A-B-C failure chain, same-tier versus
next-tier, restart, duplicate events, stale claim, policy stop and preview no-write.
Inspect actual persisted route state/reasons, not only emitted log text.

## Files likely touched

- `apps/backend/internal/agent/usage/types.go`, `client_claude.go`, `client_codex.go`
- `apps/backend/internal/backendapp/usage_adapter.go`, `dynamic_routing.go`
- `apps/backend/internal/agent/runtime/dynamic_resolver.go`
- `apps/backend/internal/agent/runtime/dynamic/engine.go`, `types.go`, `conductor.go`
- Proposed `apps/backend/internal/agent/runtime/dynamic/selection.go`
- `apps/backend/internal/task/repository/sqlite/dynamic_route.go`
- `apps/backend/internal/task/repository/sqlite/usage_totals.go`
- `apps/backend/internal/task/models/usage_event.go` and existing turn types as read contracts
- Settings controller/handlers for proposed dynamic preview endpoints

## Dependencies and risks

Requires Task 01. Before affected logic, settle the plan's cost/reservation/off
semantics with control and prove native account binding and ledger attribution.
Do not call a missing read a zero. Fetch outside the engine lock and recheck
generation after fetch. Do not clear the chain when clearing retry counters.

## Results

Partial. Commit `d7caec982` delivers the pure ranking core only. The remaining
work orders are not started, so this work order is not complete.

### Delivered so far

- `usage.UtilizationWindow` gained additive `DurationSeconds`, `StartAt` and
  `ModelID` plus a `UsableFor` predicate. A window needs a numeric duration, a
  known reset and a non-positive span to score; it is never parsed out of the
  display `Label`, and an unusable window is unknown rather than zero.
- `dynamic/ranking.go` holds the pure, side-effect-free ranking a preview and a
  live selection can share: `PaceFromWindows` with the clamped elapsed fraction
  and the 0.05 floor, the largest-pace-across-windows rule, invalid-window
  rejection, known-before-unknown ordering with saved row order on ties,
  `CostRank` class ordering with the legacy unknown class last, and
  `ReservationExclusion` as an admission gate that leaves raw pace untouched.

### Verified

| Check | Exit | Result |
| --- | --- | --- |
| `go test ./internal/agent/runtime/dynamic -count=1` | 0 | pass, includes the new pace, cost, order and reservation cases |
| `go test ./internal/agent/usage -count=1 -timeout=10m` | 0 | pass |
| `golangci-lint run ./internal/agent/runtime/dynamic/... ./internal/agent/usage/...` | 0 | 0 issues |
| `go build ./...` | 0 | pass |

Two table expectations in the new ranking tests were wrong and were corrected;
the implementation matched the reviewed semantics in both cases.

### Not delivered

The ranking helper is not yet wired into `Engine.selectContext` or
`applyPolicyFailure`, so routing is still flat ordered selection and the
`pace` and `cost` modes have no runtime effect yet. Still outstanding: the
usage snapshot seam at the composition boundary, the Claude/Codex window
duration plumbing, the manual ledger aggregate joined through
`task_session_turns.execution_profile_id` with its PostgreSQL counterpart, the
durable `selection_chain` in `PolicyStateJSON`, the route reason codes, and the
read-only preview endpoints. `TierModePace` and `TierModeCost` are therefore
settings the runtime does not yet honour, which is why WO-03 must not ship
before this work order finishes.
